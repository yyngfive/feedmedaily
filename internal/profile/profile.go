package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	relevanceDirect    = "direct"
	relevanceIndirect  = "indirect"
	relevanceUnrelated = "unrelated"
)

type profileDocument struct {
	Meta           profileMeta       `json:"meta"`
	Scope          string            `json:"scope"`
	RelevanceRules relevanceRules    `json:"relevance_rules"`
	TopicTaxonomy  []topicDefinition `json:"topic_taxonomy"`
	FewShots       []profileFewShot  `json:"few_shots"`
}

type profileMeta struct {
	Name              string    `json:"name"`
	Version           int       `json:"version"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	SourceDescription string    `json:"source_description"`
}

type topicDefinition struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type profileFewShot struct {
	Title     string   `json:"title"`
	Relevance string   `json:"relevance"`
	Tags      []string `json:"tags"`
	Rationale string   `json:"rationale"`
}

type relevanceRules struct {
	Direct    []string `json:"direct"`
	Indirect  []string `json:"indirect"`
	Unrelated []string `json:"unrelated"`
}

type proposalDelta struct {
	Summary                string            `json:"summary"`
	DirectRuleAdditions    []string          `json:"direct_rule_additions"`
	IndirectRuleAdditions  []string          `json:"indirect_rule_additions"`
	UnrelatedRuleAdditions []string          `json:"unrelated_rule_additions"`
	ScopeRewrite           *string           `json:"scope_rewrite"`
	TagAdditions           []topicDefinition `json:"tag_additions"`
	TagRemovals            []string          `json:"tag_removals"`
}

func ReadCurrent(path string) (map[string]any, error) {
	// 读取并校验当前 profile；文件不存在时返回 nil。
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read classification profile: %w", err)
	}
	return ValidateBytes(data)
}

func WriteCurrent(path string, payload map[string]any) error {
	// 以和 Python persisted_profile 一致的紧凑结构写回当前 profile 文件。
	document, err := parseDocumentMap(payload)
	if err != nil {
		return err
	}
	compact := compactDocument(document)
	data, err := json.MarshalIndent(compact, "", "  ")
	if err != nil {
		return fmt.Errorf("encode classification profile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create classification profile dir: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write classification profile: %w", err)
	}
	return nil
}

func PrepareAppliedProfile(proposed map[string]any, current map[string]any, now time.Time) (map[string]any, int, error) {
	// 复刻 Python apply proposal 时的 meta 继承和版本递增规则。
	proposedDocument, err := parseDocumentMap(proposed)
	if err != nil {
		return nil, 0, err
	}
	version := 1
	createdAt := proposedDocument.Meta.CreatedAt
	if current != nil {
		currentDocument, err := parseDocumentMap(current)
		if err != nil {
			return nil, 0, err
		}
		version = currentDocument.Meta.Version + 1
		createdAt = currentDocument.Meta.CreatedAt
	}
	proposedDocument.Meta.Version = version
	proposedDocument.Meta.CreatedAt = createdAt
	proposedDocument.Meta.UpdatedAt = now.UTC()
	return compactDocumentMap(proposedDocument)
}

func PrepareUpdatedProfile(edited map[string]any, current map[string]any, now time.Time) (map[string]any, int, error) {
	// 保存用户直接编辑的 profile 时，保留创建时间和来源描述，只递增版本与更新时间。
	if current == nil {
		return nil, 0, fmt.Errorf("no classification profile exists yet")
	}
	editedDocument, err := parseDocumentMap(edited)
	if err != nil {
		return nil, 0, err
	}
	currentDocument, err := parseDocumentMap(current)
	if err != nil {
		return nil, 0, err
	}
	editedDocument.Meta.Version = currentDocument.Meta.Version + 1
	editedDocument.Meta.CreatedAt = currentDocument.Meta.CreatedAt
	editedDocument.Meta.UpdatedAt = now.UTC()
	editedDocument.Meta.SourceDescription = currentDocument.Meta.SourceDescription
	return compactDocumentMap(editedDocument)
}

func ValidateBytes(data []byte) (map[string]any, error) {
	// 用严格 JSON 解码校验 profile 结构，再返回原始对象形状。
	if _, err := parseDocumentBytes(data); err != nil {
		return nil, err
	}
	return decodeMap(data)
}

func ValidateMap(payload map[string]any) (map[string]any, error) {
	// 校验一个内存 profile 对象，并返回标准化后的紧凑形状。
	document, err := parseDocumentMap(payload)
	if err != nil {
		return nil, err
	}
	normalized, _, err := compactDocumentMap(document)
	return normalized, err
}

func ValidateProposalDeltaBytes(data []byte, fallbackSummary string) (map[string]any, error) {
	// 校验 proposal delta；旧库缺字段时回退到一个最小合法对象。
	clean := bytes.TrimSpace(data)
	if len(clean) == 0 || bytes.Equal(clean, []byte("null")) {
		return decodeMap([]byte(defaultProposalDeltaJSON(fallbackSummary)))
	}
	var delta proposalDelta
	if err := decodeStrict(clean, &delta); err != nil {
		return nil, fmt.Errorf("parse profile proposal delta: %w", err)
	}
	if err := delta.validate(); err != nil {
		return nil, err
	}
	return decodeMap(clean)
}

func ValidateProposalDeltaMap(payload map[string]any, fallbackSummary string) (map[string]any, error) {
	// 校验一个内存 proposal delta 对象，并返回标准化后的对象。
	if payload == nil {
		return decodeMap([]byte(defaultProposalDeltaJSON(fallbackSummary)))
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode profile proposal delta: %w", err)
	}
	return ValidateProposalDeltaBytes(data, fallbackSummary)
}

func ValidateModelProfileText(payload string) (map[string]any, error) {
	// 从模型输出中提取并校验完整 profile JSON。
	data, err := extractJSONObjectBytes(payload)
	if err != nil {
		return nil, err
	}
	document, err := parseDocumentBytes(data)
	if err != nil {
		return nil, err
	}
	normalized, _, err := compactDocumentMap(document)
	return normalized, err
}

func ValidateModelProposalDeltaText(payload string, fallbackSummary string) (map[string]any, error) {
	// 从模型输出中提取并校验 proposal delta JSON。
	data, err := extractJSONObjectBytes(payload)
	if err != nil {
		return nil, err
	}
	return ValidateProposalDeltaBytes(data, fallbackSummary)
}

func defaultProposalDeltaJSON(summary string) string {
	return fmt.Sprintf(`{"summary":%q,"direct_rule_additions":[],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}`, strings.TrimSpace(summary))
}

func parseDocumentBytes(data []byte) (profileDocument, error) {
	var document profileDocument
	if err := decodeStrict(data, &document); err != nil {
		return profileDocument{}, fmt.Errorf("parse classification profile: %w", err)
	}
	if err := document.validate(); err != nil {
		return profileDocument{}, err
	}
	return document, nil
}

func parseDocumentMap(payload map[string]any) (profileDocument, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return profileDocument{}, fmt.Errorf("encode classification profile: %w", err)
	}
	return parseDocumentBytes(data)
}

func compactDocumentMap(document profileDocument) (map[string]any, int, error) {
	compact := compactDocument(document)
	data, err := json.Marshal(compact)
	if err != nil {
		return nil, 0, fmt.Errorf("encode classification profile: %w", err)
	}
	payload, err := decodeMap(data)
	if err != nil {
		return nil, 0, fmt.Errorf("decode classification profile: %w", err)
	}
	return payload, compact.Meta.Version, nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("unexpected trailing content")
	}
	return nil
}

func decodeMap(data []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (d profileDocument) validate() error {
	if err := d.Meta.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(d.Scope) == "" {
		return fmt.Errorf("classification profile scope cannot be blank")
	}
	if len(d.FewShots) > 2 {
		return fmt.Errorf("classification profile few_shots cannot contain more than 2 items")
	}
	for _, topic := range d.TopicTaxonomy {
		if err := topic.validate(); err != nil {
			return err
		}
	}
	for _, shot := range d.FewShots {
		if err := shot.validate(); err != nil {
			return err
		}
	}
	return nil
}

func compactDocument(document profileDocument) profileDocument {
	return profileDocument{
		Meta: profileMeta{
			Name:              normalizeText(document.Meta.Name),
			Version:           document.Meta.Version,
			CreatedAt:         document.Meta.CreatedAt,
			UpdatedAt:         document.Meta.UpdatedAt,
			SourceDescription: normalizeText(document.Meta.SourceDescription),
		},
		Scope:          strings.TrimSpace(document.Scope),
		RelevanceRules: compactRules(document.RelevanceRules),
		TopicTaxonomy:  compactTopics(document.TopicTaxonomy),
		FewShots:       compactFewShots(document.FewShots),
	}
}

func (m profileMeta) validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("classification profile meta.name cannot be blank")
	}
	if m.Version < 1 {
		return fmt.Errorf("classification profile meta.version must be at least 1")
	}
	if m.CreatedAt.IsZero() || m.UpdatedAt.IsZero() {
		return fmt.Errorf("classification profile meta timestamps are required")
	}
	if strings.TrimSpace(m.SourceDescription) == "" {
		return fmt.Errorf("classification profile meta.source_description cannot be blank")
	}
	return nil
}

func (t topicDefinition) validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("classification profile topic id cannot be blank")
	}
	if strings.TrimSpace(t.Label) == "" {
		return fmt.Errorf("classification profile topic label cannot be blank")
	}
	return nil
}

func (f profileFewShot) validate() error {
	if strings.TrimSpace(f.Title) == "" {
		return fmt.Errorf("classification profile few_shot title cannot be blank")
	}
	if err := validateRelevance(f.Relevance); err != nil {
		return err
	}
	if strings.TrimSpace(f.Rationale) == "" {
		return fmt.Errorf("classification profile few_shot rationale cannot be blank")
	}
	return nil
}

func (d proposalDelta) validate() error {
	if strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("profile proposal delta summary cannot be blank")
	}
	for _, topic := range d.TagAdditions {
		if err := topic.validate(); err != nil {
			return err
		}
	}
	for _, removal := range d.TagRemovals {
		if strings.TrimSpace(removal) == "" {
			return fmt.Errorf("profile proposal delta tag_removals cannot contain blank values")
		}
	}
	return nil
}

func validateRelevance(value string) error {
	switch strings.TrimSpace(value) {
	case relevanceDirect, relevanceIndirect, relevanceUnrelated:
		return nil
	default:
		return fmt.Errorf("unsupported relevance value: %s", value)
	}
}

func compactRules(rules relevanceRules) relevanceRules {
	return relevanceRules{
		Direct:    normalizeRuleList(rules.Direct),
		Indirect:  normalizeRuleList(rules.Indirect),
		Unrelated: normalizeRuleList(rules.Unrelated),
	}
}

func compactTopics(items []topicDefinition) []topicDefinition {
	seen := map[string]struct{}{}
	result := make([]topicDefinition, 0, len(items))
	for _, item := range items {
		id := normalizeTopicID(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, topicDefinition{
			ID:    id,
			Label: normalizeText(item.Label),
		})
	}
	return result
}

func compactFewShots(items []profileFewShot) []profileFewShot {
	limit := len(items)
	if limit > 2 {
		limit = 2
	}
	result := make([]profileFewShot, 0, limit)
	for _, item := range items[:limit] {
		result = append(result, profileFewShot{
			Title:     normalizeText(item.Title),
			Relevance: strings.TrimSpace(item.Relevance),
			Tags:      normalizeTopicIDs(item.Tags),
			Rationale: normalizeText(item.Rationale),
		})
	}
	return result
}

func normalizeRuleList(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		clean := normalizeText(item)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func normalizeTopicIDs(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		clean := normalizeTopicID(item)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func normalizeTopicID(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), " ", "_"), "-", "_")
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func stripCodeFence(payload string) string {
	stripped := strings.TrimSpace(payload)
	if !strings.HasPrefix(stripped, "```") {
		return stripped
	}
	lines := strings.Split(stripped, "\n")
	if len(lines) == 0 {
		return stripped
	}
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractJSONObjectBytes(payload string) ([]byte, error) {
	stripped := stripCodeFence(payload)
	if _, err := parseJSONObjectMap([]byte(stripped)); err == nil {
		return []byte(stripped), nil
	}
	extracted, ok := extractJSONObject(stripped)
	if !ok {
		return nil, fmt.Errorf("invalid classification profile JSON: could not find a complete JSON object")
	}
	if _, err := parseJSONObjectMap([]byte(extracted)); err != nil {
		return nil, fmt.Errorf("invalid classification profile JSON: %w", err)
	}
	return []byte(extracted), nil
}

func parseJSONObjectMap(data []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("JSON must be an object")
	}
	return payload, nil
}

func extractJSONObject(payload string) (string, bool) {
	start := strings.Index(payload, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(payload); i++ {
		char := payload[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}
		if char == '"' {
			inString = true
			continue
		}
		if char == '{' {
			depth++
		} else if char == '}' {
			depth--
			if depth == 0 {
				return payload[start : i+1], true
			}
		}
	}
	return "", false
}
