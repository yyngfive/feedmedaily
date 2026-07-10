package profile

import (
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
	"strings"
	"time"
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
	BaseProfileVersion int
	Summary            string
	ProposedProfile    map[string]any
	Changes            []ProposalChange
	RuleDelta          map[string]any
	Model              string
	SourceFeedbackIDs  []int64
	Rejection          *ProposalValidationResult
}

type ProposalValidationResult struct {
	Accepted       bool     `json:"accepted"`
	HardRejected   bool     `json:"hard_rejected"`
	Summary        string   `json:"summary"`
	BlockingIssues []string `json:"blocking_issues"`
	RequiredFixes  []string `json:"required_fixes"`
}

type compactProposalAttempt struct {
	Summary          string
	ProposedProfile  map[string]any
	ProposedDocument profileDocument
	Changes          []ProposalChange
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
- Write practical, compact, reusable rules.
- Do not generate topic_taxonomy entries; return an empty array.
- Do not generate few_shots; return an empty array.
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
		"Initial profile for %s with compact relevance rules.",
		proposedDocument.Meta.Name,
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
		BaseProfileVersion: 0,
		Summary:            summary,
		ProposedProfile:    proposedProfile,
		Changes:            []ProposalChange{},
		RuleDelta:          ruleDeltaMap,
		Model:              settings.ProfileModel,
	}, logInitialProposalCompleted(summary, proposedDocument, settings.ProfileModel)
}

func GenerateProfileProposal(settings config.Settings, current map[string]any, feedbackItems []FeedbackProposalContext) (ProposalDraft, error) {
	// 根据当前 profile 和 open feedback 生成一个会自动压缩同类规则的 proposal 草稿。
	maintenanceMode := len(feedbackItems) == 0
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "proposal_started",
		Message:   fmt.Sprintf("Generating profile proposal from %d feedback item(s)", len(feedbackItems)),
		Data: map[string]any{
			"feedback_items": len(feedbackItems),
			"maintenance":    maintenanceMode,
		},
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
	prompt := profileProposalPrompt(feedbackItems, compactProfileJSON, feedbackPayload, maintenanceMode)
	content, err := requestProfileModelJSONFunc(
		settings,
		"You compact scientific-literature profiles into maintainable change sets.",
		prompt,
		4200,
	)
	if err != nil {
		return ProposalDraft{}, err
	}
	attempt, err := buildCompactProposalAttempt(current, currentDocument, content)
	if err != nil {
		if !shouldRetryProfileParseWithThinkingDisabled(settings, err) {
			return ProposalDraft{}, err
		}
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "profile",
			Action:    "proposal_parse_fallback_started",
			Message:   "Retrying profile proposal generation with thinking disabled after invalid JSON.",
			Error:     err.Error(),
			Data:      map[string]any{"model": settings.ProfileModel},
		})
		fallbackSettings := settings
		fallbackSettings.ProfileThinking = "disabled"
		content, err = requestProfileModelJSONFunc(
			fallbackSettings,
			"You compact scientific-literature profiles into maintainable change sets.",
			prompt,
			4200,
		)
		if err != nil {
			return ProposalDraft{}, err
		}
		attempt, err = buildCompactProposalAttempt(current, currentDocument, content)
		if err != nil {
			return ProposalDraft{}, fmt.Errorf("parse profile proposal after thinking-disabled retry: %w", err)
		}
	}
	sourceFeedbackIDs := proposalSourceFeedbackIDs(feedbackItems)
	if audit := deterministicProposalAudit(currentDocument, feedbackItems, attempt); !audit.Accepted {
		return rejectedProposalDraft(currentDocument, settings.ProfileModel, sourceFeedbackIDs, audit), nil
	}
	validation, err := validateGeneratedProfileProposal(settings, currentDocument, feedbackItems, attempt)
	if err != nil {
		return ProposalDraft{}, err
	}
	if !validation.Accepted {
		if isHardValidationRejection(validation) {
			return rejectedProposalDraft(currentDocument, settings.ProfileModel, sourceFeedbackIDs, validation), nil
		}
		attempt, err = repairProfileProposalFromValidation(settings, currentDocument, feedbackItems, attempt, validation)
		if err != nil {
			return ProposalDraft{}, err
		}
		if audit := deterministicProposalAudit(currentDocument, feedbackItems, attempt); !audit.Accepted {
			return rejectedProposalDraft(currentDocument, settings.ProfileModel, sourceFeedbackIDs, audit), nil
		}
		validation, err = validateGeneratedProfileProposal(settings, currentDocument, feedbackItems, attempt)
		if err != nil {
			return ProposalDraft{}, err
		}
		if !validation.Accepted {
			validation.HardRejected = isHardValidationRejection(validation)
			return rejectedProposalDraft(currentDocument, settings.ProfileModel, sourceFeedbackIDs, validation), nil
		}
	}
	ruleDeltaMap, err := proposalDeltaToMap(defaultProposalDelta(attempt.Summary))
	if err != nil {
		return ProposalDraft{}, err
	}
	draft := ProposalDraft{
		BaseProfileVersion: currentDocument.Meta.Version,
		Summary:            attempt.Summary,
		ProposedProfile:    attempt.ProposedProfile,
		Changes:            attempt.Changes,
		RuleDelta:          ruleDeltaMap,
		Model:              settings.ProfileModel,
		SourceFeedbackIDs:  sourceFeedbackIDs,
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "proposal_completed",
		Message:   "Generated profile proposal.",
		Data: map[string]any{
			"feedback_items": len(feedbackItems),
			"model":          settings.ProfileModel,
			"summary":        attempt.Summary,
			"changes":        len(attempt.Changes),
			"validation":     validation.Summary,
		},
	})
	return draft, nil
}

