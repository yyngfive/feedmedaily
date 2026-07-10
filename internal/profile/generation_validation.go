package profile

import (
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
	"strings"
)

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
