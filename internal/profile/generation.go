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
	BaseProfileVersion int
	Summary            string
	ProposedProfile    map[string]any
	Changes            []ProposalChange
	RuleDelta          map[string]any
	Model              string
	SourceFeedbackIDs  []int64
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
	instructionHeader := "Compact and refine the current scientific-literature classification profile using the human feedback below."
	contextHeading := "Human feedback:"
	behaviorLine := "- Use the feedback to sharpen boundaries and reduce future mistakes."
	if maintenanceMode {
		instructionHeader = "Compact and refine the current scientific-literature classification profile using maintenance mode because no new human feedback is available."
		contextHeading = "Maintenance context:"
		behaviorLine = "- There is no new feedback in this run. Focus on profile hygiene: merge overlapping rules, remove stale residue, and compress old object-level leftovers into cleaner reason-level rules."
	}
	prompt := strings.TrimSpace(fmt.Sprintf(`
%s

Requirements:
- Return valid JSON only.
- Return both:
  1. a compact proposed_profile
  2. a structured changes array
- First do a coverage check for every feedback item:
  - ask whether the corrected boundary can be captured by rewriting or merging existing rules
  - only add a brand-new rule when no existing rule can be broadened or merged to cover it safely
- Infer the shared reasons behind the corrected labels before writing rules.
- Use reason-first abstraction:
  - prefer rules about why a paper is direct, indirect, or unrelated
  - prefer high-level criteria such as where the core innovation sits
  - do not split rules by object family when multiple object families share the same reason
- Apply the same reason-first compaction standard to direct rules, not only indirect or unrelated rules.
- Prefer merge/remove/rewrite over pure add whenever an existing rule can be edited.
- Rewrite existing indirect/unrelated rules first when the new feedback reveals a broader boundary that can absorb older object-specific rules.
- If a proposed merge or rewrite already covers a reason-level boundary, do not also add a second rule that expresses the same boundary in different wording.
- Merge and add changes must not duplicate each other in substance.
- If new feedback conflicts with an older rule, the new feedback wins. Resolve the conflict explicitly with rewrite/remove instead of keeping both sides.
- When merging, preserve the salient coverage from the old rules. Do not drop boundary-defining content such as nanostructure, nano-assembly, platform, device, or other key qualifiers if they are still needed for future classification.
- A pure add should be rare and used only when the current profile is missing an entire reason-level boundary.
- If multiple candidate rules share the same why-indirect or why-unrelated logic, merge them into one broader rule instead of keeping separate object-level rules.
- If you add a new rule, explain in rationale:
  - which current rule was the closest match
  - why that rule was still insufficient
  - why rewrite/merge would lose a necessary boundary
- If you merge rules, explain in rationale:
  - which old boundaries were preserved
  - why no important coverage was lost
- Try to reduce the total number of relevance rules, or at least keep the total flat.
- The goal is not to record every example; the goal is to explain more feedback with fewer, stabler rules.
- Keep the profile compact and easier to maintain than the current version.
- Do not replace a specific scientific-interest profile with a generic
  or placeholder profile.
- Keep direct/indirect/unrelated as the only relevance labels.
- Do not modify few_shots.
- Only touch scope and relevance rules.
%s

Current compact profile context:
%s

%s
%s

Bad vs good abstraction examples:
- Bad: keep one unrelated rule for MOF/COF/HOF papers, then add another unrelated rule for nucleic-acid biosensors using gold nanoparticles or silicon nanowires.
- Good: rewrite or merge them into one broader reason-level rule when the shared reason is that the paper targets nucleic acids but the core innovation is in non-nucleic acid materials, device architecture, sensing platform, or nano-assembly, while the nucleic acid element is only a recognition element and is not itself chemically or methodologically developed.
- Bad: merge several nanomaterial/device rules into a shorter rule, but accidentally drop important coverage such as DNA nanostructure, nano-assembly, carbon nanotube functionalization, or platform-level boundaries that the old rules were carrying.
- Good: produce a broader merged rule only if it still preserves those salient boundaries in higher-level wording, or keep a narrower rewrite when that coverage would otherwise be lost.
- Bad: create one merge change and one add change that both say the same boundary in slightly different words.
- Good: either produce one rewrite/merge that absorbs the boundary, or produce one truly non-overlapping add if a separate boundary is genuinely missing.
- Bad: create separate rules for peptide biosensors, protein sensors, and aptamer-platform papers just because the surface objects differ.
- Good: abstract them into a higher-level boundary when the shared reason is that the recognition element is not the innovation center and the real novelty is in the material, platform, device, delivery system, or assay format rather than nucleic acid chemistry, directed evolution, proximity labeling, or engineering of nucleic acid-acting enzymes.
- Bad: add multiple direct rules for specific probes, assays, or constructs that all really mean "the nucleic acid chemistry or enzyme-engineering innovation is central".
- Good: rewrite or merge them into one reason-level direct rule that states the core direct boundary instead of enumerating object families.

Return:
- summary: one short summary of what changed
- proposed_profile: the compact full next profile
- changes: a list of per-change items using add/remove/rewrite/merge
- consolidate related feedback into fewer, broader changes when they point to the same reason-level boundary
- prefer rewrite and merge changes that absorb old object-specific rules into broader reason-based rules
- every change must include before/after content, rationale, and source ids
- text_before/text_after are only for scope and relevance rules
- use empty source_feedback_ids and source_paper_ids when running maintenance mode without feedback
- do not create a few_shot section in changes
- do not create topic changes

Required JSON shape:
%s
`, instructionHeader, behaviorLine, string(compactProfileJSON), contextHeading, string(feedbackPayload), compactProposalContract()))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You compact scientific-literature profiles into maintainable change sets.",
		prompt,
		4200,
	)
	if err != nil {
		return ProposalDraft{}, err
	}
	summary, changes, err := coerceCompactProposal(content)
	if err != nil {
		return ProposalDraft{}, err
	}
	changes = withoutTopicChanges(changes)
	if len(changes) == 0 {
		return ProposalDraft{}, fmt.Errorf("model generated a profile proposal without any actionable changes")
	}
	proposedProfile, err := BuildProposedProfileFromChanges(current, changes)
	if err != nil {
		return ProposalDraft{}, err
	}
	proposedDocument, err := parseDocumentMap(proposedProfile)
	if err != nil {
		return ProposalDraft{}, err
	}
	if reason := destructiveRevisionReason(currentDocument, proposedDocument); reason != "" {
		return ProposalDraft{}, fmt.Errorf("model generated an unsafe profile proposal. %s Current profile was kept unchanged.", reason)
	}
	ruleDeltaMap, err := proposalDeltaToMap(defaultProposalDelta(summary))
	if err != nil {
		return ProposalDraft{}, err
	}
	sourceFeedbackIDs := make([]int64, 0, len(feedbackItems))
	for _, item := range feedbackItems {
		sourceFeedbackIDs = append(sourceFeedbackIDs, item.FeedbackID)
	}
	draft := ProposalDraft{
		BaseProfileVersion: currentDocument.Meta.Version,
		Summary:            summary,
		ProposedProfile:    proposedProfile,
		Changes:            changes,
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
			"summary":        summary,
			"changes":        len(changes),
		},
	})
	return draft, nil
}

