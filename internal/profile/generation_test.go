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
	if !strings.Contains((*prompts)[0], "mostly indirect -> unrelated") {
		t.Fatalf("generator prompt missing feedback-direction guard: %s", (*prompts)[0])
	}
	for _, fragment := range []string{
		"Do not optimize for the fewest rules; optimize for decision clarity",
		"Compress repeated reasoning, not noun lists",
		"Do not merge rules when the merged rule becomes a keyword net",
		"Scope rewrites are allowed when repeated same-direction feedback shows stable interest drift",
	} {
		if !strings.Contains((*prompts)[0], fragment) {
			t.Fatalf("generator prompt missing %q: %s", fragment, (*prompts)[0])
		}
	}
	if !strings.Contains((*prompts)[1], "protein or peptide probes") || !strings.Contains((*prompts)[1], "drug-target foundation model") {
		t.Fatalf("validator prompt missing regression guard or feedback context: %s", (*prompts)[1])
	}
	for _, fragment := range []string{
		"Scope is rewritten as a broad topic catalog",
		"Scope is expanded from a single ambiguous feedback item",
		"Clear negative boundaries are replaced by long noun-list rules",
	} {
		if !strings.Contains((*prompts)[1], fragment) {
			t.Fatalf("validator prompt missing %q: %s", fragment, (*prompts)[1])
		}
	}
}

func TestGenerateProfileProposalRepairsRejectedValidation(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileProposalFixture("change-1", ProposalSectionIndirectRule, ProposalOperationAdd, []string{}, []string{"Papers that could impact nucleic acid-related technologies are indirect."}),
		`{"accepted":false,"summary":"Broadens indirect.","blocking_issues":["Mostly indirect -> unrelated feedback cannot broaden indirect."],"required_fixes":["Remove future-applicability indirect rule and add unrelated negative boundaries."]}`,
		profileProposalFixture("change-2", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Protein or peptide probes, ML drug-target models, DNA repair mechanisms, RNA-binding machinery, and metabolomics papers are unrelated unless they develop nucleic acid chemistry or nucleic-acid enzyme engineering."}),
		`{"accepted":true,"summary":"Repair covers negative boundaries.","blocking_issues":[],"required_fixes":[]}`,
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
	if !strings.Contains((*prompts)[2], "Validator result") || !strings.Contains((*prompts)[2], "Remove future-applicability indirect rule") {
		t.Fatalf("repair prompt missing validator fixes: %s", (*prompts)[2])
	}
	for _, fragment := range []string{
		"Optimize for decision clarity rather than the fewest rules",
		"Rewrite scope only as a short decision policy",
		"single unnoted feedback item supports a scope expansion",
		"Do not replace clear negative boundaries with a keyword net",
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
		profileProposalFixture("change-1", ProposalSectionIndirectRule, ProposalOperationAdd, []string{}, []string{"Papers that could impact nucleic acid-related technologies are indirect."}),
		`{"accepted":false,"summary":"Broadens indirect.","blocking_issues":["Mostly indirect -> unrelated feedback cannot broaden indirect."],"required_fixes":["Remove indirect expansion."]}`,
		profileProposalFixture("change-2", ProposalSectionIndirectRule, ProposalOperationAdd, []string{}, []string{"Potentially useful nucleic-acid-adjacent platforms are indirect."}),
		`{"accepted":false,"summary":"Still broadens indirect.","blocking_issues":["Potential usefulness remains an indirect criterion."],"required_fixes":["Strengthen unrelated instead."]}`,
	}
	_ = stubProfileModelResponses(t, responses...)

	_, err := GenerateProfileProposal(profileGenerationSettings(), current, profileGenerationFeedbackFixtures())
	if err == nil {
		t.Fatal("expected validator rejection after repair")
	}
	if !strings.Contains(err.Error(), "profile proposal rejected by validator after repair") {
		t.Fatalf("unexpected error: %v", err)
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

func TestGenerateProfileProposalRepairsSingleFeedbackScopeExpansion(t *testing.T) {
	current := profileGenerationCurrentFixture()
	responses := []string{
		profileScopeProposalFixture(
			"Focus on DNA, RNA, aptamer, probe, biosensor, device, platform, nanostructure, CRISPR, sequencing, polymerase, and engineering papers.",
			"Expand scope from a single ambiguous feedback item.",
		),
		`{"accepted":false,"summary":"Scope expansion is not supported.","blocking_issues":["Single unnoted feedback item cannot justify scope expansion.","Scope is a noun list."],"required_fixes":["Keep scope unchanged and add a narrow rule instead."]}`,
		profileProposalFixture("change-2", ProposalSectionUnrelatedRule, ProposalOperationAdd, []string{}, []string{"Surface nucleic-acid adjacency is unrelated unless the core contribution is nucleic acid chemistry or nucleic-acid-enzyme engineering."}),
		`{"accepted":true,"summary":"Repaired with rule-only change.","blocking_issues":[],"required_fixes":[]}`,
	}
	prompts := stubProfileModelResponses(t, responses...)

	draft, err := GenerateProfileProposal(profileGenerationSettings(), current, []FeedbackProposalContext{
		{FeedbackID: 20, PaperID: 30, PaperTitle: "Ambiguous biosensor platform", OriginalRelevance: "indirect", CorrectedRelevance: "unrelated"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if testString(draft.ProposedProfile["scope"]) != "Nucleic acid chemistry and nucleic-acid enzyme engineering." {
		t.Fatalf("repair should keep original scope, got %s", testString(draft.ProposedProfile["scope"]))
	}
	if len(*prompts) != 4 || !strings.Contains((*prompts)[2], "Keep scope unchanged") {
		t.Fatalf("repair prompt did not receive scope fix: %#v", prompts)
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
