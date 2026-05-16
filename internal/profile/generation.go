package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
)

type FeedbackProposalContext struct {
	FeedbackID         int64
	PaperID            int64
	PaperTitle         string
	Journal            *string
	Abstract           *string
	OriginalRelevance  string
	CorrectedRelevance string
	Note               *string
}

type ProposalDraft struct {
	Summary           string
	ProposedProfile   map[string]any
	RuleDelta         map[string]any
	Model             string
	SourceFeedbackIDs []int64
}

var requestProfileModelJSONFunc = requestProfileModelJSON

func GenerateInitialProfileProposal(settings config.Settings, interestDescription string, name *string) (ProposalDraft, error) {
	// 生成初始 classification profile proposal，并返回待落库的数据草稿。
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "bootstrap_started",
		Message:   "Generating initial classification profile proposal.",
		Data: map[string]any{
			"has_name":                 name != nil && strings.TrimSpace(*name) != "",
			"interest_description_len": len(strings.TrimSpace(interestDescription)),
		},
	})
	prompt := strings.TrimSpace(fmt.Sprintf(`
Build a complete scientific-literature classification profile from the user's
research interests.

Requirements:
- Return valid JSON only.
- Use exactly the schema shape shown below.
- The fixed relevance labels are direct, indirect, unrelated.
- topic_taxonomy must be lightweight and contain only id + label.
- Write practical, compact, reusable rules.
- few_shots are optional and must contain at most 2 examples total.
- Do not include description/examples fields for tags.
- Do not include a generic placeholder profile.

User profile name hint:
%s

User interest description:
%s

Required JSON shape:
%s
`, profileNameHint(name), strings.TrimSpace(interestDescription), compactProfileContract()))

	content, err := requestProfileModelJSONFunc(
		settings,
		"You design structured classification profiles.",
		prompt,
		4200,
	)
	if err != nil {
		return ProposalDraft{}, err
	}
	proposedDocument, err := coerceProfileDocument(settings, content)
	if err != nil {
		return ProposalDraft{}, err
	}
	summary := fmt.Sprintf(
		"Initial profile for %s with %d topic tags and %d few-shot examples.",
		proposedDocument.Meta.Name,
		len(proposedDocument.TopicTaxonomy),
		len(proposedDocument.FewShots),
	)
	proposedProfile, _, err := compactDocumentMap(proposedDocument)
	if err != nil {
		return ProposalDraft{}, err
	}
	ruleDeltaMap, err := proposalDeltaToMap(initialProfileDelta(proposedDocument, summary))
	if err != nil {
		return ProposalDraft{}, err
	}
	return ProposalDraft{
		Summary:         summary,
		ProposedProfile: proposedProfile,
		RuleDelta:       ruleDeltaMap,
		Model:           settings.ProfileModel,
	}, logInitialProposalCompleted(summary, proposedDocument, settings.ProfileModel)
}

