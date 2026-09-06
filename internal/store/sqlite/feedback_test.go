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

	// 写路径总是先经 OpenOrCreate 完成列迁移（与服务器启动行为一致），再打开写 store。
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	record, err := store.CreateFeedback(1, "direct", nil, stringPointer("Make it direct."), time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if record.OriginalRelevance != "indirect" || record.CorrectedRelevance != "direct" {
		t.Fatalf("unexpected feedback record: %#v", record)
	}
	if record.OriginalTopic != nil || record.CorrectedTopic != nil {
		t.Fatalf("unexpected topic fields: %#v", record)
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
	if _, err := store.CreateFeedback(999, "direct", nil, nil, time.Now().UTC()); err == nil {
		t.Fatal("expected missing paper error")
	}
}

func TestCreateFeedbackStoresTopicCorrection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	seedMutableFixture(t, db)
	db.Close()

	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveClassification(1, Classification{
		Relevance: "indirect", Confidence: 0.8, Reason: "fixture",
		TopicTags: []string{TopicNoneID}, RecommendedAction: "scan", Model: "fixture",
	}, time.Date(2026, 5, 16, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateFeedback(1, "indirect", stringPointer("t-topic001"), nil, time.Date(2026, 5, 16, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if record.OriginalTopic == nil || *record.OriginalTopic != TopicNoneID {
		t.Fatalf("expected original topic sentinel, got %#v", record.OriginalTopic)
	}
	if record.CorrectedTopic == nil || *record.CorrectedTopic != "t-topic001" {
		t.Fatalf("unexpected corrected topic: %#v", record.CorrectedTopic)
	}

	contexts, err := store.ListOpenFeedbackContexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || contexts[0].OriginalTopic != TopicNoneID || contexts[0].CorrectedTopic == nil || *contexts[0].CorrectedTopic != "t-topic001" {
		t.Fatalf("unexpected open feedback context: %#v", contexts)
	}

	// corrected_topic 传 nil 表示“无主题”纠正。
	record, err = store.CreateFeedback(1, "indirect", nil, stringPointer("no topic"), time.Date(2026, 5, 16, 5, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if record.CorrectedTopic != nil {
		t.Fatalf("expected nil corrected topic, got %#v", record.CorrectedTopic)
	}
}

func TestRelatedPaperIDsWithoutTopicFiltersStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	seedMutableFixture(t, db)
	db.Close()

	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	seed := []struct {
		paperID int64
		rel     string
		tags    []string
		at      time.Time
	}{
		// 使用 fixture 未触碰的 paper id（fixture 给 paper 1 预置了 02:00 的分类）。
		{101, "direct", []string{"t-real0001"}, time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)},
		{102, "direct", []string{TopicNoneID}, time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)},
		{103, "indirect", []string{}, time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)},
		{104, "unrelated", []string{}, time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)},
		{105, "indirect", []string{"t-orphan09"}, time.Date(2026, 5, 16, 1, 0, 0, 0, time.UTC)},
	}
	for _, item := range seed {
		if err := store.SaveClassification(item.paperID, Classification{
			Relevance: item.rel, Confidence: 0.5, Reason: "fixture",
			TopicTags: item.tags, RecommendedAction: "scan", Model: "fixture",
		}, item.at); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := store.RelatedPaperIDsWithoutTopic(map[string]struct{}{"t-real0001": {}})
	if err != nil {
		t.Fatal(err)
	}
	// paper 1 来自 fixture：indirect 且空 tags，属于未判定；104 是 unrelated 应被排除。
	expected := []int64{105, 103, 102, 1}
	if len(ids) != len(expected) {
		t.Fatalf("expected ids %v, got %v", expected, ids)
	}
	for index, id := range expected {
		if ids[index] != id {
			t.Fatalf("expected ids %v, got %v", expected, ids)
		}
	}
}

func TestFeedbackPaperIDsSelectsOnlyOpenFeedback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	db := openSQLiteTestDB(t, path)
	seedMutableFixture(t, db)
	db.Close()

	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// fixture 里 paper 1 已有一条 used feedback；再补一条 open。
	if _, err := store.CreateFeedback(1, "direct", nil, nil, time.Date(2026, 5, 16, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	ids, err := store.FeedbackPaperIDs()
	if err != nil {
		t.Fatal(err)
	}
	// used 的不再入选：即便库里有历史 feedback，也只有 open 的论文参与重分类。
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("expected only open-feedback paper ids, got %v", ids)
	}
}
