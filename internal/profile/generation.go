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

func profileProposalPrompt(feedbackItems []FeedbackProposalContext, compactProfileJSON []byte, feedbackPayload []byte, maintenanceMode bool) string {
	instructionHeader := "Compact and refine the current scientific-literature classification profile using the human feedback below."
	contextHeading := "Human feedback:"
	behaviorLine := "- Use the feedback to sharpen boundaries and reduce future mistakes."
	if maintenanceMode {
		instructionHeader = "Compact and refine the current scientific-literature classification profile using maintenance mode because no new human feedback is available."
		contextHeading = "Maintenance context:"
		behaviorLine = "- There is no new feedback in this run. Focus on profile hygiene: merge overlapping rules, remove stale residue, and compress old object-level leftovers into cleaner reason-level rules."
	}
	return strings.TrimSpace(fmt.Sprintf(`
%s

Requirements:
- Return valid JSON only.
- Return both:
  1. one short summary
  2. a structured changes array
- The application will build the proposed profile from changes; do not rely on a hand-written proposed_profile to carry behavior.
- Every change operation must be exactly one of: add, remove, rewrite, merge.
- Do not invent operations such as restore, keep, retain, or update; express restored boundaries as add or rewrite changes.

Feedback error-type workflow:
- Before proposing changes, classify each feedback item into one error type in your private analysis.
- Error types are: surface-term false positive, indirect too broad, unrelated boundary missing, direct boundary missing, scope drift, and ambiguous or insufficient evidence.
- Propose changes only for repeated error types or explicit feedback notes that show a changed interest.
- For indirect -> unrelated feedback, default repairs are: strengthen unrelated exclusions, narrow indirect entry criteria, and preserve direct/indirect decision axes.
- Do not merge rules unless the same error type affects the same decision axis.
- If feedback conflicts with an older rule, the feedback direction wins. Resolve the conflict explicitly with rewrite/remove instead of keeping both sides.

Rule style:
- Optimize for classification clarity and stable future decisions, not for fewer rules.
- One rule should answer one classification question.
- Do not collapse different decision axes into one rule.
- Compress repeated reasoning, not noun lists.
- Prefer 2-3 clear indirect rules over one comprehensive indirect rule.
- Keep or increase rule count when needed to preserve distinct decision axes.
- Avoid long enumerations of objects, technologies, or assays.
- If a rule needs examples, keep them short and subordinate to the reason.
- Do not replace clear boundaries with keyword nets or broad umbrella rules.
- Potential future applicability, possible usefulness, or "could impact nucleic acid-related technologies" is not enough for indirect relevance.

Merge policy:
- Prefer rewrite when one existing rule can be clarified.
- Use merge only when old rules share the same decision test and differ only by wording or examples.
- A merge is allowed only when old rules share the same decision test.
- Do not merge enzyme characterization, nucleic-acid substrate/readout context, and selection/screening platform relevance into one indirect rule.
- A 3 -> 1 indirect rewrite is invalid when the source rules represent different classification axes.
- If a proposed merge already covers a boundary, do not also add a duplicate rule in different wording.
- A pure add should be rare and used only when the current profile is missing an entire decision-axis boundary.

Protected boundaries:
- Before deleting or merging an unrelated rule, preserve the negative boundary it carried and state which replacement rule now covers that boundary.
- Preserve protected unrelated boundaries unless feedback explicitly and repeatedly contradicts them.
- If a current unrelated rule says surface adjacency alone is unrelated, every rewrite or merge touching that rule must preserve that decision in equally clear wording.
- Do not replace protected negative boundaries with vague generalities such as "platform papers are unrelated"; keep the reason and priority test explicit.

Scope and fields:
- Do not replace a specific scientific-interest profile with a generic
  or placeholder profile.
- Keep direct/indirect/unrelated as the only relevance labels.
- Do not modify few_shots.
- Only touch scope and relevance rules.
- Scope rewrites are allowed when repeated same-direction feedback shows stable interest drift or feedback notes explicitly describe a changed interest.
- Scope rewrites must be short decision policies, not broad catalogs or technology lists.
- A single feedback item without an explicit note should normally change rules, not scope.
%s

Current compact profile context:
%s

Feedback direction summary:
%s

%s
%s

Bad vs good abstraction examples:
- Bad: directly rewrite the profile before deciding whether feedback is a surface-term false positive, indirect-too-broad error, missing unrelated boundary, direct-boundary miss, scope drift, or ambiguous evidence.
- Good: group feedback by error type first, then propose the smallest rule changes that address repeated error types.
- Bad: treat indirect -> unrelated feedback as a reason to add broader indirect criteria.
- Good: for indirect -> unrelated feedback, strengthen unrelated exclusions, narrow indirect entry criteria, and preserve direct/indirect decision axes.
- Bad: merge enzyme characterization, nucleic-acid substrate/readout context, and selection/screening platform relevance into one indirect sentence.
- Good: keep separate indirect rules for nucleic-acid-acting enzyme characterization that directly informs engineering or substrate specificity; close nucleic-acid substrate/probe/readout context that informs chemistry or substrate design; and selection/screening platforms explicitly demonstrated on nucleic-acid-acting enzymes, aptamers, XNA/TNA systems, or nucleic-acid substrate engineering.
- Bad: change three clear indirect rules into one broad rule about close methodological or mechanistic context.
- Good: keep 2-3 clear rules when they answer different classification questions.
- Bad: rewrite "Surface adjacency alone is unrelated" into "General platform papers are unrelated" and lose the explicit priority rule.
- Good: preserve the boundary with wording such as "If nucleic acid terms are only recognition elements, analytes, payloads, readouts, validation tools, or surface adjacency, classify as unrelated unless the core contribution is nucleic acid chemistry or nucleic-acid-enzyme engineering."
- Bad: create one merge change and one add change that both say the same boundary in slightly different words.
- Good: either produce one rewrite/merge that absorbs the boundary, or produce one truly non-overlapping add if a separate boundary is genuinely missing.
- Bad: say DNA/RNA/aptamer/probe/biosensor/device/platform/nanostructure papers are indirect because they are nucleic-acid-adjacent.
- Good: if the nucleic acid is only a recognition element, analyte, payload, or readout and the core innovation is a material, device, diagnostic, ML model, peptide/protein probe, or biological mechanism, classify as unrelated.

Return:
- summary: one short summary of what changed
- changes: a list of per-change items using add/remove/rewrite/merge
- consolidate related feedback only when they share the same error type and decision test
- prefer rewrite and merge changes only when they preserve all distinct decision axes
- every change must include before/after content, rationale, and source ids
- text_before/text_after are only for scope and relevance rules
- use empty source_feedback_ids and source_paper_ids when running maintenance mode without feedback
- do not create a few_shot section in changes
- do not create topic changes

Required JSON shape:
%s
`, instructionHeader, behaviorLine, string(compactProfileJSON), feedbackDirectionSummaryJSON(feedbackItems), contextHeading, string(feedbackPayload), compactProposalContract()))
}

