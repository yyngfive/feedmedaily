package sqlite

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/profile"
	_ "modernc.org/sqlite"
)

func TestBuildLatestReportReturnsEmptyForUninitializedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	execSQLite(t, db, `PRAGMA user_version = 0;`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	report, err := store.BuildLatestReport(time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals["total"] != 0 || len(report.Papers) != 0 || len(report.Errors) != 0 {
		t.Fatalf("unexpected empty report: %#v", report)
	}
}

func TestBuildLatestReportIncludesFeedbackAndZoteroStatus(t *testing.T) {
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
  abstract_source TEXT NOT NULL DEFAULT 'none',
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  raw_json TEXT NOT NULL
);
CREATE TABLE classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL
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
  rule_delta_json TEXT,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER
);
CREATE TABLE zotero_saves (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL UNIQUE,
  state TEXT NOT NULL,
  item_key TEXT,
  error_message TEXT,
  attempted_at TEXT NOT NULL,
  saved_at TEXT
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
  abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', 'Example Feed', 'Fixture paper', 'https://example.com/paper',
  '10.1000/test', 'Example Journal', '["Alice","Bob"]', 'Abstract text.', 'rss', '2026-05-15',
  '2026-05-16T01:02:03Z', NULL,
  '{"_abstract_html":"<p>Abstract text.</p>","_abstract_images":[{"src":"https://example.com/figure.png","alt":"Figure"}],"source":"fixture"}'
);
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '["rna_bio"]', 'scan', 'test-model', '测试标题', '2026-05-16T02:00:00Z'
);
INSERT INTO feedback (
  paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  1, 'indirect', 'direct', 'Should be direct.', 'open', 0, '2026-05-16T03:00:00Z'
);
INSERT INTO zotero_saves (
  paper_id, state, item_key, error_message, attempted_at, saved_at
) VALUES (
  1, 'saved', 'ITEM123', NULL, '2026-05-16T03:05:00Z', '2026-05-16T03:05:01Z'
);
`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	report, err := store.BuildLatestReport(time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals["total"] != 1 || report.Totals["indirect"] != 1 {
		t.Fatalf("unexpected totals: %#v", report.Totals)
	}
	if report.LastUpdatedAt == nil || !report.LastUpdatedAt.Equal(time.Date(2026, 5, 16, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("unexpected last updated at: %#v", report.LastUpdatedAt)
	}
	paper := report.Papers[0]
	if paper.Title != "Fixture paper" || paper.Classification.TranslatedTitleZH == nil || *paper.Classification.TranslatedTitleZH != "测试标题" {
		t.Fatalf("unexpected paper payload: %#v", paper)
	}
	if paper.FeedbackStatus == nil || paper.FeedbackStatus.CorrectedRelevance == nil || *paper.FeedbackStatus.CorrectedRelevance != "direct" {
		t.Fatalf("unexpected feedback status: %#v", paper.FeedbackStatus)
	}
	if paper.ZoteroStatus == nil || !paper.ZoteroStatus.Saved {
		t.Fatalf("unexpected zotero status: %#v", paper.ZoteroStatus)
	}
	if paper.AbstractHTML == nil || *paper.AbstractHTML != "<p>Abstract text.</p>" || len(paper.AbstractImages) != 1 {
		t.Fatalf("unexpected abstract payload: %#v", paper)
	}
	if len(paper.Raw) != 0 {
		t.Fatalf("unexpected raw payload: %#v", paper.Raw)
	}
}

func TestBuildLatestReportLastUpdatedAtDoesNotDriftWithReadTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seenAt := time.Date(2026, 5, 16, 1, 2, 3, 0, time.UTC)
	paper := Paper{
		SourceURL:      "https://example.com/rss",
		FeedTitle:      stringPtr("Feed"),
		Title:          "Stable timestamp paper",
		URL:            "https://example.com/paper",
		Authors:        []string{"Alice"},
		AbstractSource: "none",
	}
	paperID, _, err := store.UpsertPaper(paper, seenAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClassification(paperID, Classification{
		Relevance:         "direct",
		Confidence:        0.9,
		TopicTags:         []string{"rna_bio"},
		Reason:            "Fixture",
		RecommendedAction: "read",
		Model:             "test-model",
	}, seenAt); err != nil {
		t.Fatal(err)
	}

	reportA, err := store.BuildLatestReport(time.Date(2026, 5, 20, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	reportB, err := store.BuildLatestReport(time.Date(2026, 5, 22, 9, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if reportA.LastUpdatedAt == nil || reportB.LastUpdatedAt == nil {
		t.Fatalf("missing last updated timestamps: %#v %#v", reportA.LastUpdatedAt, reportB.LastUpdatedAt)
	}
	if !reportA.LastUpdatedAt.Equal(seenAt) || !reportB.LastUpdatedAt.Equal(seenAt) {
		t.Fatalf("last updated timestamp drifted: %#v %#v", reportA.LastUpdatedAt, reportB.LastUpdatedAt)
	}
}

func TestBuildLatestReportUsesLatestClassificationAndOpenFeedback(t *testing.T) {
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
  abstract_source TEXT NOT NULL DEFAULT 'none',
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  raw_json TEXT NOT NULL
);
CREATE TABLE classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL
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
CREATE TABLE zotero_saves (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL UNIQUE,
  state TEXT NOT NULL,
  item_key TEXT,
  error_message TEXT,
  attempted_at TEXT NOT NULL,
  saved_at TEXT
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
  abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', 'Example Feed', 'Latest selection paper', 'https://example.com/latest',
  NULL, 'Example Journal', '["Alice"]', 'Abstract text.', 'rss', '2026-05-15',
  '2026-05-16T01:02:03Z', NULL,
  '{"_abstract_html":"<p>Abstract text.</p>","_abstract_images":[]}'
);
INSERT INTO classifications (
  id, paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES
  (1, 1, 'indirect', 0.5, 'Older classification', '["old"]', 'scan', 'test-model', NULL, '2026-05-16T02:00:00Z'),
  (2, 1, 'direct', 0.9, 'Latest classification', '["new"]', 'read', 'test-model', '新标题', '2026-05-16T02:30:00Z');
INSERT INTO feedback (
  id, paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES
  (1, 1, 'indirect', 'indirect', 'Older open feedback', 'open', 0, '2026-05-16T03:00:00Z'),
  (2, 1, 'direct', 'unrelated', 'Used feedback should be ignored', 'used', 1, '2026-05-16T03:30:00Z'),
  (3, 1, 'direct', 'direct', 'Latest open feedback', 'open', 0, '2026-05-16T04:00:00Z');
INSERT INTO zotero_saves (
  paper_id, state, item_key, error_message, attempted_at, saved_at
) VALUES (
  1, 'saved', 'ITEM123', NULL, '2026-05-16T05:00:00Z', '2026-05-16T05:00:01Z'
);
`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	report, err := store.BuildLatestReport(time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals["total"] != 1 || report.Totals["direct"] != 1 {
		t.Fatalf("unexpected totals: %#v", report.Totals)
	}
	paper := report.Papers[0]
	if paper.Classification.Relevance != "direct" || paper.Classification.Reason != "Latest classification" {
		t.Fatalf("unexpected latest classification: %#v", paper.Classification)
	}
	if paper.Classification.TranslatedTitleZH == nil || *paper.Classification.TranslatedTitleZH != "新标题" {
		t.Fatalf("unexpected translated title: %#v", paper.Classification.TranslatedTitleZH)
	}
	if paper.FeedbackStatus == nil || paper.FeedbackStatus.CorrectedRelevance == nil || *paper.FeedbackStatus.CorrectedRelevance != "direct" {
		t.Fatalf("unexpected latest open feedback: %#v", paper.FeedbackStatus)
	}
	if paper.FeedbackStatus.Note == nil || *paper.FeedbackStatus.Note != "Latest open feedback" {
		t.Fatalf("unexpected feedback note: %#v", paper.FeedbackStatus)
	}
}

