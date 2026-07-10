package sqlite

import (
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestListFeedbackAndProfileProposalsSupportLegacyColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	execSQLite(t, db, `
CREATE TABLE papers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_url TEXT NOT NULL,
  feed_title TEXT,
  title TEXT NOT NULL,
  url TEXT NOT NULL,
  doi TEXT,
  journal TEXT,
  authors_json TEXT NOT NULL,
  abstract TEXT,
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  raw_json TEXT NOT NULL
);
CREATE TABLE feedback (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  original_relevance TEXT NOT NULL,
  corrected_relevance TEXT NOT NULL,
  note TEXT,
  state TEXT NOT NULL DEFAULT 'open',
  used_in_prompt INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE profile_proposals (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  summary TEXT NOT NULL,
  proposed_profile_json TEXT NOT NULL,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', NULL, 'Legacy paper', 'https://example.com/legacy', NULL, NULL, '[]', NULL, NULL, '2026-05-16T01:00:00Z', NULL, '{}'
);
INSERT INTO feedback (
  paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  1, 'indirect', 'direct', 'Legacy feedback', 'used', 1, '2026-05-16T04:00:00Z'
);
INSERT INTO profile_proposals (
  id, summary, proposed_profile_json, source_feedback_ids_json, model, state, created_at, applied_version
) VALUES (
  10, 'Legacy proposal',
  '{"meta":{"name":"Legacy","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"Fixture"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}',
  '[1,2]', 'deepseek-v4-pro', 'pending', '2026-05-16T05:00:00Z', NULL
);
`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	feedback, err := store.ListFeedback()
	if err != nil {
		t.Fatal(err)
	}
	if len(feedback) != 1 || !feedback[0].UsedInProfile || feedback[0].PaperTitle != "Legacy paper" {
		t.Fatalf("unexpected feedback payload: %#v", feedback)
	}
	proposals, err := store.ListProfileProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 || proposals[0].RuleDelta["summary"] != "Legacy proposal" || len(proposals[0].SourceFeedbackIDs) != 2 {
		t.Fatalf("unexpected proposal payload: %#v", proposals)
	}
}

func TestCreateDeleteFeedbackAndMarkRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	seedMutableFixture(t, db)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	record, err := store.CreateFeedback(1, "direct", stringPointer("Make it direct."), time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if record.OriginalRelevance != "indirect" || record.CorrectedRelevance != "direct" {
		t.Fatalf("unexpected feedback record: %#v", record)
	}

	firstReadAt, err := store.MarkPaperRead(1, time.Date(2026, 5, 16, 5, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	secondReadAt, err := store.MarkPaperRead(1, time.Date(2026, 5, 16, 6, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !firstReadAt.Equal(secondReadAt) {
		t.Fatalf("read_at should be idempotent: %s vs %s", firstReadAt, secondReadAt)
	}
	clearedReadAt, err := store.SetPaperRead(1, false, time.Date(2026, 5, 16, 7, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if clearedReadAt != nil {
		t.Fatalf("read_at should be cleared: %s", clearedReadAt)
	}

	if err := store.DeleteFeedback(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFeedback(999, "direct", nil, time.Now().UTC()); err == nil {
		t.Fatal("expected missing paper error")
	}
}
