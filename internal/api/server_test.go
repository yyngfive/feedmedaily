package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
	_ "modernc.org/sqlite"
)

func TestAppHealthAndMeta(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\nversion = \"1.2.3\"\n")

	settings := testSettings(root)
	handler := NewServer(settings, nil).Handler()

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
}

func TestStaticFallbackAndAPINotFound(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, filepath.Join(settings.WebDistDir, "index.html"), "<main>app shell</main>")
	writeFile(t, filepath.Join(settings.WebDistDir, "assets", "app.js"), "console.log('ok')")
	handler := NewServer(settings, nil).Handler()

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

func TestSettingsConfigAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\nversion = \"1.2.3\"\n")
	writeFile(t, filepath.Join(root, "src", "scirssagent", "__init__.py"), "")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_API_KEY=super-secret\n")
	handler := NewServer(testSettings(root), nil).Handler()

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings/config", nil))
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d: %s", getRecorder.Code, getRecorder.Body.String())
	}
	if getRecorder.Body.String() == "" || contains(getRecorder.Body.String(), "super-secret") {
		t.Fatalf("settings response leaked secret: %s", getRecorder.Body.String())
	}

	putBody := strings.NewReader(`{"fields":{"SCIRSS_CLASSIFIER_MODEL":{"value":"deepseek-v4-pro"}}}`)
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", putBody))
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d: %s", putRecorder.Code, putRecorder.Body.String())
	}
	envText, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(envText), "SCIRSS_CLASSIFIER_MODEL='deepseek-v4-pro'") {
		t.Fatalf("env text = %s", envText)
	}
}

func TestSettingsFeedsAPI(t *testing.T) {
	root := t.TempDir()
	handler := NewServer(testSettings(root), nil).Handler()

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings/feeds", nil))
	if getRecorder.Code != http.StatusOK || strings.TrimSpace(getRecorder.Body.String()) != "[]" {
		t.Fatalf("get response = %d %q", getRecorder.Code, getRecorder.Body.String())
	}

	putBody := strings.NewReader(`{"feeds":[{"journal":"Nature","url":"https://www.nature.com/nature.rss"}]}`)
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/feeds", putBody))
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("put status = %d: %s", putRecorder.Code, putRecorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "data", "rss_feeds.json")); err != nil {
		t.Fatal(err)
	}
}

