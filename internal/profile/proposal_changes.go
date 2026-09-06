package profile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ProposalSectionScope         = "scope"
	ProposalSectionDirectRule    = "direct_rule"
	ProposalSectionIndirectRule  = "indirect_rule"
	ProposalSectionUnrelatedRule = "unrelated_rule"
	ProposalSectionTopic         = "topic"

	ProposalOperationAdd     = "add"
	ProposalOperationRemove  = "remove"
	ProposalOperationRewrite = "rewrite"
	ProposalOperationMerge   = "merge"

	ProposalStatusProposed = "proposed"
	ProposalStatusAccepted = "accepted"
	ProposalStatusRejected = "rejected"
	ProposalStatusIgnored  = "ignored"
)

type ProposalChange struct {
	ID          string   `json:"id"`
	Section     string   `json:"section"`
	Operation   string   `json:"operation"`
	Summary     string   `json:"summary"`
	TextBefore  []string `json:"text_before"`
	TextAfter   []string `json:"text_after"`
	// TopicsBefore/TopicsAfter 是规则变更携带的主题 label 列表（direct/indirect 专属），
	// apply 时才解析为注册表 id；topic section 变更仍使用 topic_before/topic_after。
	TopicsBefore      []string          `json:"topics_before"`
	TopicsAfter       []string          `json:"topics_after"`
	TopicBefore       []topicDefinition `json:"topic_before"`
	TopicAfter        []topicDefinition `json:"topic_after"`
	Rationale         string            `json:"rationale"`
	SourceFeedbackIDs []int64           `json:"source_feedback_ids"`
	SourcePaperIDs    []int64           `json:"source_paper_ids"`
	Status            string            `json:"status"`
}

type flexibleStringList []string

func (items *flexibleStringList) UnmarshalJSON(data []byte) error {
	clean := strings.TrimSpace(string(data))
	if clean == "" || clean == "null" {
		*items = []string{}
		return nil
	}
	if strings.HasPrefix(clean, `"`) {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			*items = []string{}
		} else {
			*items = []string{value}
		}
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	*items = values
	return nil
}

func (change *ProposalChange) UnmarshalJSON(data []byte) error {
	var payload struct {
		ID                string             `json:"id"`
		Section           string             `json:"section"`
		Operation         string             `json:"operation"`
		Summary           string             `json:"summary"`
		TextBefore        flexibleStringList `json:"text_before"`
		TextAfter         flexibleStringList `json:"text_after"`
		TopicsBefore      flexibleStringList `json:"topics_before"`
		TopicsAfter       flexibleStringList `json:"topics_after"`
		TopicBefore       []topicDefinition  `json:"topic_before"`
		TopicAfter        []topicDefinition  `json:"topic_after"`
		Rationale         string             `json:"rationale"`
		SourceFeedbackIDs []int64            `json:"source_feedback_ids"`
		SourcePaperIDs    []int64            `json:"source_paper_ids"`
		Status            string             `json:"status"`
	}
	if err := decodeStrict(data, &payload); err != nil {
		return err
	}
	*change = ProposalChange{
		ID:                payload.ID,
		Section:           payload.Section,
		Operation:         payload.Operation,
		Summary:           payload.Summary,
		TextBefore:        []string(payload.TextBefore),
		TextAfter:         []string(payload.TextAfter),
		TopicsBefore:      []string(payload.TopicsBefore),
		TopicsAfter:       []string(payload.TopicsAfter),
		TopicBefore:       payload.TopicBefore,
		TopicAfter:        payload.TopicAfter,
		Rationale:         payload.Rationale,
		SourceFeedbackIDs: payload.SourceFeedbackIDs,
		SourcePaperIDs:    payload.SourcePaperIDs,
		Status:            payload.Status,
	}
	return nil
}

func ValidateProposalChangesBytes(data []byte) ([]ProposalChange, error) {
	clean := strings.TrimSpace(string(data))
	if clean == "" || clean == "null" {
		return []ProposalChange{}, nil
	}
	var changes []ProposalChange
	if err := decodeStrict([]byte(clean), &changes); err != nil {
		return nil, fmt.Errorf("parse profile proposal changes: %w", err)
	}
	return normalizeProposalChanges(changes)
}

func ValidateProposalChanges(changes []ProposalChange) ([]ProposalChange, error) {
	data, err := json.Marshal(changes)
	if err != nil {
		return nil, fmt.Errorf("encode profile proposal changes: %w", err)
	}
	return ValidateProposalChangesBytes(data)
}

