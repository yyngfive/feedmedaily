package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
	_ "modernc.org/sqlite"
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

func TestSettingsConfigAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"version":"1.2.3"}`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, "src", "scirssagent", "__init__.py"), "")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_API_KEY=super-secret\n")
	handler := newTestHandler(t, testSettings(root))

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
	handler := newTestHandler(t, testSettings(root))

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
	handler := newTestHandler(t, settings)

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

func TestProfileCurrentPutUpdatesExistingProfile(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	handler := newTestHandler(t, settings)

	body := `{
		"meta":{"name":"Edited profile","version":1,"created_at":"2026-05-20T00:00:00Z","updated_at":"2026-05-20T00:00:00Z","source_description":"edited"},
		"scope":"RNA biology and splicing",
		"relevance_rules":{"direct":["RNA","Splicing"],"indirect":["Protein complexes"],"unrelated":["Plant biology"]},
		"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],
		"few_shots":[{"title":"Example paper","relevance":"direct","tags":["rna_bio"],"rationale":"Tracks RNA mechanisms."}]
	}`
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put profile = %d %s", recorder.Code, recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"name":"Edited profile"`) || !contains(recorder.Body.String(), `"version":3`) {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"created_at":"2026-05-10T00:00:00Z"`) {
		t.Fatalf("expected created_at preserved: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"source_description":"current"`) {
		t.Fatalf("expected source_description preserved: %s", recorder.Body.String())
	}
	if !contains(recorder.Body.String(), `"topic_taxonomy":[]`) || !contains(recorder.Body.String(), `"few_shots":[]`) {
		t.Fatalf("expected deprecated profile fields cleared in response: %s", recorder.Body.String())
	}

	saved, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(saved), `"version": 3`) || !contains(string(saved), `"name": "Edited profile"`) {
		t.Fatalf("saved profile = %s", saved)
	}
	if !contains(string(saved), `"topic_taxonomy": []`) || !contains(string(saved), `"few_shots": []`) {
		t.Fatalf("expected deprecated profile fields cleared on disk: %s", saved)
	}
}