func TestReadOnlyBootstrapEndpoints(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	seedReadOnlyFixture(t, settings.DatabasePath)
	handler := NewServer(settings, nil).Handler()

	profileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(profileRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/current", nil))
	if profileRecorder.Code != http.StatusOK || !contains(profileRecorder.Body.String(), `"name":"Test"`) {
		t.Fatalf("profile response = %d %s", profileRecorder.Code, profileRecorder.Body.String())
	}

	reportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reportRecorder, httptest.NewRequest(http.MethodGet, "/api/report/latest", nil))
	if reportRecorder.Code != http.StatusOK || !contains(reportRecorder.Body.String(), `"total":1`) || !contains(reportRecorder.Body.String(), `"title":"API paper"`) {
		t.Fatalf("report response = %d %s", reportRecorder.Code, reportRecorder.Body.String())
	}

	feedbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(feedbackRecorder, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if feedbackRecorder.Code != http.StatusOK || !contains(feedbackRecorder.Body.String(), `"corrected_relevance":"direct"`) {
		t.Fatalf("feedback response = %d %s", feedbackRecorder.Code, feedbackRecorder.Body.String())
	}

	proposalsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(proposalsRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/proposals", nil))
	if proposalsRecorder.Code != http.StatusOK || !contains(proposalsRecorder.Body.String(), `"summary":"Proposal summary"`) {
		t.Fatalf("proposal list response = %d %s", proposalsRecorder.Code, proposalsRecorder.Body.String())
	}

	proposalRecorder := httptest.NewRecorder()
	handler.ServeHTTP(proposalRecorder, httptest.NewRequest(http.MethodGet, "/api/profile/proposals/1", nil))
	if proposalRecorder.Code != http.StatusOK || !contains(proposalRecorder.Body.String(), `"source_feedback_ids":[1,2]`) {
		t.Fatalf("proposal detail response = %d %s", proposalRecorder.Code, proposalRecorder.Body.String())
	}

	jobsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jobsRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs", nil))
	if jobsRecorder.Code != http.StatusOK || strings.TrimSpace(jobsRecorder.Body.String()) != "[]" {
		t.Fatalf("jobs response = %d %s", jobsRecorder.Code, jobsRecorder.Body.String())
	}
}

func TestAppUpdateOpenAndSchedulerAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	nowFunc = func() time.Time {
		return time.Date(2026, 5, 16, 7, 30, 0, 0, time.FixedZone("CST", 8*3600))
	}
	fetchUpdateManifestFunc = func(string) (map[string]any, error) {
		return map[string]any{
			"version":           "9.9.9",
			"download_url":      "https://example.com/feedmedaily.exe",
			"release_notes_url": "https://example.com/release-notes",
		}, nil
	}
	backendRunCommandFunc = func(config.Settings) ([]string, error) {
		return []string{"go", "run", "./cmd/feedmedailyd", "--run-once"}, nil
	}
	opened := ""
	openExternalTargetFunc = func(target string) error {
		opened = target
		return nil
	}
	settings.UpdateManifestURL = "https://example.com/update.json"
	handler := NewServer(settings, nil).Handler()

	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if updateRecorder.Code != http.StatusOK || !contains(updateRecorder.Body.String(), `"has_update":true`) {
		t.Fatalf("update response = %d %s", updateRecorder.Code, updateRecorder.Body.String())
	}

	openRecorder := httptest.NewRecorder()
	handler.ServeHTTP(openRecorder, httptest.NewRequest(http.MethodPost, "/api/app/open", strings.NewReader(`{"target":"download_url"}`)))
	if openRecorder.Code != http.StatusOK || opened != "https://example.com/feedmedaily.exe" {
		t.Fatalf("open response = %d %s opened=%q", openRecorder.Code, openRecorder.Body.String(), opened)
	}

	schedulerGet := httptest.NewRecorder()
	handler.ServeHTTP(schedulerGet, httptest.NewRequest(http.MethodGet, "/api/settings/scheduler", nil))
	if schedulerGet.Code != http.StatusOK || !contains(schedulerGet.Body.String(), `"installed":false`) {
		t.Fatalf("scheduler get = %d %s", schedulerGet.Code, schedulerGet.Body.String())
	}

	schedulerPut := httptest.NewRecorder()
	handler.ServeHTTP(schedulerPut, httptest.NewRequest(http.MethodPut, "/api/settings/scheduler", strings.NewReader(`{"daily_time":"08:15"}`)))
	if schedulerPut.Code != http.StatusOK || !contains(schedulerPut.Body.String(), `"installed":true`) || !contains(schedulerPut.Body.String(), `"scheduled_time":"08:15"`) {
		t.Fatalf("scheduler put = %d %s", schedulerPut.Code, schedulerPut.Body.String())
	}

	schedulerDelete := httptest.NewRecorder()
	handler.ServeHTTP(schedulerDelete, httptest.NewRequest(http.MethodDelete, "/api/settings/scheduler", nil))
	if schedulerDelete.Code != http.StatusOK || !contains(schedulerDelete.Body.String(), `"installed":false`) {
		t.Fatalf("scheduler delete = %d %s", schedulerDelete.Code, schedulerDelete.Body.String())
	}
}

func TestAdminRunAndReportJobs(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runOnceFunc = func(_ config.Settings, _ jobruntime.RunOptions, progress jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if progress != nil {
			progress("pipeline.feeds.fetching", "Fetching RSS feeds.")
			progress("pipeline.metadata.enriching", "Getting metadata for 1 paper(s).")
			progress("pipeline.classifier.classifying", "Classifying 1 paper(s).")
			progress("pipeline.report.writing", "Publishing the latest report.")
		}
		return jobruntime.RunSummary{
			Fetched:    2,
			Inserted:   1,
			Updated:    1,
			Classified: 1,
			Errors:     []string{"https://bad.feed/: timeout"},
			ReportPath: filepath.Join(root, "reports", "latest", "index.html"),
		}, nil
	}
	rebuildLatestReportFunc = func(_ config.Settings, progress jobruntime.ProgressFunc) (int, error) {
		if progress != nil {
			progress("pipeline.report.writing", "Publishing the latest report.")
		}
		return 3, nil
	}

	handler := NewServer(testSettings(root), nil).Handler()

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, runPayload.Job.ID)

	reportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reportRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/report/latest", nil))
	if reportRecorder.Code != http.StatusOK {
		t.Fatalf("report launch = %d %s", reportRecorder.Code, reportRecorder.Body.String())
	}
	var reportPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(reportRecorder.Body.Bytes(), &reportPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, reportPayload.Job.ID)

	jobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jobRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs/"+runPayload.Job.ID, nil))
	if jobRecorder.Code != http.StatusOK || !contains(jobRecorder.Body.String(), `"status":"completed"`) || !contains(jobRecorder.Body.String(), `"fetched":2`) {
		t.Fatalf("job detail = %d %s", jobRecorder.Code, jobRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs", nil))
	if listRecorder.Code != http.StatusOK || !contains(listRecorder.Body.String(), `"job_type":"run"`) || !contains(listRecorder.Body.String(), `"job_type":"report"`) {
		t.Fatalf("job list = %d %s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestFeedbackReadAndDeleteMutationAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	seedReadOnlyFixture(t, settings.DatabasePath)
	handler := NewServer(settings, nil).Handler()

	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/api/feedback", strings.NewReader(`{"paper_id":1,"corrected_relevance":"direct","note":"Promote."}`)))
	if createRecorder.Code != http.StatusOK || !contains(createRecorder.Body.String(), `"original_relevance":"indirect"`) {
		t.Fatalf("create feedback = %d %s", createRecorder.Code, createRecorder.Body.String())
	}

	readRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readRecorder, httptest.NewRequest(http.MethodPost, "/api/papers/1/read", nil))
	if readRecorder.Code != http.StatusOK || !contains(readRecorder.Body.String(), `"paper_id":1`) {
		t.Fatalf("mark read = %d %s", readRecorder.Code, readRecorder.Body.String())
	}
	firstReadPayload := readRecorder.Body.String()
	readAgainRecorder := httptest.NewRecorder()
	handler.ServeHTTP(readAgainRecorder, httptest.NewRequest(http.MethodPost, "/api/papers/1/read", nil))
	if readAgainRecorder.Code != http.StatusOK || readAgainRecorder.Body.String() != firstReadPayload {
		t.Fatalf("mark read again = %d %s", readAgainRecorder.Code, readAgainRecorder.Body.String())
	}

	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, httptest.NewRequest(http.MethodDelete, "/api/feedback/1", nil))
	if deleteRecorder.Code != http.StatusOK || !contains(deleteRecorder.Body.String(), `"deleted":true`) {
		t.Fatalf("delete feedback = %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestBootstrapProposalGenerateAndZoteroBridgeAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()
	seedReadOnlyFixture(t, settings.DatabasePath)

	bootstrapProfileFunc = func(_ config.Settings, interestDescription string, name *string, progress jobruntime.ProgressFunc) (map[string]any, error) {
		if progress != nil {
			progress("profile.bootstrap.generating", "Generating the initial classification profile proposal.")
		}
		result := map[string]any{"proposal_id": 1}
		if name != nil {
			result["name"] = *name
		}
		_ = interestDescription
		return result, nil
	}
	generateProfileProposalFunc = func(_ config.Settings, progress jobruntime.ProgressFunc) (map[string]any, error) {
		if progress != nil {
			progress("profile.proposal.collecting_feedback", "Collecting feedback for profile review.")
			progress("profile.proposal.generating", "Generating profile proposal.")
		}
		return map[string]any{"proposal_id": 2, "state": "pending"}, nil
	}
	listZoteroCollectionsFunc = func(_ config.Settings) (zoterosvc.CollectionsResponse, error) {
		return zoterosvc.CollectionsResponse{
			Collections: []zoterosvc.CollectionOption{{Key: "COLL-1", Name: "Inbox", PathLabel: "Inbox"}},
		}, nil
	}
	savePaperToZoteroFunc = func(_ config.Settings, paper store.Paper, classification store.Classification, collectionKey *string) (*string, error) {
		if paper.ID != 1 || classification.Relevance != "indirect" {
			t.Fatalf("unexpected zotero save input: paper=%#v classification=%#v", paper, classification)
		}
		if collectionKey == nil || *collectionKey != "COLL-1" {
			t.Fatalf("unexpected collection key: %#v", collectionKey)
		}
		itemKey := "ITEM-1"
		return &itemKey, nil
	}

	handler := NewServer(settings, nil).Handler()

	bootstrapRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/bootstrap", strings.NewReader(`{"interest_description":"RNA biology","name":"Alice"}`)))
	if bootstrapRecorder.Code != http.StatusOK || !contains(bootstrapRecorder.Body.String(), `"job_type":"profile-bootstrap"`) {
		t.Fatalf("bootstrap launch = %d %s", bootstrapRecorder.Code, bootstrapRecorder.Body.String())
	}
	var bootstrapPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(bootstrapRecorder.Body.Bytes(), &bootstrapPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, bootstrapPayload.Job.ID)
	bootstrapJob, ok := jobByID(bootstrapPayload.Job.ID)
	if !ok || bootstrapJob.Result["proposal_id"] != 1.0 && bootstrapJob.Result["proposal_id"] != 1 {
		t.Fatalf("unexpected bootstrap job result: %#v", bootstrapJob)
	}

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)

	generateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(generateRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/generate", nil))
	if generateRecorder.Code != http.StatusOK || !contains(generateRecorder.Body.String(), `"job_type":"profile-proposal"`) {
		t.Fatalf("proposal generate launch = %d %s", generateRecorder.Code, generateRecorder.Body.String())
	}
	var generatePayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(generateRecorder.Body.Bytes(), &generatePayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, generatePayload.Job.ID)
	generateJob, ok := jobByID(generatePayload.Job.ID)
	if !ok || generateJob.Result["proposal_id"] != 2.0 && generateJob.Result["proposal_id"] != 2 {
		t.Fatalf("unexpected proposal generate job result: %#v", generateJob)
	}

	collectionsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(collectionsRecorder, httptest.NewRequest(http.MethodGet, "/api/zotero/collections", nil))
	if collectionsRecorder.Code != http.StatusOK || !contains(collectionsRecorder.Body.String(), `"key":"COLL-1"`) {
		t.Fatalf("zotero collections = %d %s", collectionsRecorder.Code, collectionsRecorder.Body.String())
	}

	saveRecorder := httptest.NewRecorder()
	handler.ServeHTTP(saveRecorder, httptest.NewRequest(http.MethodPost, "/api/zotero/save/1", strings.NewReader(`{"collection_key":"COLL-1"}`)))
	if saveRecorder.Code != http.StatusOK || !contains(saveRecorder.Body.String(), `"item_key":"ITEM-1"`) {
		t.Fatalf("zotero save = %d %s", saveRecorder.Code, saveRecorder.Body.String())
	}
}

func TestProfileProposalApplyRejectAndReclassifyAPI(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	seedReadOnlyFixture(t, settings.DatabasePath)

	reclassifyPaperIDsFunc = func(_ config.Settings, paperIDs []int64, progress jobruntime.ProgressFunc) (int, error) {
		if progress != nil {
			progress("pipeline.classifier.classifying", "Classifying 1 paper(s).")
		}
		if len(paperIDs) == 0 {
			return 0, nil
		}
		return len(paperIDs), nil
	}
	selectReclassifyPaperIDsFunc = func(_ config.Settings, scope string, limit int) ([]int64, error) {
		return []int64{1}, nil
	}
	rebuildLatestReportFunc = func(_ config.Settings, progress jobruntime.ProgressFunc) (int, error) {
		if progress != nil {
			progress("pipeline.report.writing", "Publishing the latest report.")
		}
		return 1, nil
	}

	handler := NewServer(settings, nil).Handler()

	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/1/apply", nil))
	if applyRecorder.Code != http.StatusOK || !contains(applyRecorder.Body.String(), `"state":"applied"`) || !contains(applyRecorder.Body.String(), `"applied_version":2`) {
		t.Fatalf("apply proposal = %d %s", applyRecorder.Code, applyRecorder.Body.String())
	}
	appliedProfile, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(appliedProfile), `"version": 2`) {
		t.Fatalf("applied profile = %s", appliedProfile)
	}

	feedbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(feedbackRecorder, httptest.NewRequest(http.MethodGet, "/api/feedback", nil))
	if feedbackRecorder.Code != http.StatusOK || !contains(feedbackRecorder.Body.String(), `"used_in_profile":true`) {
		t.Fatalf("feedback after apply = %d %s", feedbackRecorder.Code, feedbackRecorder.Body.String())
	}

	rejectRecorder := httptest.NewRecorder()
	handler.ServeHTTP(rejectRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/1/reject", nil))
	if rejectRecorder.Code != http.StatusOK || !contains(rejectRecorder.Body.String(), `"state":"rejected"`) {
		t.Fatalf("reject proposal = %d %s", rejectRecorder.Code, rejectRecorder.Body.String())
	}

	reclassifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(reclassifyRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/reclassify", strings.NewReader(`{"scope":"all","limit":50}`)))
	if reclassifyRecorder.Code != http.StatusOK || !contains(reclassifyRecorder.Body.String(), `"job_type":"reclassify"`) {
		t.Fatalf("reclassify launch = %d %s", reclassifyRecorder.Code, reclassifyRecorder.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(reclassifyRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, payload.Job.ID)
	job, ok := jobByID(payload.Job.ID)
	if !ok || job.Result["reclassified"] != float64(1) && job.Result["reclassified"] != 1 {
		t.Fatalf("unexpected reclassify job payload: %#v", job)
	}
}

func testSettings(root string) config.Settings {
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
		ZoteroAPIKey:        "zotero-key",
		ZoteroLibraryType:   "user",
		ZoteroLibraryID:     "12345",
		ZoteroCollectionKey: "INBOX",
		ServerHost:          "127.0.0.1",
		ServerPort:          8000,
	}
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
  paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  1, 'indirect', 'direct', 'Should be visible as direct.', 'open', 0, '2026-05-16T00:20:00Z'
);
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

func execSQLite(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
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
	previousFetch := fetchUpdateManifestFunc
	previousBackendRunCommand := backendRunCommandFunc
	previousBootstrapProfile := bootstrapProfileFunc
	previousGenerateProfileProposal := generateProfileProposalFunc
	previousListZoteroCollections := listZoteroCollectionsFunc
	previousSavePaperToZotero := savePaperToZoteroFunc
	previousSelectReclassify := selectReclassifyPaperIDsFunc
	previousReclassify := reclassifyPaperIDsFunc
	previousRebuildLatestReport := rebuildLatestReportFunc
	previousRunOnce := runOnceFunc
	previousNow := nowFunc
	previousJobs := apiJobs
	apiJobs = jobRegistry{jobs: map[string]jobInfo{}}
	return func() {
		openExternalTargetFunc = previousOpen
		fetchUpdateManifestFunc = previousFetch
		backendRunCommandFunc = previousBackendRunCommand
		bootstrapProfileFunc = previousBootstrapProfile
		generateProfileProposalFunc = previousGenerateProfileProposal
		listZoteroCollectionsFunc = previousListZoteroCollections
		savePaperToZoteroFunc = previousSavePaperToZotero
		selectReclassifyPaperIDsFunc = previousSelectReclassify
		reclassifyPaperIDsFunc = previousReclassify
		rebuildLatestReportFunc = previousRebuildLatestReport
		runOnceFunc = previousRunOnce
		nowFunc = previousNow
		apiJobs = previousJobs
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