func buildCompactProposalAttempt(current map[string]any, currentDocument profileDocument, content string) (compactProposalAttempt, error) {
	summary, changes, err := coerceCompactProposal(content)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	changes = withoutTopicChanges(changes)
	if len(changes) == 0 {
		return compactProposalAttempt{}, fmt.Errorf("model generated a profile proposal without any actionable changes")
	}
	proposedProfile, err := BuildProposedProfileFromChanges(current, changes)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	proposedDocument, err := parseDocumentMap(proposedProfile)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	if reason := destructiveRevisionReason(currentDocument, proposedDocument); reason != "" {
		return compactProposalAttempt{}, fmt.Errorf("model generated an unsafe profile proposal. %s Current profile was kept unchanged.", reason)
	}
	return compactProposalAttempt{
		Summary:          summary,
		ProposedProfile:  proposedProfile,
		ProposedDocument: proposedDocument,
		Changes:          changes,
	}, nil
}

func validateGeneratedProfileProposal(settings config.Settings, currentDocument profileDocument, feedbackItems []FeedbackProposalContext, attempt compactProposalAttempt) (ProposalValidationResult, error) {
	// 用第二次模型调用审查 proposal，避免压缩时放大已有错分边界。
	prompt, err := proposalValidationPrompt(currentDocument, feedbackItems, attempt)
	if err != nil {
		return ProposalValidationResult{}, err
	}
	content, err := requestProfileModelJSONFunc(
		settings,
		"You audit scientific-literature classification profile proposals for regression risk.",
		prompt,
		2200,
	)
	if err != nil {
		return ProposalValidationResult{}, err
	}
	result, err := coerceProposalValidationResult(content)
	if err != nil {
		if !shouldRetryProfileParseWithThinkingDisabled(settings, err) {
			return ProposalValidationResult{}, fmt.Errorf("parse profile proposal validation: %w", err)
		}
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "profile",
			Action:    "proposal_validation_parse_fallback_started",
			Message:   "Retrying proposal validation with thinking disabled after invalid JSON.",
			Error:     err.Error(),
			Data:      map[string]any{"model": settings.ProfileModel},
		})
		fallbackSettings := settings
		fallbackSettings.ProfileThinking = "disabled"
		content, err = requestProfileModelJSONFunc(
			fallbackSettings,
			"You audit scientific-literature classification profile proposals for regression risk.",
			prompt,
			2200,
		)
		if err != nil {
			return ProposalValidationResult{}, err
		}
		result, err = coerceProposalValidationResult(content)
		if err != nil {
			return ProposalValidationResult{}, fmt.Errorf("parse profile proposal validation after thinking-disabled retry: %w", err)
		}
	}
	if !result.Accepted {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "profile",
			Action:    "proposal_validation_rejected",
			Message:   validationFailureSummary(result),
			Data: map[string]any{
				"summary": result.Summary,
				"issues":  result.BlockingIssues,
				"fixes":   result.RequiredFixes,
			},
		})
	}
	return result, nil
}