func requestProfileModelJSONBody(settings config.Settings, endpoint string, body []byte) (string, error) {
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
	content, err := requestProfileModelJSONBody(settings, endpoint, body)
	if err == nil || strings.TrimSpace(settings.ProfileThinking) == "" || strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "disabled") || !shouldRetryProfileWithoutThinking(err) {
		return content, err
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "profile",
		Action:    "thinking_fallback_started",
		Message:   "Retrying profile request with thinking disabled.",
		Error:     err.Error(),
		Data:      map[string]any{"model": settings.ProfileModel},
	})
	payload["thinking"] = map[string]string{"type": "disabled"}
	fallbackBody, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return "", fmt.Errorf("encode fallback profile generation request: %w", marshalErr)
	}
	content, fallbackErr := requestProfileModelJSONBody(settings, endpoint, fallbackBody)
	if fallbackErr != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "profile",
			Action:    "thinking_fallback_failed",
			Message:   "Retry with thinking disabled failed.",
			Error:     fallbackErr.Error(),
			Data:      map[string]any{"model": settings.ProfileModel},
		})
		return "", fallbackErr
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "thinking_fallback_completed",
		Message:   "Retry with thinking disabled succeeded.",
		Data:      map[string]any{"model": settings.ProfileModel},
	})
	return content, nil
}

func shouldRetryProfileWithoutThinking(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "deadline exceeded") ||
		strings.Contains(message, "thinking") ||
		strings.Contains(message, "reasoning") ||
		strings.Contains(message, "reasoner") ||
		strings.Contains(message, "504") ||
		strings.Contains(message, "502")
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

func coerceCompactProposal(content string) (string, []ProposalChange, error) {
	data, err := extractJSONObjectBytes(content)
	if err != nil {
		return "", nil, err
	}
	var payload struct {
		Summary         string           `json:"summary"`
		ProposedProfile json.RawMessage  `json:"proposed_profile"`
		Changes         []ProposalChange `json:"changes"`
	}
	if err := decodeStrict(data, &payload); err != nil {
		return "", nil, fmt.Errorf("parse compact profile proposal: %w", err)
	}
	summary := normalizeText(payload.Summary)
	if summary == "" {
		return "", nil, fmt.Errorf("compact profile proposal summary cannot be blank")
	}
	if len(payload.ProposedProfile) == 0 || string(payload.ProposedProfile) == "null" {
		return "", nil, fmt.Errorf("compact profile proposal must include proposed_profile")
	}
	var proposedProfile map[string]any
	if err := json.Unmarshal(payload.ProposedProfile, &proposedProfile); err != nil {
		return "", nil, fmt.Errorf("parse compact profile proposal proposed_profile: %w", err)
	}
	if _, err := ValidateMap(proposedProfile); err != nil {
		return "", nil, fmt.Errorf("validate compact profile proposal proposed_profile: %w", err)
	}
	changes, err := ValidateProposalChanges(payload.Changes)
	if err != nil {
		return "", nil, err
	}
	return summary, changes, nil
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
  "topic_taxonomy": [],
  "few_shots": []
}`
}

func compactProposalContract() string {
	return `{
  "summary": "short summary of what changed",
  "proposed_profile": {
    "meta": {
      "name": "same profile name",
      "version": 2,
      "created_at": "ISO-8601 datetime",
      "updated_at": "ISO-8601 datetime",
      "source_description": "same source description"
    },
    "scope": "rewritten scope if needed",
    "relevance_rules": {
      "direct": ["compact direct rule"],
      "indirect": ["compact indirect rule"],
      "unrelated": ["compact unrelated rule"]
    },
    "topic_taxonomy": [],
    "few_shots": []
  },
  "changes": [
    {
      "id": "change_id",
      "section": "direct_rule",
      "operation": "merge",
      "summary": "merge overlapping direct rules",
      "text_before": ["old rule 1", "old rule 2"],
      "text_after": ["merged replacement rule"],
      "topic_before": [],
      "topic_after": [],
      "rationale": "why this compaction improves future classification",
      "source_feedback_ids": [],
      "source_paper_ids": [],
      "status": "proposed"
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