func proposalSourceFeedbackIDs(feedbackItems []FeedbackProposalContext) []int64 {
	ids := make([]int64, 0, len(feedbackItems))
	for _, item := range feedbackItems {
		ids = append(ids, item.FeedbackID)
	}
	return ids
}

func rejectedProposalDraft(currentDocument profileDocument, model string, sourceFeedbackIDs []int64, validation ProposalValidationResult) ProposalDraft {
	if !validation.Accepted {
		validation.HardRejected = isHardValidationRejection(validation)
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "profile",
		Action:    "proposal_rejected_handled",
		Message:   validationFailureSummary(validation),
		Data: map[string]any{
			"hard_rejected": validation.HardRejected,
			"summary":       validation.Summary,
			"issues":        validation.BlockingIssues,
			"fixes":         validation.RequiredFixes,
		},
	})
	return ProposalDraft{
		BaseProfileVersion: currentDocument.Meta.Version,
		Summary:            validation.Summary,
		Model:              model,
		SourceFeedbackIDs:  sourceFeedbackIDs,
		Rejection:          &validation,
	}
}

func deterministicProposalAudit(currentDocument profileDocument, feedbackItems []FeedbackProposalContext, attempt compactProposalAttempt) ProposalValidationResult {
	if missingProtectedBoundary(currentDocument.RelevanceRules.Unrelated, attempt.ProposedDocument.RelevanceRules.Unrelated, "surface adjacency") {
		return ProposalValidationResult{
			Accepted:     false,
			HardRejected: true,
			Summary:      "Profile proposal rejected by deterministic safety audit.",
			BlockingIssues: []string{
				"Proposed profile removes the current surface-adjacency unrelated boundary.",
			},
			RequiredFixes: []string{
				"Preserve an explicit unrelated rule for surface DNA/RNA/probe adjacency when the core contribution is outside nucleic acid chemistry or nucleic-acid-enzyme engineering.",
			},
		}
	}
	if mostlyDirection(feedbackItems, "indirect", "unrelated") && proposalBroadensIndirect(attempt.Changes) {
		return ProposalValidationResult{
			Accepted:     false,
			HardRejected: true,
			Summary:      "Profile proposal rejected by deterministic safety audit.",
			BlockingIssues: []string{
				"Mostly indirect -> unrelated feedback cannot be handled by broadening indirect.",
			},
			RequiredFixes: []string{
				"Remove the indirect expansion and strengthen unrelated or narrow indirect instead.",
			},
		}
	}
	return ProposalValidationResult{Accepted: true, Summary: "Deterministic audit passed."}
}