func GenerateProfileProposal(settings config.Settings, current map[string]any, feedbackItems []FeedbackProposalContext) (ProposalDraft, error) {
	// 根据当前 profile 和 open feedback 生成一个 bounded proposal 草稿。
	if len(feedbackItems) == 0 {
		return ProposalDraft{}, fmt.Errorf("no feedback is available for profile proposal generation")
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "proposal_started",
		Message:   fmt.Sprintf("Generating profile proposal from %d feedback item(s)", len(feedbackItems)),
		Data:      map[string]any{"feedback_items": len(feedbackItems)},
	})
	currentDocument, err := parseDocumentMap(current)
	if err != nil {
		return ProposalDraft{}, err
	}
	compactProfileJSON, err := json.MarshalIndent(profilePromptPayload(currentDocument, false), "", "  ")
	if err != nil {
		return ProposalDraft{}, fmt.Errorf("encode current compact profile: %w", err)
	}
	feedbackPayload, err := json.MarshalIndent(feedbackPromptPayload(feedbackItems), "", "  ")
	if err != nil {
		return ProposalDraft{}, fmt.Errorf("encode feedback proposal context: %w", err)
	}
	prompt := strings.TrimSpace(fmt.Sprintf(`
Summarize the human feedback and propose compact rule updates for the current
scientific-literature classification profile.

Requirements:
- Return valid JSON only.
- Return a compact delta object, not a full profile.
- First infer the shared patterns behind the corrected labels.
- Then convert those patterns into reusable relevance-rule additions.
- Keep the current profile as the base document.
- Do not rewrite unrelated sections of the profile.
- Do not replace a specific scientific-interest profile with a generic
  or placeholder profile.
- Keep direct/indirect/unrelated as the only relevance labels.
- Only propose small bounded tag edits when clearly necessary.
- Do not generate few-shot examples.
- Use the feedback to sharpen boundaries and reduce future mistakes.

Current compact profile context:
%s

Human feedback:
%s

Return:
- a short summary of the shared patterns you found
- direct_rule_additions for patterns that should be treated as directly relevant
- indirect_rule_additions for patterns that should be treated as indirectly relevant
- unrelated_rule_additions for patterns that should be treated as unrelated
- optional scope_rewrite only if the current scope summary is misleading
- optional bounded tag_additions and tag_removals

Required JSON shape:
%s
`, string(compactProfileJSON), string(feedbackPayload), profileDeltaContract()))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You summarize feedback into compact profile rule deltas.",
		prompt,
		2600,
	)
	if err != nil {
		return ProposalDraft{}, err
	}
	ruleDelta, err := coerceProposalDelta(settings, content)
	if err != nil {
		return ProposalDraft{}, err
	}
	ruleDelta = boundedRuleDelta(ruleDelta)
	proposedDocument := mergeProfileDelta(currentDocument, ruleDelta)
	proposedDocument.Meta = normalizedProfileMeta(currentDocument)
	if reason := destructiveRevisionReason(currentDocument, proposedDocument); reason != "" {
		return ProposalDraft{}, fmt.Errorf("model generated an unsafe profile proposal. %s Current profile was kept unchanged.", reason)
	}
	proposedProfile, _, err := compactDocumentMap(proposedDocument)
	if err != nil {
		return ProposalDraft{}, err
	}
	ruleDeltaMap, err := proposalDeltaToMap(ruleDelta)
	if err != nil {
		return ProposalDraft{}, err
	}
	sourceFeedbackIDs := make([]int64, 0, len(feedbackItems))
	for _, item := range feedbackItems {
		sourceFeedbackIDs = append(sourceFeedbackIDs, item.FeedbackID)
	}
	draft := ProposalDraft{
		Summary:           ruleDelta.Summary,
		ProposedProfile:   proposedProfile,
		RuleDelta:         ruleDeltaMap,
		Model:             settings.ProfileModel,
		SourceFeedbackIDs: sourceFeedbackIDs,
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "proposal_completed",
		Message:   "Generated profile proposal.",
		Data: map[string]any{
			"feedback_items": len(feedbackItems),
			"model":          settings.ProfileModel,
			"summary":        ruleDelta.Summary,
		},
	})
	return draft, nil
}

func requestProfileModelJSON(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int) (string, error) {
	// 通过 OpenAI-compatible chat completions 调用 profile model 并返回 message content。
	if strings.TrimSpace(settings.ProfileAPIKey) == "" {
		return "", fmt.Errorf("SCIRSS_PROFILE_API_KEY is required for profile generation and prompt revision")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(settings.ProfileBaseURL), "/") + "/chat/completions"
	payload := map[string]any{
		"model": settings.ProfileModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	if thinking := strings.TrimSpace(settings.ProfileThinking); thinking != "" {
		payload["thinking"] = map[string]string{"type": thinking}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode profile generation request: %w", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build profile generation request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+settings.ProfileAPIKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "profile",
			Action:    "request_failed",
			Message:   fmt.Sprintf("LLM Request: POST %s failed", endpoint),
			Error:     err.Error(),
			Data: map[string]any{
				"model":       settings.ProfileModel,
				"duration_ms": time.Since(started).Milliseconds(),
			},
		})
		return "", fmt.Errorf("request profile generation: %w", err)
	}
	defer response.Body.Close()
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "request",
		Message:   fmt.Sprintf("LLM Request: POST %s %q", endpoint, response.Proto+" "+response.Status),
		Data: map[string]any{
			"model":       settings.ProfileModel,
			"status_code": response.StatusCode,
			"duration_ms": time.Since(started).Milliseconds(),
		},
	})
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read profile generation response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		return "", fmt.Errorf("profile model request failed: %s", message)
	}
	var payloadResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &payloadResponse); err != nil {
		return "", fmt.Errorf("parse profile generation response: %w", err)
	}
	if len(payloadResponse.Choices) == 0 {
		return "", fmt.Errorf("profile model response did not contain any choices")
	}
	return payloadResponse.Choices[0].Message.Content, nil
}

