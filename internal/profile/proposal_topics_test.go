package profile

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProposalTopicOpinionSurvivesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, field, want string
	}{
		{"omitted", "", "t-alpha"},
		{"null", `,"topics_after":null`, "t-alpha"},
		{"clear", `,"topics_after":[]`, ""},
		{"retag", `,"topics_after":["Beta"]`, "t-beta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changes, err := ValidateProposalChangesBytes([]byte(fmt.Sprintf(`[{"id":"edit","section":"direct_rule","operation":"rewrite","summary":"Refine rule","rationale":"Feedback","text_before":["Original"],"text_after":["Revised"]%s}]`, tc.field)))
			if err != nil {
				t.Fatal(err)
			}
			// Proposal persistence and API validation serialize and normalize the change again.
			changes, err = ValidateProposalChanges(changes)
			if err != nil {
				t.Fatal(err)
			}
			changes, err = FinalizeProposalChanges(changes, []string{"edit"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			base := profileDocument{
				Meta:  profileMeta{Name: "Test", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(), SourceDescription: "Test"},
				Scope: "Test rules", TopicTaxonomy: []topicDefinition{{ID: "t-alpha", Label: "Alpha"}, {ID: "t-beta", Label: "Beta"}},
				RelevanceRules: relevanceRules{Direct: []classificationRule{{Text: "Original", TopicIDs: []string{"t-alpha"}}}},
			}
			current, _, err := compactDocumentMap(base)
			if err != nil {
				t.Fatal(err)
			}
			applied, _, err := PrepareAppliedProfileFromChanges(current, changes, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			document, err := parseDocumentMap(applied)
			if err != nil {
				t.Fatal(err)
			}
			got := document.RelevanceRules.Direct
			if len(got) != 1 || got[0].Text != "Revised" || strings.Join(got[0].TopicIDs, ",") != tc.want {
				t.Fatalf("unexpected rewritten rules: %#v", got)
			}
		})
	}
}

func TestProposalTopicMergePreservesCommonTagAndRejectsAmbiguity(t *testing.T) {
	base := []classificationRule{{Text: "A", TopicIDs: []string{"t-alpha"}}, {Text: "B", TopicIDs: []string{"t-alpha"}}}
	change := ProposalChange{ID: "merge", Operation: ProposalOperationMerge, TextBefore: []string{"A", "B"}, TextAfter: []string{"Merged"}}
	tags, err := ruleChangeTopicTags(base, change, nil)
	if err != nil || strings.Join(tags, ",") != "t-alpha" {
		t.Fatalf("tags=%v err=%v", tags, err)
	}
	base[1].TopicIDs = nil
	if _, err := ruleChangeTopicTags(base, change, nil); err == nil {
		t.Fatal("ambiguous merge must fail")
	}
	change.TopicsAfter = []string{}
	if tags, err := ruleChangeTopicTags(base, change, nil); err != nil || len(tags) != 0 {
		t.Fatalf("explicit clear: tags=%v err=%v", tags, err)
	}
}