func FinalizeProposalChanges(changes []ProposalChange, acceptedIDs []string, rejectedIDs []string) ([]ProposalChange, error) {
	accepted := map[string]struct{}{}
	rejected := map[string]struct{}{}
	for _, id := range acceptedIDs {
		clean := strings.TrimSpace(id)
		if clean == "" {
			return nil, fmt.Errorf("accepted_change_ids cannot contain blank values")
		}
		accepted[clean] = struct{}{}
	}
	for _, id := range rejectedIDs {
		clean := strings.TrimSpace(id)
		if clean == "" {
			return nil, fmt.Errorf("rejected_change_ids cannot contain blank values")
		}
		if _, ok := accepted[clean]; ok {
			return nil, fmt.Errorf("accepted_change_ids and rejected_change_ids must be disjoint")
		}
		rejected[clean] = struct{}{}
	}
	finalized := make([]ProposalChange, 0, len(changes))
	acceptedCount := 0
	for _, change := range changes {
		next := change
		if _, ok := accepted[next.ID]; ok {
			next.Status = ProposalStatusAccepted
			acceptedCount++
		} else if _, ok := rejected[next.ID]; ok {
			next.Status = ProposalStatusRejected
		} else {
			next.Status = ProposalStatusIgnored
		}
		finalized = append(finalized, next)
		delete(accepted, next.ID)
		delete(rejected, next.ID)
	}
	if len(accepted) > 0 || len(rejected) > 0 {
		return nil, fmt.Errorf("apply payload referenced unknown proposal change ids")
	}
	if acceptedCount == 0 {
		return nil, fmt.Errorf("at least one accepted change is required")
	}
	return finalized, nil
}

func PrepareAppliedProfileFromChanges(current map[string]any, changes []ProposalChange, now time.Time) (map[string]any, int, error) {
	if current == nil {
		return nil, 0, fmt.Errorf("no classification profile exists yet")
	}
	document, err := parseDocumentMap(current)
	if err != nil {
		return nil, 0, err
	}
	appliedDocument, err := applyProposalChanges(document, changes, func(change ProposalChange) bool {
		return change.Status == ProposalStatusAccepted
	})
	if err != nil {
		return nil, 0, err
	}
	appliedDocument.Meta = normalizedProfileMeta(document)
	appliedDocument.Meta.UpdatedAt = now.UTC()
	return compactDocumentMap(appliedDocument)
}

func BuildProposedProfileFromChanges(current map[string]any, changes []ProposalChange) (map[string]any, error) {
	document, err := parseDocumentMap(current)
	if err != nil {
		return nil, err
	}
	proposedDocument, err := applyProposalChanges(document, changes, func(ProposalChange) bool {
		return true
	})
	if err != nil {
		return nil, err
	}
	proposedDocument.Meta = normalizedProfileMeta(document)
	proposedDocument.FewShots = compactFewShots(document.FewShots)
	payload, _, err := compactDocumentMap(proposedDocument)
	return payload, err
}

func CurrentProfileVersion(current map[string]any) (int, error) {
	document, err := parseDocumentMap(current)
	if err != nil {
		return 0, err
	}
	return document.Meta.Version, nil
}

func normalizeProposalChanges(changes []ProposalChange) ([]ProposalChange, error) {
	result := make([]ProposalChange, 0, len(changes))
	seen := map[string]struct{}{}
	for _, item := range changes {
		next := item
		next.ID = strings.TrimSpace(next.ID)
		next.Section = strings.TrimSpace(next.Section)
		next.Operation = normalizeProposalOperation(next.Operation)
		next.Summary = normalizeText(next.Summary)
		next.Rationale = normalizeText(next.Rationale)
		if next.Status == "" {
			next.Status = ProposalStatusProposed
		}
		next.Status = strings.TrimSpace(next.Status)
		next.TextBefore = normalizeRuleList(next.TextBefore)
		next.TextAfter = normalizeRuleList(next.TextAfter)
		next.TopicsBefore = normalizeLabelList(next.TopicsBefore)
		next.TopicsAfter = normalizeLabelList(next.TopicsAfter)
		// topic add 变更的 topic_after 允许缺 id（label-only），按 label 去重。
		if next.Section == ProposalSectionTopic {
			next.TopicBefore = normalizeLabelOnlyTopics(next.TopicBefore)
			next.TopicAfter = normalizeLabelOnlyTopics(next.TopicAfter)
		} else {
			next.TopicBefore = compactTopics(next.TopicBefore)
			next.TopicAfter = compactTopics(next.TopicAfter)
		}
		next.SourceFeedbackIDs = normalizeInt64List(next.SourceFeedbackIDs)
		next.SourcePaperIDs = normalizeInt64List(next.SourcePaperIDs)
		if err := next.validate(); err != nil {
			return nil, err
		}
		if _, ok := seen[next.ID]; ok {
			return nil, fmt.Errorf("profile proposal changes cannot contain duplicate ids")
		}
		seen[next.ID] = struct{}{}
		result = append(result, next)
	}
	return result, nil
}