func TestProfileCurrentPutRejectsInvalidOrMissingProfile(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	handler := newTestHandler(t, settings)

	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(`{}`)))
	if missingRecorder.Code != http.StatusBadRequest || !contains(missingRecorder.Body.String(), `No classification profile exists yet.`) {
		t.Fatalf("missing profile response = %d %s", missingRecorder.Code, missingRecorder.Body.String())
	}

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`)
	invalidRecorder := httptest.NewRecorder()
	invalidBody := `{"meta":{"name":"","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"","relevance_rules":{"direct":[],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`
	handler.ServeHTTP(invalidRecorder, httptest.NewRequest(http.MethodPut, "/api/profile/current", strings.NewReader(invalidBody)))
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile response = %d %s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}

func TestAppUpdateOpenAndSchedulerAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	manifestCalls := 0
	nowFunc = func() time.Time {
		return time.Date(2026, 5, 16, 7, 30, 0, 0, time.FixedZone("CST", 8*3600))
	}
	fetchUpdateManifestFunc = func(string) (map[string]any, error) {
		manifestCalls++
		return map[string]any{
			"version":           "9.9.9",
			"download_url":      "https://example.com/feedmedaily.exe",
			"release_notes_url": "https://example.com/release-notes",
		}, nil
	}
	backendRunCommandFunc = func(config.Settings) ([]string, error) {
		return []string{"bash", filepath.Join(root, "tools", "feedmedaily.sh"), "sync"}, nil
	}
	opened := ""
	openExternalTargetFunc = func(target string) error {
		opened = target
		return nil
	}
	settings.UpdateManifestURL = "https://example.com/update.json"
	handler := newTestHandler(t, settings)

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
	if manifestCalls != 1 {
		t.Fatalf("expected cached update status, manifestCalls=%d", manifestCalls)
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

func TestAppUpdateCachesSuccessfulManifestFetches(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	settings.UpdateManifestURL = "https://example.com/update.json"
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	manifestCalls := 0
	fetchUpdateManifestFunc = func(string) (map[string]any, error) {
		manifestCalls++
		return map[string]any{
			"version":           "9.9.9",
			"download_url":      "https://example.com/feedmedaily.exe",
			"release_notes_url": "https://example.com/release-notes",
		}, nil
	}
	handler := newTestHandler(t, settings)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if first.Code != http.StatusOK || !contains(first.Body.String(), `"has_update":true`) {
		t.Fatalf("first update response = %d %s", first.Code, first.Body.String())
	}

	currentTime = currentTime.Add(2 * time.Minute)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if second.Code != http.StatusOK || !contains(second.Body.String(), `"has_update":true`) {
		t.Fatalf("second update response = %d %s", second.Code, second.Body.String())
	}
	if manifestCalls != 1 {
		t.Fatalf("expected cached success response, manifestCalls=%d", manifestCalls)
	}
}

func TestAppUpdateForceBypassesCache(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	settings.UpdateManifestURL = "https://example.com/update.json"
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	manifestCalls := 0
	fetchUpdateManifestFunc = func(string) (map[string]any, error) {
		manifestCalls++
		return map[string]any{
			"version":           "9.9.9",
			"download_url":      "https://example.com/feedmedaily.exe",
			"release_notes_url": "https://example.com/release-notes",
		}, nil
	}
	handler := newTestHandler(t, settings)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if first.Code != http.StatusOK || !contains(first.Body.String(), `"checked_at":"2026-05-16T07:30:00Z"`) {
		t.Fatalf("first update response = %d %s", first.Code, first.Body.String())
	}

	currentTime = currentTime.Add(1 * time.Minute)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/app/update?force=1", nil))
	if second.Code != http.StatusOK || !contains(second.Body.String(), `"checked_at":"2026-05-16T07:31:00Z"`) {
		t.Fatalf("forced update response = %d %s", second.Code, second.Body.String())
	}
	if manifestCalls != 2 {
		t.Fatalf("expected force update to bypass cache, manifestCalls=%d", manifestCalls)
	}
}

func TestAppUpdateCachesFailuresBriefly(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	settings.UpdateManifestURL = "https://example.com/update.json"
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	manifestCalls := 0
	fetchUpdateManifestFunc = func(string) (map[string]any, error) {
		manifestCalls++
		return nil, errors.New("dial timeout")
	}
	handler := newTestHandler(t, settings)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if first.Code != http.StatusOK || !contains(first.Body.String(), `"status":"check_failed"`) {
		t.Fatalf("first failed update response = %d %s", first.Code, first.Body.String())
	}

	currentTime = currentTime.Add(20 * time.Second)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if second.Code != http.StatusOK || !contains(second.Body.String(), `"status":"check_failed"`) {
		t.Fatalf("second failed update response = %d %s", second.Code, second.Body.String())
	}
	if manifestCalls != 1 {
		t.Fatalf("expected cached failure response, manifestCalls=%d", manifestCalls)
	}

	currentTime = currentTime.Add(20 * time.Second)

	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if third.Code != http.StatusOK || !contains(third.Body.String(), `"status":"check_failed"`) {
		t.Fatalf("third failed update response = %d %s", third.Code, third.Body.String())
	}
	if manifestCalls != 2 {
		t.Fatalf("expected failure cache expiry, manifestCalls=%d", manifestCalls)
	}
}

func TestSettingsConfigUpdateRefreshesUpdateManifestURL(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	settings.UpdateManifestURL = "https://example.com/original-update.json"
	restore := stubAPIGlobals(t)
	defer restore()

	manifestURLs := make([]string, 0, 2)
	fetchUpdateManifestFunc = func(manifestURL string) (map[string]any, error) {
		manifestURLs = append(manifestURLs, manifestURL)
		return map[string]any{
			"version":           "9.9.9",
			"download_url":      "https://example.com/feedmedaily.exe",
			"release_notes_url": "https://example.com/release-notes",
		}, nil
	}
	handler := newTestHandler(t, settings)

	firstUpdate := httptest.NewRecorder()
	handler.ServeHTTP(firstUpdate, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if firstUpdate.Code != http.StatusOK {
		t.Fatalf("first update response = %d %s", firstUpdate.Code, firstUpdate.Body.String())
	}

	configRecorder := httptest.NewRecorder()
	configBody := `{"fields":{"FEEDMEDAILY_UPDATE_MANIFEST_URL":{"value":"https://mirror.example.com/update.json"}}}`
	handler.ServeHTTP(configRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", strings.NewReader(configBody)))
	if configRecorder.Code != http.StatusOK || !contains(configRecorder.Body.String(), `"key":"FEEDMEDAILY_UPDATE_MANIFEST_URL"`) {
		t.Fatalf("settings update response = %d %s", configRecorder.Code, configRecorder.Body.String())
	}

	metaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metaRecorder, httptest.NewRequest(http.MethodGet, "/api/app/meta", nil))
	if metaRecorder.Code != http.StatusOK || !contains(metaRecorder.Body.String(), `"update_manifest_url":"https://mirror.example.com/update.json"`) {
		t.Fatalf("meta response = %d %s", metaRecorder.Code, metaRecorder.Body.String())
	}

	secondUpdate := httptest.NewRecorder()
	handler.ServeHTTP(secondUpdate, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if secondUpdate.Code != http.StatusOK {
		t.Fatalf("second update response = %d %s", secondUpdate.Code, secondUpdate.Body.String())
	}
	if len(manifestURLs) != 2 {
		t.Fatalf("expected cache reset after settings change, manifestURLs=%v", manifestURLs)
	}
	if manifestURLs[0] != "https://example.com/original-update.json" || manifestURLs[1] != "https://mirror.example.com/update.json" {
		t.Fatalf("unexpected manifest URLs: %v", manifestURLs)
	}
}

func TestAdminSyncJob(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, _ jobruntime.RunOptions, progress jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if progress != nil {
			progress(jobruntime.ItemProgress("pipeline.feeds.fetching", "fetch", 1, 1, "Test Journal", "Fetching feed 1/1: Test Journal."))
			progress(jobruntime.PercentProgress("pipeline.metadata.enriching", "metadata", 1, 1, "Getting metadata 1/1 (100%)."))
			progress(jobruntime.PercentProgress("pipeline.classifier.classifying", "classification", 1, 1, "Classifying papers 1/1 (100%)."))
			progress(jobruntime.ProgressUpdate{MessageKey: "pipeline.report.refreshing", Message: "Refreshing the latest report from SQLite.", Stage: "report"})
		}
		return jobruntime.RunSummary{
			Fetched:    2,
			Inserted:   1,
			Updated:    1,
			Classified: 1,
			Errors:     []string{"https://bad.feed/: timeout"},
		}, nil
	}
	handler := newTestHandler(t, testSettings(root))

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

	jobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jobRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs/"+runPayload.Job.ID, nil))
	if jobRecorder.Code != http.StatusOK || !contains(jobRecorder.Body.String(), `"status":"completed"`) || !contains(jobRecorder.Body.String(), `"fetched":2`) {
		t.Fatalf("job detail = %d %s", jobRecorder.Code, jobRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs", nil))
	if listRecorder.Code != http.StatusOK || !contains(listRecorder.Body.String(), `"job_type":"sync"`) {
		t.Fatalf("job list = %d %s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestFeedbackReadAndDeleteMutationAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	seedReadOnlyFixture(t, settings.DatabasePath)
	handler := newTestHandler(t, settings)

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
			progress(jobruntime.StepProgress("profile.bootstrap.generating", "profile-bootstrap", 2, 4, "Step 2/4 (50%): Generating initial profile proposal."))
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
			progress(jobruntime.StepProgress("profile.proposal.collecting_feedback", "profile-proposal", 1, 4, "Step 1/4 (25%): Collecting feedback and current profile context."))
			progress(jobruntime.StepProgress("profile.proposal.generating", "profile-proposal", 2, 4, "Step 2/4 (50%): Generating profile proposal."))
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

	handler := newTestHandler(t, settings)

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
			progress(jobruntime.PercentProgress("pipeline.classifier.classifying", "classification", 1, 1, "Classifying papers 1/1 (100%)."))
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
			progress(jobruntime.ProgressUpdate{MessageKey: "pipeline.report.refreshing", Message: "Refreshing the latest report from SQLite.", Stage: "report"})
		}
		return 1, nil
	}

	handler := newTestHandler(t, settings)

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

func TestCompactProfileProposalApplyUsesAcceptedChanges(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":1,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}`)
	seedCompactProposalFixture(t, settings.DatabasePath)

	reclassifyPaperIDsFunc = func(_ config.Settings, paperIDs []int64, _ jobruntime.ProgressFunc) (int, error) {
		return len(paperIDs), nil
	}
	rebuildLatestReportFunc = func(_ config.Settings, _ jobruntime.ProgressFunc) (int, error) {
		return 1, nil
	}

	handler := newTestHandler(t, settings)
	body := `{"accepted_change_ids":["change-1"],"rejected_change_ids":["change-2"]}`
	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/21/apply", strings.NewReader(body)))
	if applyRecorder.Code != http.StatusOK || !contains(applyRecorder.Body.String(), `"state":"applied"`) || !contains(applyRecorder.Body.String(), `"status":"accepted"`) || !contains(applyRecorder.Body.String(), `"status":"rejected"`) {
		t.Fatalf("compact apply = %d %s", applyRecorder.Code, applyRecorder.Body.String())
	}

	appliedProfile, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(appliedProfile), `"RNA chemistry"`) || !contains(string(appliedProfile), `"Plant biology"`) {
		t.Fatalf("applied compact profile = %s", appliedProfile)
	}
	if contains(string(appliedProfile), `"RNA",`) {
		t.Fatalf("expected accepted rewrite to replace the old direct rule: %s", appliedProfile)
	}
}

func TestCompactProfileProposalApplyRejectsVersionMismatch(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	writeFile(t, settings.ProfilePath, `{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-12T00:00:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}`)
	seedCompactProposalFixture(t, settings.DatabasePath)

	handler := newTestHandler(t, settings)
	body := `{"accepted_change_ids":["change-1"],"rejected_change_ids":[]}`
	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/proposals/21/apply", strings.NewReader(body)))
	if applyRecorder.Code != http.StatusConflict || !contains(applyRecorder.Body.String(), "Regenerate the proposal") {
		t.Fatalf("version mismatch apply = %d %s", applyRecorder.Code, applyRecorder.Body.String())
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
) VALUES (
  1, 'https://example.com/rss', 'Fixture Feed', 'Compact proposal paper', 'https://example.com/compact-paper',
  '10.1000/compact', 'Example Journal', '["Alice","Bob"]', 'Abstract text.',
  'rss', '2026-05-15', '2026-05-16T00:00:00Z', NULL,
  '{"_abstract_html":"<p>Abstract text.</p>","_abstract_images":[]}'
);
INSERT INTO classifications (
  paper_id, relevance, confidence, reason, topic_tags_json, recommended_action, model, translated_title_zh, classified_at
) VALUES (
  1, 'indirect', 0.8, 'Fixture', '[]', 'scan', 'test', NULL, '2026-05-16T00:10:00Z'
);
INSERT INTO feedback (
  id, paper_id, original_relevance, corrected_relevance, note, state, used_in_prompt, created_at
) VALUES (
  1, 1, 'indirect', 'direct', 'Should be direct.', 'open', 0, '2026-05-16T00:20:00Z'
);
INSERT INTO profile_proposals (
  id, summary, proposed_profile_json, rule_delta_json, base_profile_version, change_set_json, source_feedback_ids_json, model, state, created_at
) VALUES (
  21, 'Compact proposal summary',
  '{"meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-16T00:30:00Z","source_description":"current"},"scope":"RNA biology","relevance_rules":{"direct":["RNA chemistry"],"indirect":["Background"],"unrelated":["Plant biology"]},"topic_taxonomy":[],"few_shots":[]}',
  '{"summary":"Compact proposal summary","direct_rule_additions":[],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}',
  1,
  '[{"id":"change-1","section":"direct_rule","operation":"rewrite","summary":"Promote chemistry-first RNA work.","text_before":["RNA"],"text_after":["RNA chemistry"],"topic_before":[],"topic_after":[],"rationale":"The feedback shows chemistry-first RNA papers should be direct.","source_feedback_ids":[1],"source_paper_ids":[1],"status":"proposed"},{"id":"change-2","section":"unrelated_rule","operation":"remove","summary":"Drop the unrelated rule.","text_before":["Plant biology"],"text_after":[],"topic_before":[],"topic_after":[],"rationale":"A review candidate that can be declined.","source_feedback_ids":[1],"source_paper_ids":[1],"status":"proposed"}]',
  '[1]', 'deepseek-v4-pro', 'pending', '2026-05-16T00:30:00Z'
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
	previousRunSync := runSyncFunc
	previousStartVerification := startVerificationFlowFunc
	previousCompleteVerification := completeVerificationFlowFunc
	previousNow := nowFunc
	previousJobs := apiJobs
	previousVerifications := apiVerifications
	apiJobs = jobRegistry{jobs: map[string]jobInfo{}}
	apiVerifications = verificationRegistry{items: map[string]*pendingVerification{}}
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
		runSyncFunc = previousRunSync
		startVerificationFlowFunc = previousStartVerification
		completeVerificationFlowFunc = previousCompleteVerification
		nowFunc = previousNow
		apiJobs = previousJobs
		apiVerifications = previousVerifications
	}
}

func TestAdminRunWaitsForCloudflareVerificationAndResumes(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runCalls := 0
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, progress jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		runCalls++
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:    "https://chemrxiv.org/action/showFeed?type=latest&format=rss",
					Target: "cloudflare",
					Reason: "challenge",
				}},
			}
		}
		if string(opts.FeedBodyOverrides["https://chemrxiv.org/action/showFeed?type=latest&format=rss"]) != "<rdf:RDF />" {
			t.Fatalf("unexpected override body: %#v", opts.FeedBodyOverrides)
		}
		return jobruntime.RunSummary{
			Fetched:    1,
			Inserted:   1,
			Updated:    0,
			Classified: 1,
			Errors:     nil,
		}, nil
	}

	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatalf("expected pending verification")
		}
		if pending.FeedURL != "https://chemrxiv.org/action/showFeed?type=latest&format=rss" {
			t.Fatalf("unexpected feed url: %q", pending.FeedURL)
		}
		return nil
	}
	handler := newTestHandler(t, testSettings(root))

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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	job, ok := jobByID(runPayload.Job.ID)
	if !ok || !job.VerificationRequired || job.VerificationTarget != "cloudflare" {
		t.Fatalf("unexpected waiting job: %#v", job)
	}
	pending, ok := pendingVerificationForJob(runPayload.Job.ID, "https://chemrxiv.org/action/showFeed?type=latest&format=rss")
	if !ok {
		t.Fatalf("expected pending verification")
	}

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://chemrxiv.org/action/showFeed?type=latest&format=rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}
	callbackBody := `{"verification_id":"` + pending.ID + `","status":"success","content_type":"application/xml","feed_xml":"<rdf:RDF />","error":""}`
	callbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callbackRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/callback", strings.NewReader(callbackBody)))
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("verification callback = %d %s", callbackRecorder.Code, callbackRecorder.Body.String())
	}

	waitForJobCompletion(t, runPayload.Job.ID)
	completedJob, ok := jobByID(runPayload.Job.ID)
	if !ok || completedJob.Status != "completed" {
		t.Fatalf("unexpected completed job: %#v", completedJob)
	}
	if runCalls != 2 {
		t.Fatalf("runCalls = %d", runCalls)
	}
}