func missingProtectedBoundary(currentRules []string, proposedRules []string, phrase string) bool {
	needle := strings.ToLower(strings.TrimSpace(phrase))
	if needle == "" || !rulesContainPhrase(currentRules, needle) {
		return false
	}
	return !rulesContainPhrase(proposedRules, needle)
}

func rulesContainPhrase(rules []string, phrase string) bool {
	for _, rule := range rules {
		if strings.Contains(strings.ToLower(rule), phrase) {
			return true
		}
	}
	return false
}

func mostlyDirection(feedbackItems []FeedbackProposalContext, original string, corrected string) bool {
	if len(feedbackItems) == 0 {
		return false
	}
	matches := 0
	for _, item := range feedbackItems {
		if strings.EqualFold(item.OriginalRelevance, original) && strings.EqualFold(item.CorrectedRelevance, corrected) {
			matches++
		}
	}
	return matches > len(feedbackItems)/2
}

func proposalBroadensIndirect(changes []ProposalChange) bool {
	for _, change := range changes {
		if change.Section != ProposalSectionIndirectRule {
			continue
		}
		if change.Operation != ProposalOperationAdd && change.Operation != ProposalOperationRewrite && change.Operation != ProposalOperationMerge {
			continue
		}
		text := strings.ToLower(strings.Join(change.TextAfter, " "))
		for _, phrase := range []string{
			"could impact",
			"potentially useful",
			"potential usefulness",
			"future applicability",
			"nucleic-acid-adjacent",
			"surface adjacency",
		} {
			if strings.Contains(text, phrase) {
				return true
			}
		}
	}
	return false
}

func logInitialProposalCompleted(summary string, document profileDocument, model string) error {
	compact := compactDocument(document)
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "bootstrap_completed",
		Message:   "Generated initial classification profile proposal.",
		Data: map[string]any{
			"model":        model,
			"summary":      summary,
			"topic_tags":   len(compact.TopicTaxonomy),
			"few_shots":    len(compact.FewShots),
			"profile_name": compact.Meta.Name,
		},
	})
	return nil
}

func defaultProposalDelta(summary string) proposalDelta {
	return proposalDelta{
		Summary:                strings.TrimSpace(summary),
		DirectRuleAdditions:    []string{},
		IndirectRuleAdditions:  []string{},
		UnrelatedRuleAdditions: []string{},
		ScopeRewrite:           nil,
		TagAdditions:           []topicDefinition{},
		TagRemovals:            []string{},
	}
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

func feedbackDirectionSummaryJSON(items []FeedbackProposalContext) string {
	counts := map[string]int{}
	for _, item := range items {
		key := strings.TrimSpace(item.OriginalRelevance) + " -> " + strings.TrimSpace(item.CorrectedRelevance)
		counts[key]++
	}
	data, err := json.MarshalIndent(counts, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
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
	}
	if includeFewShots && len(compact.FewShots) > 0 {
		payload["few_shots"] = compact.FewShots
	}
	return payload
}

func withoutTopicChanges(changes []ProposalChange) []ProposalChange {
	filtered := make([]ProposalChange, 0, len(changes))
	for _, change := range changes {
		if change.Section == ProposalSectionTopic {
			continue
		}
		filtered = append(filtered, change)
	}
	return filtered
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
	compact := compactDocument(document)
	return proposalDelta{
		Summary:                strings.TrimSpace(summary),
		DirectRuleAdditions:    compact.RelevanceRules.Direct,
		IndirectRuleAdditions:  compact.RelevanceRules.Indirect,
		UnrelatedRuleAdditions: compact.RelevanceRules.Unrelated,
		ScopeRewrite:           stringPointer(compact.Scope),
		TagAdditions:           []topicDefinition{},
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