func normalizeLabelList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := normalizeText(value)
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

// normalizeLabelOnlyTopics 规整 label-only 主题条目：保留缺失的 id，按 label 去重。
func normalizeLabelOnlyTopics(items []topicDefinition) []topicDefinition {
	result := make([]topicDefinition, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		label := normalizeText(item.Label)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, topicDefinition{ID: strings.TrimSpace(item.ID), Label: label})
	}
	return result
}

func normalizeProposalOperation(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "restore":
		return ProposalOperationAdd
	default:
		return strings.TrimSpace(value)
	}
}

func (c ProposalChange) validate() error {
	if c.ID == "" {
		return fmt.Errorf("profile proposal change id cannot be blank")
	}
	if c.Summary == "" {
		return fmt.Errorf("profile proposal change summary cannot be blank")
	}
	if c.Rationale == "" {
		return fmt.Errorf("profile proposal change rationale cannot be blank")
	}
	switch c.Section {
	case ProposalSectionScope, ProposalSectionDirectRule, ProposalSectionIndirectRule, ProposalSectionUnrelatedRule, ProposalSectionTopic:
	default:
		return fmt.Errorf("unsupported proposal change section: %s", c.Section)
	}
	switch c.Operation {
	case ProposalOperationAdd, ProposalOperationRemove, ProposalOperationRewrite, ProposalOperationMerge:
	default:
		return fmt.Errorf("unsupported proposal change operation: %s", c.Operation)
	}
	switch c.Status {
	case ProposalStatusProposed, ProposalStatusAccepted, ProposalStatusRejected, ProposalStatusIgnored:
	default:
		return fmt.Errorf("unsupported proposal change status: %s", c.Status)
	}
	// 主题标签 label 列表只允许出现在 direct/indirect 规则变更上。
	if c.Section != ProposalSectionDirectRule && c.Section != ProposalSectionIndirectRule {
		if len(c.TopicsBefore) > 0 || len(c.TopicsAfter) > 0 {
			return fmt.Errorf("section %s cannot include topics_before or topics_after", c.Section)
		}
	}
	switch c.Section {
	case ProposalSectionScope, ProposalSectionUnrelatedRule:
		if len(c.TopicBefore) > 0 || len(c.TopicAfter) > 0 {
			return fmt.Errorf("text changes cannot include topic_before or topic_after")
		}
	case ProposalSectionDirectRule, ProposalSectionIndirectRule:
		if len(c.TopicBefore) > 0 || len(c.TopicAfter) > 0 {
			return fmt.Errorf("rule changes cannot include topic_before or topic_after; use topics_before/topics_after")
		}
	case ProposalSectionTopic:
		// 主题注册表变更只允许 add：删除/改名/合并由用户在编辑器手动完成。
		if c.Operation != ProposalOperationAdd {
			return fmt.Errorf("topic changes support only the add operation")
		}
		if len(c.TextBefore) > 0 || len(c.TextAfter) > 0 {
			return fmt.Errorf("topic changes cannot include text_before or text_after")
		}
		for _, item := range c.TopicBefore {
			if err := item.validate(); err != nil {
				return err
			}
		}
		if len(c.TopicAfter) == 0 {
			return fmt.Errorf("topic add changes require topic_after")
		}
		for _, item := range c.TopicAfter {
			if strings.TrimSpace(item.Label) == "" {
				return fmt.Errorf("topic add changes require a non-blank label")
			}
		}
	}
	switch c.Operation {
	case ProposalOperationAdd:
		if c.Section != ProposalSectionTopic && len(c.TextAfter) == 0 {
			return fmt.Errorf("text add changes require text_after")
		}
	case ProposalOperationRemove:
		if c.Section == ProposalSectionTopic {
			return fmt.Errorf("topic changes support only the add operation")
		}
		if len(c.TextBefore) == 0 {
			return fmt.Errorf("text remove changes require text_before")
		}
	case ProposalOperationRewrite, ProposalOperationMerge:
		if c.Section == ProposalSectionTopic {
			return fmt.Errorf("topic changes support only the add operation")
		}
		if len(c.TextBefore) == 0 || len(c.TextAfter) == 0 {
			return fmt.Errorf("text rewrite and merge changes require text_before and text_after")
		}
	}
	return nil
}