func coerceProfileDocument(settings config.Settings, content string) (profileDocument, error) {
	payload, err := ValidateModelProfileText(content)
	if err == nil {
		return parseDocumentMap(payload)
	}
	repaired, repairErr := repairProfileJSON(settings, content)
	if repairErr != nil {
		return profileDocument{}, fmt.Errorf("model returned invalid classification profile JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseDocumentMap(repaired)
}

func coerceProposalDelta(settings config.Settings, content string) (proposalDelta, error) {
	payload, err := ValidateModelProposalDeltaText(content, "Generated profile proposal.")
	if err == nil {
		return parseProposalDeltaMap(payload)
	}
	repaired, repairErr := repairProposalDeltaJSON(settings, content)
	if repairErr != nil {
		return proposalDelta{}, fmt.Errorf("model returned invalid profile delta JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseProposalDeltaMap(repaired)
}

func repairProfileJSON(settings config.Settings, malformedContent string) (map[string]any, error) {
	prompt := strings.TrimSpace(fmt.Sprintf(`
Repair the malformed scientific-literature classification profile below.

Requirements:
- Return valid JSON only.
- Return a complete profile object.
- Follow the required schema exactly.
- Keep the repaired content faithful to the original intent.
- If the draft was truncated, infer the smallest sensible completion.

Required JSON shape:
%s

Malformed draft:
%s
`, compactProfileContract(), malformedContent))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You repair malformed JSON classification profiles.",
		prompt,
		4200,
	)
	if err != nil {
		return nil, err
	}
	return ValidateModelProfileText(content)
}

func repairProposalDeltaJSON(settings config.Settings, malformedContent string) (map[string]any, error) {
	prompt := strings.TrimSpace(fmt.Sprintf(`
Repair the malformed profile-update delta below.

Requirements:
- Return valid JSON only.
- Return a complete delta object.
- Follow the required schema exactly.
- Keep the repaired content faithful to the original intent.
- If the draft was truncated, infer the smallest sensible completion.

Required JSON shape:
%s

Malformed draft:
%s
`, profileDeltaContract(), malformedContent))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You repair malformed JSON profile update deltas.",
		prompt,
		2200,
	)
	if err != nil {
		return nil, err
	}
	return ValidateModelProposalDeltaText(content, "Generated profile proposal.")
}

func logInitialProposalCompleted(summary string, document profileDocument, model string) error {
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "bootstrap_completed",
		Message:   "Generated initial classification profile proposal.",
		Data: map[string]any{
			"model":        model,
			"summary":      summary,
			"topic_tags":   len(document.TopicTaxonomy),
			"few_shots":    len(document.FewShots),
			"profile_name": document.Meta.Name,
		},
	})
	return nil
}

func compactProfileContract() string {
	return `{
  "meta": {
    "name": "short profile name",
    "version": 1,
    "created_at": "ISO-8601 datetime",
    "updated_at": "ISO-8601 datetime",
    "source_description": "original user interest description"
  },
  "scope": "one paragraph describing the reader's research interests",
  "relevance_rules": {
    "direct": ["rule string"],
    "indirect": ["rule string"],
    "unrelated": ["rule string"]
  },
  "topic_taxonomy": [
    {
      "id": "snake_case_tag",
      "label": "Display Label"
    }
  ],
  "few_shots": [
    {
      "title": "paper title",
      "relevance": "direct",
      "tags": ["snake_case_tag"],
      "rationale": "why this label is correct"
    }
  ]
}`
}

