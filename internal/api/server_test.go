package api

import (
	"database/sql"
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppHealthAndMeta(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"version":"1.2.3"}`)

	settings := testSettings(root)
	handler := newTestHandler(t, settings)

	healthRecorder := httptest.NewRecorder()
	handler.ServeHTTP(healthRecorder, httptest.NewRequest(http.MethodGet, "/api/app/health", nil))
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("health status = %d", healthRecorder.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(healthRecorder.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health["name"] != appruntime.AppPublicName || health["version"] != "1.2.3" || health["status"] != "ok" {
		t.Fatalf("unexpected health payload: %#v", health)
	}

	metaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metaRecorder, httptest.NewRequest(http.MethodGet, "/api/app/meta", nil))
	if metaRecorder.Code != http.StatusOK {
		t.Fatalf("meta status = %d", metaRecorder.Code)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaRecorder.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta["scheduler_task_name"] != appruntime.SchedulerTaskName {
		t.Fatalf("unexpected meta payload: %#v", meta)
	}
	if meta["data_dir"] != settings.DataDir || meta["static_dir"] != settings.WebDistDir {
		t.Fatalf("meta did not preserve configured paths: %#v", meta)
	}
	if meta["tray_instance_id"] != appruntime.TrayInstanceID(settings.ConfigDir) {
		t.Fatalf("unexpected tray instance in meta: %#v", meta)
	}
}

func TestStaticFallbackAndAPINotFound(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, filepath.Join(settings.WebDistDir, "index.html"), "<main>app shell</main>")
	writeFile(t, filepath.Join(settings.WebDistDir, "assets", "app.js"), "console.log('ok')")
	handler := newTestHandler(t, settings)

	assetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(assetRecorder, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if assetRecorder.Code != http.StatusOK || assetRecorder.Body.String() != "console.log('ok')" {
		t.Fatalf("unexpected asset response: %d %q", assetRecorder.Code, assetRecorder.Body.String())
	}

	spaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(spaRecorder, httptest.NewRequest(http.MethodGet, "/papers/123", nil))
	if spaRecorder.Code != http.StatusOK || spaRecorder.Body.String() != "<main>app shell</main>" {
		t.Fatalf("unexpected spa response: %d %q", spaRecorder.Code, spaRecorder.Body.String())
	}

	apiRecorder := httptest.NewRecorder()
	handler.ServeHTTP(apiRecorder, httptest.NewRequest(http.MethodGet, "/api/unknown", nil))
	if apiRecorder.Code != http.StatusNotFound {
		t.Fatalf("api status = %d", apiRecorder.Code)
	}
}

func TestReportLatestReturnsEmptyWhenDatabaseMissing(t *testing.T) {
	root := t.TempDir()
	handler := newTestHandler(t, testSettings(root))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("report status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"total":0`) || !contains(recorder.Body.String(), `"papers":[]`) {
		t.Fatalf("unexpected empty report payload: %s", recorder.Body.String())
	}
}

func TestServerOpensDatabaseAfterInitialMiss(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	handler := newTestHandler(t, settings)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if first.Code != http.StatusOK || !contains(first.Body.String(), `"total":0`) {
		t.Fatalf("first report response = %d %s", first.Code, first.Body.String())
	}

	seedReadOnlyFixture(t, settings.DatabasePath)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if second.Code != http.StatusOK || !contains(second.Body.String(), `"total":1`) || !contains(second.Body.String(), `"title":"API paper"`) {
		t.Fatalf("second report response = %d %s", second.Code, second.Body.String())
	}
}