func normalizeInt64List(values []int64) []int64 {
	result := make([]int64, 0, len(values))
	seen := map[int64]struct{}{}
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func applyProposalChanges(base profileDocument, changes []ProposalChange, include func(ProposalChange) bool) (profileDocument, error) {
	current := compactDocument(base)
	// 先应用 topic add 变更并建立 label→id 解析表，规则标签引用才能解析。
	labelToID := map[string]string{}
	for _, topic := range current.TopicTaxonomy {
		labelToID[strings.ToLower(topic.Label)] = topic.ID
	}
	for _, change := range changes {
		if !include(change) || change.Section != ProposalSectionTopic {
			continue
		}
		for _, item := range change.TopicAfter {
			label := strings.TrimSpace(item.Label)
			if label == "" {
				continue
			}
			if _, exists := labelToID[strings.ToLower(label)]; exists {
				continue
			}
			entry := topicDefinition{ID: newTopicID(), Label: label}
			current.TopicTaxonomy = append(current.TopicTaxonomy, entry)
			labelToID[strings.ToLower(label)] = entry.ID
		}
	}
	for _, change := range changes {
		if !include(change) {
			continue
		}
		switch change.Section {
		case ProposalSectionScope:
			if len(change.TextAfter) == 0 {
				return profileDocument{}, fmt.Errorf("scope change %s is missing text_after", change.ID)
			}
			current.Scope = strings.TrimSpace(change.TextAfter[0])
		case ProposalSectionDirectRule:
			tags, err := resolveTopicTags(change, labelToID)
			if err != nil {
				return profileDocument{}, err
			}
			current.RelevanceRules.Direct = applyRuleChange(current.RelevanceRules.Direct, change, tags)
		case ProposalSectionIndirectRule:
			tags, err := resolveTopicTags(change, labelToID)
			if err != nil {
				return profileDocument{}, err
			}
			current.RelevanceRules.Indirect = applyRuleChange(current.RelevanceRules.Indirect, change, tags)
		case ProposalSectionUnrelatedRule:
			current.RelevanceRules.Unrelated = applyRuleChange(current.RelevanceRules.Unrelated, change, nil)
		case ProposalSectionTopic:
			continue
		default:
			return profileDocument{}, fmt.Errorf("unsupported proposal change section: %s", change.Section)
		}
	}
	current = compactDocument(current)
	return current, nil
}

// resolveTopicTags 把规则变更的主题 label 解析为注册表 id（大小写不敏感）。
// 引用了既不在注册表、也不由本次 topic add 变更提供的 label 时返回错误。
func resolveTopicTags(change ProposalChange, labelToID map[string]string) ([]string, error) {
	result := make([]string, 0, len(change.TopicsAfter))
	for _, label := range change.TopicsAfter {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		id, ok := labelToID[strings.ToLower(clean)]
		if !ok {
			return nil, fmt.Errorf("change %s references unknown topic label: %s", change.ID, clean)
		}
		result = append(result, id)
	}
	return result, nil
}

func applyRuleChange(base []classificationRule, change ProposalChange, tags []string) []classificationRule {
	switch change.Operation {
	case ProposalOperationAdd:
		return appendRuleObjects(base, change.TextAfter, tags)
	case ProposalOperationRemove:
		return removeRules(base, change.TextBefore)
	case ProposalOperationRewrite, ProposalOperationMerge:
		next := removeRules(base, change.TextBefore)
		return appendRuleObjects(next, change.TextAfter, tags)
	default:
		return normalizeRuleObjects(base)
	}
}

func appendRuleObjects(base []classificationRule, texts []string, tags []string) []classificationRule {
	merged := append([]classificationRule{}, base...)
	for _, text := range texts {
		merged = append(merged, classificationRule{
			Text:     text,
			TopicIDs: append([]string{}, tags...),
		})
	}
	return normalizeRuleObjects(merged)
}

func removeRules(base []classificationRule, targets []string) []classificationRule {
	removals := map[string]struct{}{}
	for _, target := range normalizeRuleList(targets) {
		removals[target] = struct{}{}
	}
	result := make([]classificationRule, 0, len(base))
	for _, item := range normalizeRuleObjects(base) {
		if _, ok := removals[item.Text]; ok {
			continue
		}
		result = append(result, item)
	}
	return result
}

func applyTopicChange(base []topicDefinition, change ProposalChange) []topicDefinition {
	switch change.Operation {
	case ProposalOperationAdd:
		return compactTopics(append(append([]topicDefinition{}, base...), change.TopicAfter...))
	case ProposalOperationRemove:
		return removeTopics(base, change.TopicBefore)
	case ProposalOperationRewrite, ProposalOperationMerge:
		next := removeTopics(base, change.TopicBefore)
		return compactTopics(append(next, change.TopicAfter...))
	default:
		return compactTopics(base)
	}
}

func removeTopics(base []topicDefinition, targets []topicDefinition) []topicDefinition {
	removals := map[string]struct{}{}
	for _, item := range compactTopics(targets) {
		removals[item.ID] = struct{}{}
	}
	result := make([]topicDefinition, 0, len(base))
	for _, item := range compactTopics(base) {
		if _, ok := removals[item.ID]; ok {
			continue
		}
		result = append(result, item)
	}
	return result
}