func profileDeltaContract() string {
	return `{
  "summary": "short summary of what changed based on feedback",
  "direct_rule_additions": ["rule to append to direct relevance rules"],
  "indirect_rule_additions": ["rule to append to indirect relevance rules"],
  "unrelated_rule_additions": ["rule to append to unrelated relevance rules"],
  "scope_rewrite": "optional short rewritten scope summary",
  "tag_additions": [{"id": "snake_case_tag", "label": "Display Label"}],
  "tag_removals": ["old_tag_id"]
}`
}

func profileNameHint(name *string) string {
	if name == nil || strings.TrimSpace(*name) == "" {
		return "Default profile"
	}
	return strings.TrimSpace(*name)
}

func feedbackPromptPayload(items []FeedbackProposalContext) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"feedback_id":         item.FeedbackID,
			"paper_id":            item.PaperID,
			"paper_title":         item.PaperTitle,
			"journal":             item.Journal,
			"abstract":            item.Abstract,
			"original_relevance":  item.OriginalRelevance,
			"corrected_relevance": item.CorrectedRelevance,
			"note":                item.Note,
		})
	}
	return payload
}

func profilePromptPayload(document profileDocument, includeFewShots bool) map[string]any {
	compact := compactDocument(document)
	payload := map[string]any{
		"scope": compact.Scope,
		"relevance_rules": map[string]any{
			"direct":    compact.RelevanceRules.Direct,
			"indirect":  compact.RelevanceRules.Indirect,
			"unrelated": compact.RelevanceRules.Unrelated,
		},
		"topic_taxonomy": compact.TopicTaxonomy,
	}
	if includeFewShots && len(compact.FewShots) > 0 {
		payload["few_shots"] = compact.FewShots
	}
	return payload
}

func normalizedProfileMeta(current profileDocument) profileMeta {
	return profileMeta{
		Name:              current.Meta.Name,
		Version:           current.Meta.Version + 1,
		CreatedAt:         current.Meta.CreatedAt,
		UpdatedAt:         time.Now().UTC(),
		SourceDescription: current.Meta.SourceDescription,
	}
}

func boundedRuleDelta(delta proposalDelta) proposalDelta {
	result := delta
	result.DirectRuleAdditions = sliceStrings(delta.DirectRuleAdditions, 6)
	result.IndirectRuleAdditions = sliceStrings(delta.IndirectRuleAdditions, 6)
	result.UnrelatedRuleAdditions = sliceStrings(delta.UnrelatedRuleAdditions, 6)
	result.TagAdditions = compactTopics(sliceTopics(delta.TagAdditions, 3))
	result.TagRemovals = normalizeTopicIDs(sliceStrings(delta.TagRemovals, 3))
	return result
}

func initialProfileDelta(document profileDocument, summary string) proposalDelta {
	return proposalDelta{
		Summary:                strings.TrimSpace(summary),
		DirectRuleAdditions:    document.RelevanceRules.Direct,
		IndirectRuleAdditions:  document.RelevanceRules.Indirect,
		UnrelatedRuleAdditions: document.RelevanceRules.Unrelated,
		ScopeRewrite:           stringPointer(document.Scope),
		TagAdditions:           document.TopicTaxonomy,
		TagRemovals:            []string{},
	}
}