func TestAdminRunCanFallbackToBrowserManualXMLAfterVerifierAbort(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	opened := ""
	openExternalTargetFunc = func(target string) error {
		opened = target
		return nil
	}
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if string(opts.FeedBodyOverrides["https://www.cell.com/cell/current.rss"]) == "<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>" {
			return jobruntime.RunSummary{
				Fetched:    1,
				Inserted:   1,
				Updated:    0,
				Classified: 1,
				Errors:     nil,
			}, nil
		}
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:     "https://www.cell.com/cell/current.rss",
					Target:  "cloudflare",
					Reason:  "challenge",
					Journal: "Cell",
				}},
			}
		}
		t.Fatalf("unexpected resumed run: %#v", opts.FeedBodyOverrides)
		return jobruntime.RunSummary{}, nil
	}

	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatalf("expected pending verification")
		}
		if pending.FeedURL != "https://www.cell.com/cell/current.rss" {
			t.Fatalf("unexpected feed url: %q", pending.FeedURL)
		}
		return nil
	}
	handler := newTestHandler(t, testSettings(root))

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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
	pending, ok := pendingVerificationForJob(runPayload.Job.ID, "https://www.cell.com/cell/current.rss")
	if !ok {
		t.Fatalf("expected pending verification")
	}

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}
	callbackBody := `{"verification_id":"` + pending.ID + `","status":"aborted","content_type":"","feed_xml":"","error":"Cloudflare challenge was not completed before the window closed."}`
	callbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callbackRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/callback", strings.NewReader(callbackBody)))
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("verification callback = %d %s", callbackRecorder.Code, callbackRecorder.Body.String())
	}

	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
	waitingJob, ok := jobByID(runPayload.Job.ID)
	if !ok || waitingJob.VerificationMethod != verificationMethodWebview {
		t.Fatalf("expected waiting job after verifier abort: %#v", waitingJob)
	}

	browserRecorder := httptest.NewRecorder()
	handler.ServeHTTP(browserRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/browser", strings.NewReader(startBody)))
	if browserRecorder.Code != http.StatusOK {
		t.Fatalf("verification browser = %d %s", browserRecorder.Code, browserRecorder.Body.String())
	}
	if opened != "https://www.cell.com/cell/current.rss" {
		t.Fatalf("unexpected opened target: %q", opened)
	}
	waitingJob, ok = jobByID(runPayload.Job.ID)
	if !ok || waitingJob.VerificationMethod != verificationMethodBrowserManual {
		t.Fatalf("expected browser fallback waiting job: %#v", waitingJob)
	}

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusOK {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}

	waitForJobCompletion(t, runPayload.Job.ID)
}

func TestVerificationCompleteRequiresCallbackXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:     "https://www.cell.com/cell/current.rss",
					Target:  "cloudflare",
					Reason:  "challenge",
					Journal: "Cell",
				}},
			}
		}
		t.Fatalf("did not expect resumed run without callback data")
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))

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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	startBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/start", strings.NewReader(startBody)))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("verification start = %d %s", startRecorder.Code, startRecorder.Body.String())
	}

	completeBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss"}`
	completeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(completeRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/complete", strings.NewReader(completeBody)))
	if completeRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification complete = %d %s", completeRecorder.Code, completeRecorder.Body.String())
	}
	if !contains(completeRecorder.Body.String(), "has not returned RSS XML yet") {
		t.Fatalf("unexpected verification complete error: %s", completeRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsEmptyXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:     "https://www.cell.com/cell/current.rss",
					Target:  "cloudflare",
					Reason:  "challenge",
					Journal: "Cell",
				}},
			}
		}
		t.Fatalf("did not expect resumed run")
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"   "}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	if !contains(manualRecorder.Body.String(), "Feed XML is required.") {
		t.Fatalf("unexpected manual submit error: %s", manualRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsMalformedXML(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:     "https://www.cell.com/cell/current.rss",
					Target:  "cloudflare",
					Reason:  "challenge",
					Journal: "Cell",
				}},
			}
		}
		t.Fatalf("did not expect resumed run")
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<html><body>challenge</body></html>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")
}

