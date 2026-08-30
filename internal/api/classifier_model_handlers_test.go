package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
)

func TestClassifierModelTestAPIUsesUnsavedKeyWithoutChangingSettings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_ENABLED_MODELS=deepseek-v4-flash,glm-5.3-flash\nSCIRSS_CLASSIFIER_DEFAULT_MODEL=deepseek-v4-flash\nSCIRSS_CLASSIFIER_THINKING=enabled\nSCIRSS_DEEPSEEK_API_KEY=deepseek-saved\nSCIRSS_GLM_API_KEY=glm-saved\n")
	restore := stubAPIGlobals(t)
	defer restore()

	var captured classifier.LLMConfig
	testClassifierConnectionFunc = func(cfg classifier.LLMConfig) error {
		captured = cfg
		return nil
	}
	settings, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(settings, nil)
	defer server.Close()

	const temporaryKey = "temporary-test-key"
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/settings/classifier-models/test", strings.NewReader(`{"model_id":"glm-5.3-flash","api_key":"`+temporaryKey+`"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("test endpoint status = %d: %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	job := waitForJobTerminalStatus(t, payload.Job.ID)
	if job.Status != "completed" {
		t.Fatalf("connection test job status = %s: %s", job.Status, job.Error)
	}
	if captured.APIKey != temporaryKey || captured.Model != config.ClassifierModelGLM53Flash || captured.Provider != "zhipu" || captured.Thinking != "enabled" || captured.ReasoningEffort != "low" || !captured.UseConfiguredProviderControls || captured.MinMaxTokens != 0 {
		t.Fatalf("captured GLM config = %#v", captured)
	}

	deepSeekRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(deepSeekRecorder, httptest.NewRequest(http.MethodPost, "/api/settings/classifier-models/test", strings.NewReader(`{"model_id":"deepseek-v4-flash"}`)))
	if deepSeekRecorder.Code != http.StatusOK {
		t.Fatalf("DeepSeek test endpoint status = %d: %s", deepSeekRecorder.Code, deepSeekRecorder.Body.String())
	}
	if err := json.Unmarshal(deepSeekRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	job = waitForJobTerminalStatus(t, payload.Job.ID)
	if job.Status != "completed" {
		t.Fatalf("DeepSeek connection test job status = %s: %s", job.Status, job.Error)
	}
	if captured.Model != config.ClassifierModelDeepSeekV4Flash || captured.Thinking != "enabled" || captured.ReasoningEffort != "low" || captured.MinMaxTokens != classifier.ThinkingMaxTokensFloor {
		t.Fatalf("captured DeepSeek config = %#v", captured)
	}
	if got := server.snapshotSettings().ClassifierModels.DefaultModelID; got != config.ClassifierModelDeepSeekV4Flash {
		t.Fatalf("connection test changed default model to %q", got)
	}
	envText, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envText), temporaryKey) {
		t.Fatal("temporary connection-test key was persisted")
	}
	logText, err := os.ReadFile(job.LogPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logText), temporaryKey) {
		t.Fatal("temporary connection-test key was written to the job log")
	}
}

func TestClassifierModelTestAPIReturnsValidationAndJobErrors(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeFile(t, filepath.Join(root, ".env"), "SCIRSS_DEEPSEEK_API_KEY=deepseek-saved\n")
	restore := stubAPIGlobals(t)
	defer restore()
	server := NewServer(func() config.Settings {
		settings, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		return settings
	}(), nil)
	defer server.Close()

	missing := httptest.NewRecorder()
	server.Handler().ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/api/settings/classifier-models/test", strings.NewReader(`{"model_id":"glm-5.3-flash"}`)))
	if missing.Code != http.StatusBadRequest || !strings.Contains(missing.Body.String(), "API key is required") {
		t.Fatalf("missing key response = %d: %s", missing.Code, missing.Body.String())
	}
	unknown := httptest.NewRecorder()
	server.Handler().ServeHTTP(unknown, httptest.NewRequest(http.MethodPost, "/api/settings/classifier-models/test", strings.NewReader(`{"model_id":"unknown"}`)))
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "unsupported classifier model") {
		t.Fatalf("unknown model response = %d: %s", unknown.Code, unknown.Body.String())
	}

	testClassifierConnectionFunc = func(classifier.LLMConfig) error { return errors.New("provider rejected request") }
	failure := httptest.NewRecorder()
	server.Handler().ServeHTTP(failure, httptest.NewRequest(http.MethodPost, "/api/settings/classifier-models/test", strings.NewReader(`{"model_id":"deepseek-v4-flash"}`)))
	if failure.Code != http.StatusOK {
		t.Fatalf("failed test launch response = %d: %s", failure.Code, failure.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(failure.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	job := waitForJobTerminalStatus(t, payload.Job.ID)
	if job.Status != "failed" || !strings.Contains(job.Error, "provider rejected request") {
		t.Fatalf("failed connection test job = %#v", job)
	}
}
