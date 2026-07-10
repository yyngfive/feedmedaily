package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
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
	bootstrapProfileFunc = func(settings config.Settings, _ string, _ *string, _ jobruntime.ProgressFunc) (map[string]any, error) {
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