func TestServerCloseIsIdempotent(t *testing.T) {
	server := NewServer(testSettings(t.TempDir()), nil)
	if err := server.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestServerUsesSeparateReadAndWriteStores(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	seedReadOnlyFixture(t, settings.DatabasePath)
	server := NewServer(settings, nil)
	defer func() {
		if err := server.Close(); err != nil {
			t.Fatalf("close api server: %v", err)
		}
	}()

	readStore, err := server.getReadStore()
	if err != nil {
		t.Fatal(err)
	}
	writeStore, err := server.getWriteStore()
	if err != nil {
		t.Fatal(err)
	}
	if readStore == writeStore {
		t.Fatal("expected separate read and write stores")
	}
}

func TestNewServerRepairsLegacyLLMUsageBeforeDashboardRead(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.DatabasePath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	execSQLite(t, db, `
CREATE TABLE llm_usage_jobs (
  job_id TEXT PRIMARY KEY, job_type TEXT NOT NULL, status TEXT NOT NULL, model TEXT NOT NULL,
  request_count INTEGER NOT NULL, prompt_tokens INTEGER NOT NULL,
  prompt_cache_hit_tokens INTEGER NOT NULL, prompt_cache_miss_tokens INTEGER NOT NULL,
  completion_tokens INTEGER NOT NULL, pricing_status TEXT NOT NULL, pricing_json TEXT NOT NULL,
  estimated_cost_nano_cny INTEGER, estimated_cost_cny TEXT, completed_at TEXT NOT NULL
);
INSERT INTO llm_usage_jobs VALUES (
  'legacy-sync', 'sync', 'completed', 'deepseek-v4-flash', 28, 95249,
  52608, 42641, 12547, 'estimated',
  '[{"model":"deepseek-v4-flash","snapshot":"deepseek-cny-2026-07-24","cache_hit_nano_cny_per_token":20,"cache_miss_nano_cny_per_token":1000,"completion_nano_cny_per_token":2000}]',
  68787160, '0.068787', '2026-08-22T04:36:23Z'
);`)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	server := NewServer(settings, nil)
	defer server.Close()
	readStore, err := server.getReadStore()
	if err != nil {
		t.Fatal(err)
	}
	items, err := readStore.ListLLMUsage(time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EstimatedCostCNY == nil || *items[0].EstimatedCostCNY != "0.123053" {
		t.Fatalf("legacy usage was not repaired on server startup: %#v", items)
	}
}

func newTestHandler(t *testing.T, settings config.Settings) http.Handler {
	t.Helper()
	server := NewServer(settings, nil)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close api server: %v", err)
		}
	})
	return server.Handler()
}

func seedReadOnlyFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
  id, paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES
  (1, 1, 'indirect', 'direct', 'Should be visible as direct.', 'open', 0, '2026-05-16T00:20:00Z'),
  (2, 1, 'indirect', 'unrelated', 'Legacy second feedback.', 'open', 0, '2026-05-16T00:21:00Z');
INSERT INTO profile_proposals (
  id, summary, proposed_profile_json, rule_delta_json, source_feedback_ids_json, model, state, created_at
) VALUES (
  1, 'Proposal summary',
  '{"meta":{"name":"Proposal","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"proposal"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}',
  '{"summary":"Proposal summary","direct_rule_additions":["RNA chemistry"],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}',
  '[1,2]', 'deepseek-v4-pro', 'pending', '2026-05-16T00:30:00Z'
);
`)
}

func seedCompactProposalFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
  base_profile_version INTEGER,
  change_set_json TEXT,
  source_feedback_ids_json TEXT NOT NULL,
  model TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  created_at TEXT NOT NULL,
  applied_at TEXT,
  rejected_at TEXT,
  applied_version INTEGER,
  applied_profile_json TEXT
);
`)
	execSQLite(t, db, `
INSERT INTO papers (
  id, source_url, feed_title, title, url, doi, journal, authors_json, abstract,
  abstract_source, published_date, first_seen_at, read_at, raw_json
) VALUES
  (
    1, 'https://example.com/rss', 'Fixture Feed', 'Compact proposal paper', 'https://example.com/compact-paper',
    '10.1000/compact', 'Example Journal', '["Alice","Bob"]', 'Abstract text.',
    'rss', '2026-05-15', '2026-05-16T00:00:00Z', NULL,
    '{"_abstract_html":"<p>Abstract text.</p>","_abstract_images":[]}'
  ),
  (
    2, 'https://example.com/rss', 'Fixture Feed', 'Rejected feedback paper', 'https://example.com/rejected-paper',
    '10.1000/rejected', 'Example Journal', '["Carol"]', 'Rejected abstract text.',
    'rss', '2026-05-15', '2026-05-16T00:00:00Z', NULL,
    '{"_abstract_html":"<p>Rejected abstract text.</p>","_abstract_images":[]}'
  );
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '[]', 'scan', 'test', NULL, '2026-05-16T00:10:00Z'
);
INSERT INTO feedback (
  id, paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES
  (1, 1, 'indirect', 'direct', 'Should be direct.', 'open', 0, '2026-05-16T00:20:00Z'),
  (2, 2, 'indirect', 'unrelated', 'Should stay available after rejected change.', 'open', 0, '2026-05-16T00:21:00Z');
INSERT INTO profile_proposals (
  id, summary, proposed_profile_json, rule_delta_json, base_profile_version, change_set_json, source_feedback_ids_json, model, state, created_at
) VALUES (
  21, 'Compact proposal summary',
  '{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-16T00:30:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA chemistry"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}',
  '{"summary":"Compact proposal summary","direct_rule_additions":[],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}',
  1,
  '[{"id":"change-1","section":"direct_rule","operation":"rewrite","summary":"Promote chemistry-first RNA work.","text_before":["RNA"],"text_after":["RNA chemistry"],"topic_before":[],"topic_after":[],"rationale":"The feedback shows chemistry-first RNA papers should be direct.","source_feedback_ids":[1],"source_paper_ids":[1],"status":"proposed"},{"id":"change-2","section":"unrelated_rule","operation":"remove","summary":"Drop the unrelated rule.","text_before":["Plant biology"],"text_after":[],"topic_before":[],"topic_after":[],"rationale":"A review candidate that can be declined.","source_feedback_ids":[2],"source_paper_ids":[2],"status":"proposed"}]',
  '[1,2]', 'deepseek-v4-pro', 'pending', '2026-05-16T00:30:00Z'
);
`)
}