func repairProfileProposalFromValidation(settings config.Settings, currentDocument profileDocument, feedbackItems []FeedbackProposalContext, attempt compactProposalAttempt, validation ProposalValidationResult) (compactProposalAttempt, error) {
	// 按 validator 指出的阻断问题只重试一次，防止坏 proposal 入库。
	prompt, err := proposalRepairPrompt(currentDocument, feedbackItems, attempt, validation)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	content, err := requestProfileModelJSONFunc(
		settings,
		"You repair rejected scientific-literature profile proposals by applying validator fixes.",
		prompt,
		4200,
	)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	currentMap, _, err := compactDocumentMap(currentDocument)
	if err != nil {
		return compactProposalAttempt{}, err
	}
	repairedAttempt, err := buildCompactProposalAttempt(currentMap, currentDocument, content)
	if err != nil {
		if !shouldRetryProfileParseWithThinkingDisabled(settings, err) {
			return compactProposalAttempt{}, err
		}
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "profile",
			Action:    "proposal_repair_parse_fallback_started",
			Message:   "Retrying profile proposal repair with thinking disabled after invalid JSON.",
			Error:     err.Error(),
			Data:      map[string]any{"model": settings.ProfileModel},
		})
		fallbackSettings := settings
		fallbackSettings.ProfileThinking = "disabled"
		content, err = requestProfileModelJSONFunc(
			fallbackSettings,
			"You repair rejected scientific-literature profile proposals by applying validator fixes.",
			prompt,
			4200,
		)
		if err != nil {
			return compactProposalAttempt{}, err
		}
		repairedAttempt, err = buildCompactProposalAttempt(currentMap, currentDocument, content)
		if err != nil {
			return compactProposalAttempt{}, fmt.Errorf("parse profile proposal repair after thinking-disabled retry: %w", err)
		}
	}
	return repairedAttempt, nil
}

