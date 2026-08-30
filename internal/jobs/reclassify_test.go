package jobs

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	_ "modernc.org/sqlite"
)

func TestSelectPaperIDsForScopeAndRebuildReport(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	seedJobFixture(t, settings)

	ids, err := SelectPaperIDsForScope(settings, "all", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("unexpected paper ids: %#v", ids)
	}
	reportCount, err := RebuildLatestReport(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reportCount != 1 {
		t.Fatalf("unexpected report count: %d", reportCount)
	}
	if _, err := os.Stat(filepath.Join(settings.ReportsDir, "data", "latest.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no latest.json artifact, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(settings.ReportsDir, "latest", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("expected no static report artifact, got err=%v", err)
	}
}

func TestReclassifyPaperIDsDegradesFailedBatchToSingles(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	settings.ClassifierBatchSize = 2
	seedJobFixture(t, settings)
	db, err := sql.Open("sqlite", settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	execJobSQL(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
  abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  2, 'https://example.com/rss', 'Fixture Feed', 'Second API paper', 'https://example.com/api-paper-2',
  '10.1000/api2', 'Original Feed Title', '["Alice"]', 'Second plain abstract text.',
  'rss', '2026-05-15', '2026-05-16T00:00:00Z', NULL,
  '{"_abstract_html":"<p>Second plain abstract text.</p>","_abstract_images":[]}'
);
`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userContent := classifierUserPrompt(t, r)
		if strings.Contains(userContent, `"id":"2"`) {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":["}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"重分类论文\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ClassifierBaseURL = server.URL

	classified, err := ReclassifyPaperIDs(settings, []int64{1, 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if classified != 2 {
		t.Fatalf("expected 2 classifications, got %d", classified)
	}
	assertClassificationCount(t, settings, 2, 2)
}

func TestReclassifyUsesSavedThinkingPreferenceAndSafeTokenFloor(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	settings.ClassifierThinking = "enabled"
	settings.ClassifierBatchSize = 1
	seedJobFixture(t, settings)

	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"translated_title_zh\":\"重分类论文\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ClassifierModels = config.ClassifierModels{
		EnabledModelIDs: []string{config.ClassifierModelDeepSeekV4Flash},
		DefaultModelID:  config.ClassifierModelDeepSeekV4Flash,
		Models: map[string]config.ClassifierModelConfig{
			config.ClassifierModelDeepSeekV4Flash: {
				ClassifierModelSpec: config.ClassifierModelSpec{ID: config.ClassifierModelDeepSeekV4Flash, Provider: "deepseek", BaseURL: server.URL},
				APIKey:              "deepseek-key",
			},
		},
	}

	classified, err := ReclassifyPaperIDs(settings, []int64{1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if classified != 1 {
		t.Fatalf("classified = %d", classified)
	}
	thinking := request["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || request["reasoning_effort"] != "low" {
		t.Fatalf("runtime thinking controls = %#v", request)
	}
	if request["max_tokens"] != float64(4096) {
		t.Fatalf("max_tokens = %#v, want 4096", request["max_tokens"])
	}
}

func testJobSettings(root string) config.Settings {
	return config.Settings{
		Mode:                appruntime.ModeSource,
		RootDir:             root,
		AppDir:              root,
		UserDataDir:         root,
		ConfigDir:           root,
		DataDir:             filepath.Join(root, "data"),
		DatabasePath:        filepath.Join(root, "data", "literature.sqlite"),
		LogsDir:             filepath.Join(root, "logs"),
		ReportsDir:          filepath.Join(root, "reports"),
		RuntimeStatePath:    filepath.Join(root, "runtime.json"),
		WebDistDir:          filepath.Join(root, "web", "dist"),
		FeedsPath:           filepath.Join(root, "data", "rss_feeds.json"),
		ProfilePath:         filepath.Join(root, "data", "classification_profile.json"),
		ClassifierAPIKey:    "classifier-key",
		ClassifierBaseURL:   "https://example.com",
		ClassifierModel:     "classifier-model",
		ClassifierThinking:  "disabled",
		ClassifierBatchSize: 10,
	}
}

func seedJobFixture(t *testing.T, settings config.Settings) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(settings.DatabasePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	execJobSQL(t, db, `
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
	execJobSQL(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
  abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES (
  1, 'https://example.com/rss', 'Fixture Feed', 'API paper', 'https://example.com/api-paper',
  '10.1000/api', 'Original Feed Title', '["Alice","Bob"]', 'Plain abstract text.',
  'rss', '2026-05-15', '2026-05-16T00:00:00Z', NULL,
  '{"_abstract_html":"<p>Plain abstract text.</p>","_abstract_images":[]}'
);
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '["rna_bio"]', 'scan', 'test', NULL, '2026-05-16T00:10:00Z'
);
INSERT INTO feedback (
  paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  1, 'indirect', 'direct', 'Should be visible as direct.', 'open', 0, '2026-05-16T00:20:00Z'
);
`)
}

func execJobSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}