func TestBuildLatestReportSupportsLegacySchemaWithoutFeedbackOrZoteroTables(t *testing.T) {
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
CREATE TABLE classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  model TEXT NOT NULL,
  classified_at TEXT NOT NULL
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', NULL, 'Legacy report paper', 'https://example.com/legacy-report', NULL, NULL, '["Alice"]', 'Legacy abstract', NULL, '2026-05-16T01:00:00Z', NULL, '{}'
);
INSERT INTO classifications (
  id, paper_id, relevance, confidence, reason, topic_tags_json, model, classified_at
) VALUES (
  1, 1, 'indirect', 0.4, 'Legacy classification', '[]', 'legacy-model', '2026-05-16T02:00:00Z'
);
`)
	db.Close()

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	report, err := store.BuildLatestReport(time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Papers) != 1 || report.Totals["indirect"] != 1 {
		t.Fatalf("unexpected report payload: %#v", report)
	}
	paper := report.Papers[0]
	if paper.AbstractSource != "none" {
		t.Fatalf("unexpected abstract source fallback: %#v", paper.AbstractSource)
	}
	if paper.FeedbackStatus != nil {
		t.Fatalf("expected nil feedback status for missing feedback table: %#v", paper.FeedbackStatus)
	}
	if paper.ZoteroStatus != nil {
		t.Fatalf("expected nil zotero status for missing zotero table: %#v", paper.ZoteroStatus)
	}
	if paper.Classification.RecommendedAction != "scan" {
		t.Fatalf("unexpected recommended action fallback: %#v", paper.Classification)
	}
}

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

	if err := store.DeleteFeedback(record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateFeedback(999, "direct", nil, time.Now().UTC()); err == nil {
		t.Fatal("expected missing paper error")
	}
}

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

func TestUpsertZoteroStatusTracksSavedAndErrorStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	paperID, _, err := store.UpsertPaper(Paper{
		SourceURL: "https://example.com/rss",
		Title:     "Zotero write test",
		URL:       "https://example.com/zotero-write-test",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	itemKey := "ITEM-1"
	saved, err := store.UpsertZoteroStatus(paperID, "saved", &itemKey, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || !saved.Saved || saved.ItemKey == nil || *saved.ItemKey != "ITEM-1" || saved.SavedAt == nil {
		t.Fatalf("unexpected saved status: %#v", saved)
	}
	lastError := "rate limited"
	failed, err := store.UpsertZoteroStatus(paperID, "error", nil, &lastError, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if failed == nil || failed.Saved || failed.LastError == nil || *failed.LastError != "rate limited" || failed.SavedAt != nil {
		t.Fatalf("unexpected error status: %#v", failed)
	}
	reloaded, err := store.LatestZoteroStatus(paperID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == nil || reloaded.State == nil || *reloaded.State != "error" {
		t.Fatalf("unexpected reloaded status: %#v", reloaded)
	}
}

func openSQLiteTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func execSQLite(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func seedMutableFixture(t *testing.T, db *sql.DB) {
	t.Helper()
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
  abstract_source TEXT NOT NULL DEFAULT 'none',
  published_date TEXT,
  first_seen_at TEXT NOT NULL,
  read_at TEXT,
  raw_json TEXT NOT NULL
);
CREATE TABLE classifications (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  paper_id INTEGER NOT NULL,
  relevance TEXT NOT NULL,
  confidence REAL NOT NULL,
  reason TEXT NOT NULL,
  topic_tags_json TEXT NOT NULL,
  recommended_action TEXT NOT NULL,
  model TEXT NOT NULL,
  translated_title_zh TEXT,
  classified_at TEXT NOT NULL
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
  rule_delta_json TEXT,
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
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract, abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', 'Feed', 'Mutable paper', 'https://example.com/mutable', NULL, NULL, '[]', NULL, 'none', NULL, '2026-05-16T01:00:00Z', NULL, '{}'
);
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '[]', 'scan', 'test-model', NULL, '2026-05-16T02:00:00Z'
);
`)
}

func stringPointer(value string) *string {
	return &value
}
