package profile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
)

func TestGenerateProfileProposalValidatesAcceptedProposal(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"ML/software papers are unrelated without nucleic acid chemistry."}),
		`{"accepted":true,"summary":"Looks safe.","blocking_issues":[],"required_fixes":[]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "Tighten unrelated boundaries" || len(draft.Changes) != 1 {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	if len(*prompts) != 2 {
		t.Fatalf("expected generator and validator prompts, got %d", len(*prompts))
	}
	if !strings.Contains((*prompts)[0], "For indirect -> unrelated feedback, default repairs are") {
		t.Fatalf("generator prompt missing feedback-direction guard: %s", (*prompts)[0])
	}
	for _, fragment := range []string{
		"Before proposing changes, classify each feedback item into one error type",
		"surface-term false positive, indirect too broad, unrelated boundary missing, direct boundary missing, scope drift",
		"Propose changes only for repeated error types",
		"For indirect -> unrelated feedback, default repairs are: strengthen unrelated exclusions, narrow indirect entry criteria",
		"Do not merge rules unless the same error type affects the same decision axis",
		"Do not collapse different decision axes into one rule",
		"Compress repeated reasoning, not noun lists",
		"Prefer 2-3 clear indirect rules over one comprehensive indirect rule",
		"group feedback by error type first",
		"Scope rewrites are allowed when repeated same-direction feedback shows stable interest drift",
		"If a current unrelated rule says surface adjacency alone is unrelated",
	} {
		if !strings.Contains((*prompts)[0], fragment) {
			t.Fatalf("generator prompt missing %q: %s", fragment, (*prompts)[0])
		}
	}
	if !strings.Contains((*prompts)[1], "protein or peptide probes") || !strings.Contains((*prompts)[1], "drug-target foundation model") {
		t.Fatalf("validator prompt missing regression guard or feedback context: %s", (*prompts)[1])
	}
	for _, fragment := range []string{
		"The proposal is not grounded in repeated feedback error types",
		"The proposal skips error-type analysis and performs broad profile cleanup",
		"For indirect -> unrelated feedback, the proposal does not use the default repairs",
		"Scope is rewritten as a broad topic catalog",
		"Scope is expanded from a single ambiguous feedback item",
		"Clear negative boundaries are replaced by long noun-list rules",
		"The proposal collapses distinct decision axes into one umbrella rule",
		"The proposal merges rules even though the same error type does not affect the same decision axis",
		"enzyme characterization, nucleic-acid substrate/readout context, and selection/screening platform relevance",
	} {
		if !strings.Contains((*prompts)[1], fragment) {
			t.Fatalf("validator prompt missing %q: %s", fragment, (*prompts)[1])
		}
	}
}

func TestGenerateProfileProposalRepairsRejectedValidation(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Surface-adjacent platform papers are unrelated without a core nucleic-acid-method contribution."}),
		`{"accepted":false,"hard_rejected":false,"summary":"Needs clearer rationale.","blocking_issues":["The rule is directionally safe but too terse."],"required_fixes":["Explain the corrected feedback boundary more explicitly."]}`,
		profileProposalFixture("change-2", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Protein or peptide probes, ML drug-target models, DNA repair mechanisms, RNA-binding machinery, and metabolomics papers are unrelated unless they develop nucleic acid chemistry or nucleic-acid enzyme engineering."}),
		`{"accepted":true,"hard_rejected":false,"summary":"Repair covers negative boundaries.","blocking_issues":[],"required_fixes":[]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Changes) != 1 || draft.Changes[0].ID != "change-2" {
		t.Fatalf("expected repaired change, got %#v", draft.Changes)
	}
	if len(*prompts) != 4 {
		t.Fatalf("expected generator, validator, repair, validator prompts, got %d", len(*prompts))
	}
	if !strings.Contains((*prompts)[2], "Validator result") || !strings.Contains((*prompts)[2], "Explain the corrected feedback boundary") {
		t.Fatalf("repair prompt missing validator fixes: %s", (*prompts)[2])
	}
	for _, fragment := range []string{
		"Repair around the feedback error type that caused the rejection",
		"For indirect -> unrelated feedback, repair by strengthening unrelated exclusions, narrowing indirect entry criteria",
		"Optimize for decision clarity rather than the fewest rules",
		"Rewrite scope only as a short decision policy",
		"single unnoted feedback item supports a scope expansion",
		"Do not replace clear negative boundaries with a keyword net",
		"Keep any current unrelated rule that says surface adjacency alone is unrelated",
		"If validator flags collapsed decision axes, split the rule back into separate decision-axis rules",
		"Do not repair an axis-collapse rejection by adding examples to a broad umbrella rule",
		"Do not merge rules unless the same error type affects the same decision axis",
	} {
		if !strings.Contains((*prompts)[2], fragment) {
			t.Fatalf("repair prompt missing %q: %s", fragment, (*prompts)[2])
		}
	}
}

