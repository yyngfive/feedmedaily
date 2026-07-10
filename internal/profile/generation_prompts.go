package profile

import (
	"fmt"
	"strings"
)

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
