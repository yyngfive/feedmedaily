package sqlite

import (
	"github.com/yyngfive/scirssagent/internal/profile"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyAndRejectProfileProposalState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	seedMutableFixture(t, db)
	execSQLite(t, db, `
INSERT INTO feedback (
  id, paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  10, 1, 'indirect', 'direct', 'Feedback', 'open', 0, '2026-05-16T03:00:00Z'
);
INSERT INTO profile_proposals (
  id, summary, proposed_profile_json, rule_delta_json, source_feedback_ids_json, model, state, created_at
) VALUES (
  11, 'Proposal summary',
  '{"meta":{"name":"Proposal","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"proposal"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}',
  '{"summary":"Proposal summary","direct_rule_additions":["RNA chemistry"],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}',
  '[10]', 'deepseek-v4-pro', 'pending', '2026-05-16T04:00:00Z'
);
`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.ApplyProfileProposalState(11, 3, map[string]any{
		"meta": map[string]any{
			"name":               "Proposal",
			"version":            3,
			"created_at":         "2026-05-16T00:00:00Z",
			"updated_at":         "2026-05-16T07:00:00Z",
			"source_description": "proposal",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{},
		"few_shots":      []any{},
	}, []profile.ProposalChange{}, time.Date(2026, 5, 16, 7, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkFeedbackUsed([]int64{10}); err != nil {
		t.Fatal(err)
	}
	paperIDs, err := store.PaperIDsForFeedbackIDs([]int64{10})
	if err != nil {
		t.Fatal(err)
	}
	if len(paperIDs) != 1 || paperIDs[0] != 1 {
		t.Fatalf("unexpected paper ids: %#v", paperIDs)
	}
	applied, err := store.GetProfileProposal(11)
	if err != nil {
		t.Fatal(err)
	}
	if applied == nil || applied.State != "applied" || applied.AppliedVersion == nil || *applied.AppliedVersion != 3 {
		t.Fatalf("unexpected applied proposal: %#v", applied)
	}
	feedback, err := store.FeedbackByID(10)
	if err != nil {
		t.Fatal(err)
	}
	if feedback == nil || !feedback.UsedInProfile || feedback.State != "used" {
		t.Fatalf("unexpected feedback after apply: %#v", feedback)
	}

	if err := store.RejectProfileProposalState(11, time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	rejected, err := store.GetProfileProposal(11)
	if err != nil {
		t.Fatal(err)
	}
	if rejected == nil || rejected.State != "rejected" || rejected.RejectedAt == nil {
		t.Fatalf("unexpected rejected proposal: %#v", rejected)
	}
}