func TestGenerateProfileProposalRepairPromptSplitsCollapsedIndirectAxes(t *testing.T) {
	current := profileGenerationCurrentWithIndirectAxesFixture()
	oldRules := profileGenerationIndirectAxisRules()
	collapsedRule := "The paper provides close methodological or mechanistic context that directly informs nucleic acid chemistry, nucleic-acid substrate design, or engineering of nucleic-acid-acting enzymes, including enzyme characterization, substrate/readout context, and selection or screening platforms."
	repairedRules := []string{
		"The paper characterizes a nucleic-acid-acting enzyme in a way that directly informs engineering, catalytic mechanism, fidelity, or substrate specificity.",
		"The paper provides close nucleic-acid substrate, modified-nucleotide, probe, aptamer, sequencing, or chemical-probing context that directly informs nucleic acid chemistry or substrate design.",
		"The paper develops a selection, display, or screening platform explicitly demonstrated on nucleic-acid-acting enzymes, aptamers, XNA/TNA systems, or nucleic-acid substrate engineering.",
	}
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionIndirectRule, ProposalOperationRewrite, oldRules, []string{collapsedRule}),
		`{"accepted":false,"hard_rejected":false,"summary":"Collapsed indirect decision axes.","blocking_issues":["The proposal collapses distinct decision axes into one umbrella rule."],"required_fixes":["Split enzyme characterization, substrate/readout context, and selection/screening platform relevance back into separate indirect rules."]}`,
		profileProposalFixture("change-2", ProposalSectionIndirectRule, ProposalOperationRewrite, oldRules, repairedRules),
		`{"accepted":true,"hard_rejected":false,"summary":"Indirect axes remain separate.","blocking_issues":[],"required_fixes":[]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Changes) != 1 || len(draft.Changes[0].TextAfter) != 3 {
		t.Fatalf("expected repaired split indirect rules, got %#v", draft.Changes)
	}
	if len(*prompts) != 4 {
		t.Fatalf("expected generator, validator, repair, validator prompts, got %d", len(*prompts))
	}
	for _, fragment := range []string{
		"Split enzyme characterization, substrate/readout context, and selection/screening platform relevance",
		"split the rule back into separate decision-axis rules",
		"Do not repair an axis-collapse rejection by adding examples to a broad umbrella rule",
	} {
		if !strings.Contains((*prompts)[2], fragment) {
			t.Fatalf("repair prompt missing %q: %s", fragment, (*prompts)[2])
		}
	}
}

func TestGenerateProfileProposalRetriesInvalidValidatorJSONWithThinkingDisabled(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"ML/software papers are unrelated without nucleic acid chemistry."}),
		"",
		`{"accepted":true,"summary":"Accepted after thinking-disabled retry.","blocking_issues":[],"required_fixes":[]}`,
	}
	prompts, settingsSeen := stubProfileModelResponsesWithSettings(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Summary != "Tighten unrelated boundaries" {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	if len(*prompts) != 3 {
		t.Fatalf("expected generator, validator, validator retry prompts, got %d", len(*prompts))
	}
	if (*settingsSeen)[1].ProfileThinking != "enabled" {
		t.Fatalf("first validator should use original thinking setting, got %#v", (*settingsSeen)[1].ProfileThinking)
	}
	if (*settingsSeen)[2].ProfileThinking != "disabled" {
		t.Fatalf("validator retry should disable thinking, got %#v", (*settingsSeen)[2].ProfileThinking)
	}
}

func TestGenerateProfileProposalRejectsAfterFailedRepair(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Surface-adjacent platform papers are unrelated without a core nucleic-acid-method contribution."}),
		`{"accepted":false,"hard_rejected":false,"summary":"Needs clearer rationale.","blocking_issues":["The rule is directionally safe but too terse."],"required_fixes":["Make the negative boundary explicit."]}`,
		profileProposalFixture("change-2", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Surface-adjacent platform papers are usually unrelated."}),
		`{"accepted":false,"hard_rejected":false,"summary":"Still too vague.","blocking_issues":["The repaired rule still lacks a clear decision reason."],"required_fixes":["Name the core contribution priority."]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection == nil || draft.Rejection.Accepted || draft.Rejection.HardRejected {
		t.Fatalf("expected handled soft rejection after repair, got %#v", draft.Rejection)
	}
	if len(*prompts) != 4 {
		t.Fatalf("expected generator, validator, repair, validator prompts, got %d", len(*prompts))
	}
}