func TestVerificationManualSubmitRejectsWrongFeedURL(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if len(opts.FeedBodyOverrides) == 0 {
			return jobruntime.RunSummary{}, &jobruntime.VerificationRequiredError{
				Requests: []feeds.VerificationRequest{{
					URL:     "https://www.cell.com/cell/current.rss",
					Target:  "cloudflare",
					Reason:  "challenge",
					Journal: "Cell",
				}},
			}
		}
		t.Fatalf("did not expect resumed run")
		return jobruntime.RunSummary{}, nil
	}
	startVerificationFlowFunc = func(_ config.Settings, _ *pendingVerification) error {
		return nil
	}

	handler := newTestHandler(t, testSettings(root))
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
	waitForJobStatus(t, runPayload.Job.ID, "waiting_for_user")

	manualBody := `{"job_id":"` + runPayload.Job.ID + `","feed_url":"https://wrong.example/rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusNotFound {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
}

func TestVerificationManualSubmitRejectsWhenJobNotWaiting(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	handler := newTestHandler(t, testSettings(root))
	storeJob(jobInfo{
		ID:        "job-ready",
		JobType:   "sync",
		Status:    "completed",
		CreatedAt: time.Now().UTC(),
	})
	manualBody := `{"job_id":"job-ready","feed_url":"https://www.cell.com/cell/current.rss","feed_xml":"<rss><channel><title>Cell</title><item><title>Paper</title><link>https://example.com/paper</link></item></channel></rss>"}`
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, httptest.NewRequest(http.MethodPost, "/api/feeds/verification/manual-submit", strings.NewReader(manualBody)))
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("verification manual submit = %d %s", manualRecorder.Code, manualRecorder.Body.String())
	}
	if !contains(manualRecorder.Body.String(), "Job is not waiting for manual verification.") {
		t.Fatalf("unexpected manual submit error: %s", manualRecorder.Body.String())
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