func mergeProfileDelta(current profileDocument, delta proposalDelta) profileDocument {
	currentCompact := compactDocument(current)
	bounded := boundedRuleDelta(delta)
	removals := map[string]struct{}{}
	for _, removal := range bounded.TagRemovals {
		removals[normalizeTopicID(removal)] = struct{}{}
	}
	mergedTopics := make([]topicDefinition, 0, len(currentCompact.TopicTaxonomy)+len(bounded.TagAdditions))
	mergedIDs := map[string]struct{}{}
	for _, topic := range currentCompact.TopicTaxonomy {
		if _, removed := removals[topic.ID]; removed {
			continue
		}
		mergedTopics = append(mergedTopics, topic)
		mergedIDs[topic.ID] = struct{}{}
	}
	for _, topic := range bounded.TagAdditions {
		if _, exists := mergedIDs[topic.ID]; exists {
			continue
		}
		mergedTopics = append(mergedTopics, topic)
		mergedIDs[topic.ID] = struct{}{}
	}
	mergedFewShots := make([]profileFewShot, 0, len(currentCompact.FewShots))
	for _, item := range currentCompact.FewShots {
		filteredTags := make([]string, 0, len(item.Tags))
		for _, tag := range item.Tags {
			if _, exists := mergedIDs[tag]; exists {
				filteredTags = append(filteredTags, tag)
			}
		}
		mergedFewShots = append(mergedFewShots, profileFewShot{
			Title:     item.Title,
			Relevance: item.Relevance,
			Tags:      filteredTags,
			Rationale: item.Rationale,
		})
	}
	scope := currentCompact.Scope
	if bounded.ScopeRewrite != nil && strings.TrimSpace(*bounded.ScopeRewrite) != "" {
		scope = strings.TrimSpace(*bounded.ScopeRewrite)
	}
	return profileDocument{
		Meta:  currentCompact.Meta,
		Scope: scope,
		RelevanceRules: relevanceRules{
			Direct:    normalizeRuleList(append(append([]string{}, currentCompact.RelevanceRules.Direct...), bounded.DirectRuleAdditions...)),
			Indirect:  normalizeRuleList(append(append([]string{}, currentCompact.RelevanceRules.Indirect...), bounded.IndirectRuleAdditions...)),
			Unrelated: normalizeRuleList(append(append([]string{}, currentCompact.RelevanceRules.Unrelated...), bounded.UnrelatedRuleAdditions...)),
		},
		TopicTaxonomy: mergedTopics,
		FewShots:      compactFewShots(mergedFewShots),
	}
}

func destructiveRevisionReason(current profileDocument, proposed profileDocument) string {
	currentTags := map[string]struct{}{}
	for _, item := range current.TopicTaxonomy {
		currentTags[item.ID] = struct{}{}
	}
	proposedTags := map[string]struct{}{}
	for _, item := range proposed.TopicTaxonomy {
		proposedTags[item.ID] = struct{}{}
	}
	if len(currentTags) > 0 && len(proposedTags) == 0 {
		return "Generated proposal removed every existing topic tag."
	}
	overlap := 0
	for tag := range currentTags {
		if _, ok := proposedTags[tag]; ok {
			overlap++
		}
	}
	if len(currentTags) >= 5 && len(proposedTags) <= max(2, len(currentTags)/3) && overlap < max(2, len(currentTags)/3) {
		return fmt.Sprintf("Generated proposal collapsed the topic taxonomy too aggressively (%d -> %d tags, overlap %d).", len(currentTags), len(proposedTags), overlap)
	}
	if strings.Contains(strings.ToLower(proposed.Scope), "general scientific literature") && strings.TrimSpace(current.Scope) != strings.TrimSpace(proposed.Scope) {
		return "Generated proposal replaced the specific research scope with a generic scope."
	}
	return ""
}

func proposalDeltaToMap(delta proposalDelta) (map[string]any, error) {
	data, err := json.Marshal(delta)
	if err != nil {
		return nil, fmt.Errorf("encode profile proposal delta: %w", err)
	}
	return decodeMap(data)
}

func parseProposalDeltaMap(payload map[string]any) (proposalDelta, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return proposalDelta{}, fmt.Errorf("encode profile proposal delta: %w", err)
	}
	var delta proposalDelta
	if err := decodeStrict(data, &delta); err != nil {
		return proposalDelta{}, fmt.Errorf("parse profile proposal delta: %w", err)
	}
	if err := delta.validate(); err != nil {
		return proposalDelta{}, err
	}
	return delta, nil
}

func sliceStrings(items []string, limit int) []string {
	if len(items) <= limit {
		return append([]string{}, items...)
	}
	return append([]string{}, items[:limit]...)
}

func sliceTopics(items []topicDefinition, limit int) []topicDefinition {
	if len(items) <= limit {
		return append([]topicDefinition{}, items...)
	}
	return append([]topicDefinition{}, items[:limit]...)
}

func stringPointer(value string) *string {
	clean := value
	return &clean
}