func proposalValidationPrompt(currentDocument profileDocument, feedbackItems []FeedbackProposalContext, attempt compactProposalAttempt) (string, error) {
	currentJSON, err := json.MarshalIndent(profilePromptPayload(currentDocument, false), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode current profile for validation: %w", err)
	}
	feedbackJSON, err := json.MarshalIndent(feedbackPromptPayload(feedbackItems), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode feedback for validation: %w", err)
	}
	proposedJSON, err := json.MarshalIndent(profilePromptPayload(attempt.ProposedDocument, false), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode proposed profile for validation: %w", err)
	}
	changesJSON, err := json.MarshalIndent(attempt.Changes, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode changes for validation: %w", err)
	}
	return strings.TrimSpace(fmt.Sprintf(`
Audit the proposed scientific-literature classification profile.

Return valid JSON only.
Set hard_rejected=true for unsafe profile behavior that should not be repaired by another model pass.
Set hard_rejected=false only for mechanical or schema-adjacent issues that are directionally safe to repair once.

Accept only if the proposal is likely to reduce the supplied feedback mistakes.
Reject when any blocking issue is present:
- The proposed profile makes any source feedback corrected label harder to explain.
- The proposal is not grounded in repeated feedback error types or explicit feedback notes.
- The proposal skips error-type analysis and performs broad profile cleanup or structural compaction instead.
- Indirect and unrelated rules cover the same boundary without a clear priority.
- The proposal removes high-value negative boundaries for ML/software, protein or peptide probes, DNA repair or RNA biology mechanisms, platform/device/diagnostic papers, or metabolomics/nucleotide-analyte papers.
- For a mostly indirect -> unrelated feedback batch, the proposal adds or broadens indirect instead of tightening indirect or strengthening unrelated.
- For indirect -> unrelated feedback, the proposal does not use the default repairs: strengthen unrelated exclusions, narrow indirect entry criteria, and preserve direct/indirect decision axes.
- Potential future applicability, possible usefulness, or "could impact nucleic acid-related technologies" is treated as enough for indirect relevance.
- The proposal optimizes for fewer rules at the cost of decision clarity.
- Scope is rewritten as a broad topic catalog, keyword list, or noun list instead of a short decision policy.
- Scope is expanded from a single ambiguous feedback item without an explicit note indicating changed interests.
- Clear negative boundaries are replaced by long noun-list rules or a keyword net.
- V25-style negative boundaries are deleted without equally clear replacement rules.
- The proposal collapses distinct decision axes into one umbrella rule.
- The proposal merges rules even though the same error type does not affect the same decision axis.
- A 3 -> 1 indirect rewrite merges enzyme characterization, nucleic-acid substrate/readout context, and selection/screening platform relevance even though they answer different classification questions.
- The proposal is shorter but less discriminative, even if protected unrelated boundaries are preserved.
- Hard rejection examples: removed key negative boundaries, broadened indirect for indirect -> unrelated feedback, noun-list scope, contradictory changes, or failure to explain corrected feedback.
- Soft rejection examples: missing rationale, overly verbose but directionally correct rules, unsupported operation wording, or schema-adjacent formatting that can be repaired without changing the decision direction.

Current compact profile:
%s

Feedback direction summary:
%s

Human feedback:
%s

Proposed compact profile:
%s

Proposed changes:
%s

Required JSON shape:
%s
`, string(currentJSON), feedbackDirectionSummaryJSON(feedbackItems), string(feedbackJSON), string(proposedJSON), string(changesJSON), proposalValidationContract())), nil
}

func proposalRepairPrompt(currentDocument profileDocument, feedbackItems []FeedbackProposalContext, attempt compactProposalAttempt, validation ProposalValidationResult) (string, error) {
	currentJSON, err := json.MarshalIndent(profilePromptPayload(currentDocument, false), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode current profile for repair: %w", err)
	}
	feedbackJSON, err := json.MarshalIndent(feedbackPromptPayload(feedbackItems), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode feedback for repair: %w", err)
	}
	proposedJSON, err := json.MarshalIndent(profilePromptPayload(attempt.ProposedDocument, false), "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode rejected profile for repair: %w", err)
	}
	changesJSON, err := json.MarshalIndent(attempt.Changes, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode rejected changes for repair: %w", err)
	}
	validationJSON, err := json.MarshalIndent(validation, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode validation result for repair: %w", err)
	}
	return strings.TrimSpace(fmt.Sprintf(`
Repair the rejected profile proposal by applying every required fix.

Requirements:
- Return valid JSON only.
- Return the same compact proposal shape as the original generator.
- Every change operation must be exactly one of: add, remove, rewrite, merge.
- Do not invent operations such as restore, keep, retain, or update; express restored boundaries as add or rewrite changes.
- Preserve the current profile's specific scientific scope.
- Optimize for decision clarity rather than the fewest rules.
- Compress repeated reasoning, not noun lists.
- Rewrite scope only as a short decision policy; do not turn it into a broad technology catalog.
- If only a single unnoted feedback item supports a scope expansion, keep scope unchanged and repair rules instead.
- Repair around the feedback error type that caused the rejection, not broad profile cleanup.
- For indirect -> unrelated feedback, repair by strengthening unrelated exclusions, narrowing indirect entry criteria, and preserving direct/indirect decision axes.
- If feedback is mostly indirect -> unrelated, tighten indirect or strengthen unrelated; do not broaden indirect.
- Keep negative boundaries for ML/software, protein or peptide probes, DNA repair or RNA biology mechanisms, platform/device/diagnostic papers, and metabolomics/nucleotide-analyte papers.
- Keep any current unrelated rule that says surface adjacency alone is unrelated, or rewrite it only into an equally explicit priority rule.
- Do not repair a rejected proposal by replacing protected negative boundaries with vague generalities.
- Do not rely on potential future applicability as an indirect criterion.
- Do not replace clear negative boundaries with a keyword net.
- If validator flags collapsed decision axes, split the rule back into separate decision-axis rules.
- Do not repair an axis-collapse rejection by adding examples to a broad umbrella rule.
- Keep enzyme characterization, nucleic-acid substrate/readout context, and selection/screening platform relevance separate when they answer different indirect classification questions.
- Do not merge rules unless the same error type affects the same decision axis.

Current compact profile:
%s

Feedback direction summary:
%s

Human feedback:
%s

Rejected proposed profile:
%s

Rejected changes:
%s

Validator result:
%s

Required JSON shape:
%s
`, string(currentJSON), feedbackDirectionSummaryJSON(feedbackItems), string(feedbackJSON), string(proposedJSON), string(changesJSON), string(validationJSON), compactProposalContract())), nil
}