func TestGenerateProfileProposalHardRejectsBroadenedIndirectWithoutRepair(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionIndirectRule, ProposalOperationAdd, []string{}, []string{"Papers that could impact nucleic acid-related technologies are indirect."}),
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection == nil || !draft.Rejection.HardRejected {
		t.Fatalf("expected hard rejection, got %#v", draft.Rejection)
	}
	if len(*prompts) != 1 {
		t.Fatalf("hard rejection should not call validator or repair, got %d prompts", len(*prompts))
	}
}

func TestGenerateProfileProposalHardValidatorRejectionDoesNotRepair(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Surface-adjacent platform papers are unrelated without a core nucleic-acid-method contribution."}),
		`{"accepted":false,"hard_rejected":true,"summary":"Removed key boundary.","blocking_issues":["The proposal removes high-value negative boundaries."],"required_fixes":["Preserve the negative boundaries."]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection == nil || !draft.Rejection.HardRejected {
		t.Fatalf("expected hard validator rejection, got %#v", draft.Rejection)
	}
	if len(*prompts) != 2 {
		t.Fatalf("hard validator rejection should not repair, got %d prompts", len(*prompts))
	}
}

func TestGenerateProfileProposalHardRejectsRemovedSurfaceBoundary(t *testing.T) {
	current := profileGenerationCurrentFixture()
	current["relevance_rules"].(map[string]any)["unrelated"] = []any{
		"General biology without nucleic acid chemistry.",
		"Surface adjacency alone is unrelated unless the core contribution is nucleic acid chemistry or nucleic-acid-enzyme engineering.",
	}
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationRewrite, []string{"Surface adjacency alone is unrelated unless the core contribution is nucleic acid chemistry or nucleic-acid-enzyme engineering."}, []string{"General platform papers are unrelated."}),
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection == nil || !draft.Rejection.HardRejected {
		t.Fatalf("expected removed boundary hard rejection, got %#v", draft.Rejection)
	}
	if len(*prompts) != 1 {
		t.Fatalf("deterministic hard rejection should not call validator, got %d prompts", len(*prompts))
	}
}

func TestGenerateProfileProposalAllowsRepeatedFeedbackScopeNarrowing(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileScopeProposalFixture(
			"Classify by core contribution. Indirect requires close evidence for nucleic-acid substrate design, nucleic acid chemistry, or nucleic-acid-enzyme engineering. Surface DNA/RNA/probe adjacency remains unrelated.",
			"Narrow scope around core contribution.",
		),
		`{"accepted":true,"summary":"Repeated indirect-to-unrelated feedback supports scope narrowing.","blocking_issues":[],"required_fixes":[]}`,
	}
	_ = stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	scope := testString(draft.ProposedProfile["scope"])
	if !strings.Contains(scope, "Classify by core contribution") || !strings.Contains(scope, "Surface DNA/RNA/probe adjacency remains unrelated") {
		t.Fatalf("unexpected narrowed scope: %s", scope)
	}
	if len(draft.Changes) != 1 || draft.Changes[0].Section != ProposalSectionScope {
		t.Fatalf("expected scope change, got %#v", draft.Changes)
	}
}

func TestGenerateProfileProposalHardRejectsSingleFeedbackScopeExpansion(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileScopeProposalFixture(
			"Focus on DNA, RNA, aptamer, probe, biosensor, device, platform, nanostructure, CRISPR, sequencing, polymerase, and engineering papers.",
			"Expand scope from a single ambiguous feedback item.",
		),
		`{"accepted":false,"summary":"Scope expansion is not supported.","blocking_issues":["Single unnoted feedback item cannot justify scope expansion.","Scope is a noun list."],"required_fixes":["Keep scope unchanged and add a narrow rule instead."]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, []FeedbackProposalContext{
		{FeedbackID: 20, PaperID: 30, PaperTitle: "Ambiguous biosensor platform", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection == nil || !draft.Rejection.HardRejected {
		t.Fatalf("expected hard scope rejection, got %#v", draft.Rejection)
	}
	if len(*prompts) != 2 {
		t.Fatalf("hard scope rejection should not repair, got %#v", prompts)
	}
}

func TestGenerateProfileProposalAcceptsChangesOnlyPayload(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalChangesOnlyFixture("change-1", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"ML/software papers are unrelated without nucleic acid chemistry."}),
		`{"accepted":true,"hard_rejected":false,"summary":"Looks safe.","blocking_issues":[],"required_fixes":[]}`,
	}
	_ = stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Rejection != nil || len(draft.Changes) != 1 {
		t.Fatalf("unexpected draft: %#v", draft)
	}
}

func stubProfileModelResponses(t *testing.T, responses ...string) *[]string {
	prompts, _ := stubProfileModelResponsesWithSettings(t, responses...)
	return prompts
}

func stubProfileModelResponsesWithSettings(t *testing.T, responses ...string) (*[]string, *[]config.Settings) {
	t.Helper()
	original := requestProfileModelJSONFunc
	prompts := []string{}
	settingsSeen := []config.Settings{}
	index := 0
	requestProfileModelJSONFunc = func(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int) (string, error) {
		prompts = append(prompts, userPrompt)
		settingsSeen = append(settingsSeen, settings)
		if index >= len(responses) {
			t.Fatalf("unexpected profile model call %d: %s", index+1, userPrompt)
		}
		value := responses[index]
		index++
		return value, nil
	}
	t.Cleanup(func() {
		requestProfileModelJSONFunc = original
	})
	return &prompts, &settingsSeen
}

func profileGenerationSettings() config.Settings {
	return config.Settings{
		ProfileModel:    "profile-model",
		ProfileThinking: "enabled",
	}
}

func profileGenerationCurrentFixture() map[string]any {
	return map[string]any{
		"meta": map[string]any{
			"name":               "Current",
			"version":            1,
			"created_at":         "2026-05-10T00:00:00Z",
			"updated_at":         "2026-05-10T00:00:00Z",
			"source_description": "current",
		},
		"scope": "Nucleic acid chemistry and nucleic-acid enzyme engineering.",
		"relevance_rules": map[string]any{
			"direct":    []any{"Nucleic acid chemistry."},
			"indirect":  []any{"Close mechanistic context for nucleic-acid enzyme engineering."},
			"unrelated": []any{"General biology without nucleic acid chemistry."},
		},
		"topic_taxonomy": []any{},
		"few_shots":      []any{},
	}
}

func profileGenerationCurrentWithIndirectAxesFixture() map[string]any {
	current := profileGenerationCurrentFixture()
	rules := profileGenerationIndirectAxisRules()
	current["relevance_rules"].(map[string]any)["indirect"] = []any{
		rules[0],
		rules[1],
		rules[2],
	}
	return current
}

func profileGenerationIndirectAxisRules() []string {
	return []string{
		"The paper characterizes a nucleic-acid-acting enzyme in a way that directly informs catalytic mechanism, fidelity, substrate recognition, inhibitor resistance, or future enzyme engineering.",
		"The paper provides close nucleic-acid substrate, modified-nucleotide, probe, aptamer, sequencing, or chemical-probing context without introducing new nucleic acid chemistry or enzyme engineering.",
		"The paper develops a selection, display, or screening platform explicitly demonstrated on nucleic-acid-acting enzymes, aptamers, XNA/TNA systems, or nucleic-acid substrate engineering.",
	}
}

func profileGenerationFeedbackFixtures() []FeedbackProposalContext {
	return []FeedbackProposalContext{
		{FeedbackID: 1, PaperID: 10, PaperTitle: "A 19F MRI/NIR-FL peptide nanoprobe", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
		{FeedbackID: 2, PaperID: 11, PaperTitle: "A drug-target foundation model", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
		{FeedbackID: 3, PaperID: 12, PaperTitle: "FBH1 reverses stalled replication forks", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
		{FeedbackID: 4, PaperID: 13, PaperTitle: "Engineered Brr1 in spliceosome machinery", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
		{FeedbackID: 5, PaperID: 14, PaperTitle: "Nanoflow LC-MS metabolomics including nucleotides", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
	}
}

func profileProposalFixture(changeID string, section string, operation string, before []string, after []string) string {
	change := ProposalChange{
		ID:                changeID,
		Section:           section,
		Operation:         operation,
		Summary:           "Tighten unrelated boundaries.",
		TextBefore:        before,
		TextAfter:         after,
		TopicBefore:       []topicDefinition{},
		TopicAfter:        []topicDefinition{},
		Rationale:         "The feedback requires core contribution boundaries instead of surface nucleic-acid adjacency.",
		SourceFeedbackIDs: []int64{1, 2, 3, 4, 5},
		SourcePaperIDs:    []int64{10, 11, 12, 13, 14},
		Status:            ProposalStatusProposed,
	}
	changeJSON, _ := jsonMarshal(change)
	return `{
  "summary":"Tighten unrelated boundaries",
  "proposed_profile":{
    "meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","source_description":"current"},
    "scope":"Nucleic acid chemistry and nucleic-acid enzyme engineering.",
    "relevance_rules":{"direct":["Nucleic acid chemistry."],"indirect":["Close mechanistic context for nucleic-acid enzyme engineering."],"unrelated":["General biology without nucleic acid chemistry."]},
    "topic_taxonomy":[],
    "few_shots":[]
  },
  "changes":[` + changeJSON + `]
}`
}

func profileProposalChangesOnlyFixture(changeID string, section string, operation string, before []string, after []string) string {
	change := ProposalChange{
		ID:                changeID,
		Section:           section,
		Operation:         operation,
		Summary:           "Tighten unrelated boundaries.",
		TextBefore:        before,
		TextAfter:         after,
		TopicBefore:       []topicDefinition{},
		TopicAfter:        []topicDefinition{},
		Rationale:         "The feedback requires core contribution boundaries instead of surface nucleic-acid adjacency.",
		SourceFeedbackIDs: []int64{1, 2, 3, 4, 5},
		SourcePaperIDs:    []int64{10, 11, 12, 13, 14},
		Status:            ProposalStatusProposed,
	}
	changeJSON, _ := jsonMarshal(change)
	return `{
  "summary":"Tighten unrelated boundaries",
  "changes":[` + changeJSON + `]
}`
}

func profileScopeProposalFixture(scope string, summary string) string {
	change := ProposalChange{
		ID:                "scope-change",
		Section:           ProposalSectionScope,
		Operation:         ProposalOperationRewrite,
		Summary:           summary,
		TextBefore:        []string{"Nucleic acid chemistry and nucleic-acid enzyme engineering."},
		TextAfter:         []string{scope},
		TopicBefore:       []topicDefinition{},
		TopicAfter:        []topicDefinition{},
		Rationale:         "The feedback pattern supports a short scope decision policy.",
		SourceFeedbackIDs: []int64{1, 2, 3, 4, 5},
		SourcePaperIDs:    []int64{10, 11, 12, 13, 14},
		Status:            ProposalStatusProposed,
	}
	changeJSON, _ := jsonMarshal(change)
	return `{
  "summary":"` + summary + `",
  "proposed_profile":{
    "meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","source_description":"current"},
    "scope":"` + scope + `",
    "relevance_rules":{"direct":["Nucleic acid chemistry."],"indirect":["Close mechanistic context for nucleic-acid enzyme engineering."],"unrelated":["General biology without nucleic acid chemistry."]},
    "topic_taxonomy":[],
    "few_shots":[]
  },
  "changes":[` + changeJSON + `]
}`
}

func jsonMarshal(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func testString(value any) string {
	text, _ := value.(string)
	return text
}
