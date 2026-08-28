package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
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

func TestSettingsConfigAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"version":"1.2.3"}`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
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

func TestClassifierModelsConfigAPI(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"version":"1.2.3"}`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_API_KEY=legacy-secret\nSCIRSS_CLASSIFIER_MODEL=deepseek-v4-flash\n")
	handler := newTestHandler(t, testSettings(root))

	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/settings/config", nil))
	if getRecorder.Code != http.StatusOK || contains(getRecorder.Body.String(), "legacy-secret") {
		t.Fatalf("structured settings response = %d %s", getRecorder.Code, getRecorder.Body.String())
	}
	var getPayload struct {
		ClassifierModels config.ClassifierModelsResponse `json:"classifier_models"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &getPayload); err != nil {
		t.Fatal(err)
	}
	if getPayload.ClassifierModels.DefaultModelID != config.ClassifierModelDeepSeekV4Flash {
		t.Fatalf("default classifier model = %q", getPayload.ClassifierModels.DefaultModelID)
	}

	putBody := strings.NewReader(`{"fields":{},"classifier_models":{"enabled_model_ids":["deepseek-v4-flash","glm-5.3-flash"],"default_model_id":"glm-5.3-flash","credentials":{"deepseek-v4-flash":{"value":"deepseek-new"},"glm-5.3-flash":{"value":"glm-new"}}}}`)
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", putBody))
	if putRecorder.Code != http.StatusOK || contains(putRecorder.Body.String(), "deepseek-new") || contains(putRecorder.Body.String(), "glm-new") {
		t.Fatalf("structured settings update = %d %s", putRecorder.Code, putRecorder.Body.String())
	}
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModel != config.ClassifierModelGLM53Flash || settings.ClassifierAPIKey != "glm-new" {
		t.Fatalf("updated effective classifier = %#v", settings.EffectiveClassifierModel())
	}

	unknownBody := strings.NewReader(`{"fields":{},"classifier_models":{"enabled_model_ids":["unknown-model"],"default_model_id":"unknown-model","credentials":{}}}`)
	unknownRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unknownRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", unknownBody))
	if unknownRecorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown model status = %d %s", unknownRecorder.Code, unknownRecorder.Body.String())
	}
}

func TestClassifierDefaultChangeOnlyAffectsLaterJobs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"version":"1.2.3"}`)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_ENABLED_MODELS=deepseek-v4-flash,glm-5.3-flash\nSCIRSS_CLASSIFIER_DEFAULT_MODEL=deepseek-v4-flash\nSCIRSS_DEEPSEEK_API_KEY=deepseek-key\nSCIRSS_GLM_API_KEY=glm-key\n")
	restore := stubAPIGlobals(t)
	defer restore()
	seenModels := make(chan string, 2)
	releaseJob := make(chan struct{})
	runSyncFunc = func(settings config.Settings, _ jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		seenModels <- settings.EffectiveClassifierModelName()
		<-releaseJob
		return jobruntime.RunSummary{}, nil
	}
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(t, settings)
	launch := func() string {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("sync launch status = %d: %s", recorder.Code, recorder.Body.String())
		}
		var payload struct {
			Job jobInfo `json:"job"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload.Job.ID
	}

	firstJobID := launch()
	select {
	case model := <-seenModels:
		if model != config.ClassifierModelDeepSeekV4Flash {
			t.Fatalf("first job model = %q", model)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first sync did not start")
	}
	configBody := strings.NewReader(`{"fields":{},"classifier_models":{"enabled_model_ids":["deepseek-v4-flash","glm-5.3-flash"],"default_model_id":"glm-5.3-flash","credentials":{}}}`)
	configRecorder := httptest.NewRecorder()
	handler.ServeHTTP(configRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", configBody))
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("classifier default update status = %d: %s", configRecorder.Code, configRecorder.Body.String())
	}
	close(releaseJob)
	waitForJobCompletion(t, firstJobID)

	// The first release channel is closed, so a second job can finish immediately too.
	secondJobID := launch()
	select {
	case model := <-seenModels:
		if model != config.ClassifierModelGLM53Flash {
			t.Fatalf("later job model = %q", model)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("later sync did not start")
	}
	waitForJobCompletion(t, secondJobID)
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

func TestAppUpdateOpenAndSchedulerAPIs(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	txtCalls := 0
	nowFunc = func() time.Time {
		return time.Date(2026, 5, 16, 7, 30, 0, 0, time.FixedZone("CST", 8*3600))
	}
	lookupUpdateTXTFunc = func(string) ([]string, error) {
		txtCalls++
		return []string{"version=9.9.9;url=https://example.com/release"}, nil
	}
	backendRunCommandFunc = func(config.Settings) ([]string, error) {
		return []string{"bash", filepath.Join(root, "tools", "feedmedaily.sh"), "sync"}, nil
	}
	opened := ""
	openExternalTargetFunc = func(target string) error {
		opened = target
		return nil
	}
	notifyCalls := 0
	notifyTraySettingsChangedFunc = func(configDir string) error {
		notifyCalls++
		if configDir != settings.ConfigDir {
			t.Fatalf("notify configDir = %q, want %q", configDir, settings.ConfigDir)
		}
		return nil
	}
	handler := newTestHandler(t, settings)

	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if updateRecorder.Code != http.StatusOK || !contains(updateRecorder.Body.String(), `"has_update":true`) {
		t.Fatalf("update response = %d %s", updateRecorder.Code, updateRecorder.Body.String())
	}

	openRecorder := httptest.NewRecorder()
	handler.ServeHTTP(openRecorder, httptest.NewRequest(http.MethodPost, "/api/app/open", strings.NewReader(`{"target":"download_url"}`)))
	if openRecorder.Code != http.StatusOK || opened != "https://example.com/release" {
		t.Fatalf("open response = %d %s opened=%q", openRecorder.Code, openRecorder.Body.String(), opened)
	}
	if txtCalls != 1 {
		t.Fatalf("expected cached update status, txtCalls=%d", txtCalls)
	}
	if err := os.MkdirAll(settings.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.LogsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for target, expected := range map[string]string{
		"data_dir":    settings.DataDir,
		"logs_dir":    settings.LogsDir,
		"install_dir": settings.AppDir,
	} {
		opened = ""
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/app/open", strings.NewReader(fmt.Sprintf(`{"target":%q}`, target))))
		if recorder.Code != http.StatusOK || opened != expected {
			t.Fatalf("open %s response = %d %s opened=%q want=%q", target, recorder.Code, recorder.Body.String(), opened, expected)
		}
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
	if notifyCalls != 1 {
		t.Fatalf("notify calls after scheduler put = %d, want 1", notifyCalls)
	}

	schedulerDelete := httptest.NewRecorder()
	handler.ServeHTTP(schedulerDelete, httptest.NewRequest(http.MethodDelete, "/api/settings/scheduler", nil))
	if schedulerDelete.Code != http.StatusOK || !contains(schedulerDelete.Body.String(), `"installed":false`) {
		t.Fatalf("scheduler delete = %d %s", schedulerDelete.Code, schedulerDelete.Body.String())
	}
	if notifyCalls != 2 {
		t.Fatalf("notify calls after scheduler delete = %d, want 2", notifyCalls)
	}
}

func TestAppUpdateCachesSuccessfulManifestFetches(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	txtCalls := 0
	lookupUpdateTXTFunc = func(string) ([]string, error) {
		txtCalls++
		return []string{"version=9.9.9;url=https://example.com/release"}, nil
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
	if txtCalls != 1 {
		t.Fatalf("expected cached success response, txtCalls=%d", txtCalls)
	}
}

func TestAppUpdateReportsUpToDateFromDNSTXT(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	currentVersion := appruntime.PackageVersion(root)
	lookupUpdateTXTFunc = func(string) ([]string, error) {
		return []string{"version=" + currentVersion + ";url=https://example.com/release"}, nil
	}
	handler := newTestHandler(t, settings)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if recorder.Code != http.StatusOK || !contains(recorder.Body.String(), `"status":"up_to_date"`) || !contains(recorder.Body.String(), `"has_update":false`) {
		t.Fatalf("update response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAppUpdateForceBypassesCache(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	txtCalls := 0
	lookupUpdateTXTFunc = func(string) ([]string, error) {
		txtCalls++
		return []string{"version=9.9.9;url=https://example.com/release"}, nil
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
	if txtCalls != 2 {
		t.Fatalf("expected force update to bypass cache, txtCalls=%d", txtCalls)
	}
}

func TestAppUpdateCachesFailuresBriefly(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	currentTime := time.Date(2026, 5, 16, 7, 30, 0, 0, time.UTC)
	nowFunc = func() time.Time {
		return currentTime
	}
	txtCalls := 0
	lookupUpdateTXTFunc = func(string) ([]string, error) {
		txtCalls++
		return nil, errors.New("dns timeout")
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
	if txtCalls != 1 {
		t.Fatalf("expected cached failure response, txtCalls=%d", txtCalls)
	}

	currentTime = currentTime.Add(20 * time.Second)

	third := httptest.NewRecorder()
	handler.ServeHTTP(third, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if third.Code != http.StatusOK || !contains(third.Body.String(), `"status":"check_failed"`) {
		t.Fatalf("third failed update response = %d %s", third.Code, third.Body.String())
	}
	if txtCalls != 2 {
		t.Fatalf("expected failure cache expiry, txtCalls=%d", txtCalls)
	}
}

func TestAppUpdateFailsWhenDNSTXTIsIncomplete(t *testing.T) {
	root := t.TempDir()
	settings := testSettings(root)
	restore := stubAPIGlobals(t)
	defer restore()

	lookupUpdateTXTFunc = func(string) ([]string, error) {
		return []string{"version=9.9.9"}, nil
	}
	handler := newTestHandler(t, settings)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/app/update", nil))
	if recorder.Code != http.StatusOK || !contains(recorder.Body.String(), `"status":"check_failed"`) {
		t.Fatalf("update response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestSettingsConfigUpdateRefreshesSettingsForBootstrap(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "user-data")
	t.Setenv("FEEDMEDAILY_RUNTIME_MODE", "release")
	t.Setenv("FEEDMEDAILY_DATA_ROOT", dataRoot)
	restore := stubAPIGlobals(t)
	defer restore()

	seenProfileKey := make(chan string, 1)
	bootstrapProfileFunc = func(settings config.Settings, _ string, _ *string, _ jobruntime.ProgressFunc, _ ...*llmusage.Collector) (map[string]any, error) {
		seenProfileKey <- settings.ProfileAPIKey
		return map[string]any{"proposal_id": 1}, nil
	}
	handler := newTestHandler(t, testSettings(root))

	configRecorder := httptest.NewRecorder()
	configBody := `{"fields":{"SCIRSS_PROFILE_API_KEY":{"value":"release-profile-key"}}}`
	handler.ServeHTTP(configRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", strings.NewReader(configBody)))
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("settings update response = %d %s", configRecorder.Code, configRecorder.Body.String())
	}

	bootstrapRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bootstrapRecorder, httptest.NewRequest(http.MethodPost, "/api/profile/bootstrap", strings.NewReader(`{"interest_description":"RNA biology"}`)))
	if bootstrapRecorder.Code != http.StatusOK {
		t.Fatalf("bootstrap response = %d %s", bootstrapRecorder.Code, bootstrapRecorder.Body.String())
	}
	var bootstrapPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(bootstrapRecorder.Body.Bytes(), &bootstrapPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, bootstrapPayload.Job.ID)

	select {
	case profileKey := <-seenProfileKey:
		if profileKey != "release-profile-key" {
			t.Fatalf("bootstrap saw stale profile key %q", profileKey)
		}
	default:
		t.Fatal("bootstrap did not receive settings")
	}
}

func TestSettingsConfigUpdateAppliesManualPricingToNextJob(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "user-data")
	t.Setenv("FEEDMEDAILY_RUNTIME_MODE", "release")
	t.Setenv("FEEDMEDAILY_DATA_ROOT", dataRoot)
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		opts.Usage.Record(llmusage.Event{
			BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Operation: "classification",
			OccurredAt: time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC),
			Usage:      llmusage.ResponseUsage{CompletionTokens: 1_000_000, CacheBreakdownPresent: true},
		})
		return jobruntime.RunSummary{Classified: 1}, nil
	}
	handler := newTestHandler(t, testSettings(root))

	configRecorder := httptest.NewRecorder()
	configBody := `{"fields":{"SCIRSS_DEEPSEEK_FLASH_PEAK_OUTPUT_CNY_PER_MILLION":{"value":"10"}}}`
	handler.ServeHTTP(configRecorder, httptest.NewRequest(http.MethodPut, "/api/settings/config", strings.NewReader(configBody)))
	if configRecorder.Code != http.StatusOK {
		t.Fatalf("settings update response = %d %s", configRecorder.Code, configRecorder.Body.String())
	}

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run response = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, payload.Job.ID)
	job, ok := jobByID(payload.Job.ID)
	if !ok || job.LLMUsage == nil || job.LLMUsage.EstimatedCostCNY == nil || *job.LLMUsage.EstimatedCostCNY != "10.000000" {
		t.Fatalf("job pricing = %#v", job.LLMUsage)
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