func execSQLite(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func feedbackStateCount(t *testing.T, path string, state string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feedback WHERE state = ?`, state).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stubAPIGlobals(t *testing.T) func() {
	t.Helper()
	previousOpen := openExternalTargetFunc
	previousLookupTXT := lookupUpdateTXTFunc
	previousBackendRunCommand := backendRunCommandFunc
	previousBootstrapProfile := bootstrapProfileFunc
	previousGenerateProfileProposal := generateProfileProposalFunc
	previousListZoteroCollections := listZoteroCollectionsFunc
	previousSavePaperToZotero := savePaperToZoteroFunc
	previousSelectReclassify := selectReclassifyPaperIDsFunc
	previousReclassify := reclassifyPaperIDsFunc
	previousRebuildLatestReport := rebuildLatestReportFunc
	previousRunSync := runSyncFunc
	previousNotifyTraySettingsChanged := notifyTraySettingsChangedFunc
	previousStartVerification := startVerificationFlowFunc
	previousCompleteVerification := completeVerificationFlowFunc
	previousNow := nowFunc
	previousJobs := apiJobs
	previousVerifications := apiVerifications
	apiJobs = jobRegistry{jobs: map[string]jobInfo{}}
	apiVerifications = verificationRegistry{items: map[string]*pendingVerification{}}
	return func() {
		openExternalTargetFunc = previousOpen
		lookupUpdateTXTFunc = previousLookupTXT
		backendRunCommandFunc = previousBackendRunCommand
		bootstrapProfileFunc = previousBootstrapProfile
		generateProfileProposalFunc = previousGenerateProfileProposal
		listZoteroCollectionsFunc = previousListZoteroCollections
		savePaperToZoteroFunc = previousSavePaperToZotero
		selectReclassifyPaperIDsFunc = previousSelectReclassify
		reclassifyPaperIDsFunc = previousReclassify
		rebuildLatestReportFunc = previousRebuildLatestReport
		runSyncFunc = previousRunSync
		notifyTraySettingsChangedFunc = previousNotifyTraySettingsChanged
		startVerificationFlowFunc = previousStartVerification
		completeVerificationFlowFunc = previousCompleteVerification
		nowFunc = previousNow
		apiJobs = previousJobs
		apiVerifications = previousVerifications
	}
}

func waitForJobCompletion(t *testing.T, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := jobByID(jobID)
		if ok && (job.Status == "completed" || job.Status == "failed") {
			if job.Status != "completed" {
				t.Fatalf("job %s finished with status %s: %s", jobID, job.Status, job.Error)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not complete in time", jobID)
}

func waitForJobTerminalStatus(t *testing.T, jobID string) jobInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := jobByID(jobID)
		if ok && (job.Status == "completed" || job.Status == "failed") {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach a terminal status in time", jobID)
	return jobInfo{}
}

func waitForJobStatus(t *testing.T, jobID string, status string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := jobByID(jobID)
		if ok && job.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s in time", jobID, status)
}