func coerceProposalValidationResult(content string) (ProposalValidationResult, error) {
	data, err := extractJSONObjectBytes(content)
	if err != nil {
		return ProposalValidationResult{}, err
	}
	var result ProposalValidationResult
	if err := decodeStrict(data, &result); err != nil {
		return ProposalValidationResult{}, fmt.Errorf("parse profile proposal validation: %w", err)
	}
	result.Summary = normalizeText(result.Summary)
	result.BlockingIssues = normalizeRuleList(result.BlockingIssues)
	result.RequiredFixes = normalizeRuleList(result.RequiredFixes)
	if result.Summary == "" {
		if result.Accepted {
			result.Summary = "Proposal accepted."
		} else {
			result.Summary = "Proposal rejected."
		}
	}
	if !result.Accepted && len(result.BlockingIssues) == 0 && len(result.RequiredFixes) == 0 {
		return ProposalValidationResult{}, fmt.Errorf("rejected profile proposal validation must include blocking_issues or required_fixes")
	}
	if !result.Accepted {
		result.HardRejected = isHardValidationRejection(result)
	}
	return result, nil
}

func isHardValidationRejection(result ProposalValidationResult) bool {
	if result.Accepted {
		return false
	}
	if result.HardRejected {
		return true
	}
	text := strings.ToLower(validationFailureSummary(result))
	for _, phrase := range []string{
		"removed high-value negative",
		"removes high-value negative",
		"removes the current surface-adjacency",
		"removes the explicit surface-adjacency",
		"surface-adjacency unrelated",
		"broadened indirect",
		"broadens indirect",
		"cannot broaden indirect",
		"noun-list scope",
		"scope is a noun list",
		"keyword net",
		"contradictory",
		"hard rejection",
		"hard_rejected",
		"v25-style negative boundaries are deleted",
		"makes any source feedback corrected label harder to explain",
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func validationFailureSummary(result ProposalValidationResult) string {
	parts := []string{}
	if strings.TrimSpace(result.Summary) != "" {
		parts = append(parts, result.Summary)
	}
	parts = append(parts, result.BlockingIssues...)
	parts = append(parts, result.RequiredFixes...)
	if len(parts) == 0 {
		return "proposal validation rejected the generated profile"
	}
	return strings.Join(parts, "; ")
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

func shouldRetryProfileParseWithThinkingDisabled(settings config.Settings, err error) bool {
	if strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "disabled") {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid classification profile json") ||
		strings.Contains(message, "could not find a complete json object") ||
		strings.Contains(message, "parse compact profile proposal") ||
		strings.Contains(message, "parse profile proposal validation") ||
		strings.Contains(message, "unexpected end of json input")
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

func proposalValidationContract() string {
	return `{
  "accepted": true,
  "hard_rejected": false,
  "summary": "short validation summary",
  "blocking_issues": ["issue that must block saving the proposal"],
  "required_fixes": ["specific change needed before saving"]
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
