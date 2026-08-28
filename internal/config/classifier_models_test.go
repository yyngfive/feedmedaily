package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifierModelsStructuredSourceConfigKeepsDisabledKeys(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")

	deepSeekKey := "deepseek-test-key"
	glmKey := "glm-test-key"
	response, err := UpdateLocalSettingsWithClassifierModels(root, nil, ClassifierModelsUpdate{
		EnabledModelIDs: []string{ClassifierModelDeepSeekV4Flash, ClassifierModelGLM53Flash},
		DefaultModelID:  ClassifierModelGLM53Flash,
		Credentials: map[string]SettingsConfigFieldUpdate{
			ClassifierModelDeepSeekV4Flash: {Value: &deepSeekKey},
			ClassifierModelGLM53Flash:      {Value: &glmKey},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSON(t, response)), deepSeekKey) || strings.Contains(string(mustJSON(t, response)), glmKey) {
		t.Fatal("classifier API keys must not be returned in the structured response")
	}
	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModel != ClassifierModelGLM53Flash || settings.ClassifierAPIKey != glmKey {
		t.Fatalf("effective classifier = %q/%q", settings.ClassifierModel, settings.ClassifierAPIKey)
	}
	if settings.ClassifierThinking != "enabled" || settings.ClassifierBaseURL != "https://open.bigmodel.cn/api/paas/v4" {
		t.Fatalf("GLM provider contract not resolved: %#v", settings.EffectiveClassifierModel())
	}
	envText, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envText), "SCIRSS_CLASSIFIER_API_KEY") || !strings.Contains(string(envText), "SCIRSS_GLM_API_KEY") {
		t.Fatalf("unexpected migrated .env: %s", envText)
	}

	updated, err := UpdateLocalSettingsWithClassifierModels(root, nil, ClassifierModelsUpdate{
		EnabledModelIDs: []string{ClassifierModelDeepSeekV4Flash},
		DefaultModelID:  ClassifierModelGLM53Flash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ClassifierModels.DefaultModelID != ClassifierModelDeepSeekV4Flash {
		t.Fatalf("single enabled model should become default, got %q", updated.ClassifierModels.DefaultModelID)
	}
	settings, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModels.Models[ClassifierModelGLM53Flash].APIKey != glmKey {
		t.Fatal("disabling a model must retain its local key")
	}

	clear := true
	_, err = UpdateLocalSettingsWithClassifierModels(root, nil, ClassifierModelsUpdate{
		EnabledModelIDs: []string{ClassifierModelDeepSeekV4Flash},
		DefaultModelID:  ClassifierModelDeepSeekV4Flash,
		Credentials: map[string]SettingsConfigFieldUpdate{
			ClassifierModelGLM53Flash: {Clear: clear},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModels.Models[ClassifierModelGLM53Flash].APIKey != "" {
		t.Fatal("explicit clear must remove the disabled model key")
	}
}

func TestClassifierModelsMigrateLegacyAndRespectEnvironmentOverride(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_API_KEY=legacy-key\nSCIRSS_CLASSIFIER_MODEL=glm-5.3-flash\nSCIRSS_CLASSIFIER_BASE_URL=https://open.bigmodel.cn/api/paas/v4\n")

	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModels.DefaultModelID != ClassifierModelGLM53Flash || settings.ClassifierModels.Models[ClassifierModelGLM53Flash].APIKey != "legacy-key" {
		t.Fatalf("legacy classifier config was not mapped: %#v", settings.ClassifierModels)
	}

	localKey := "local-deepseek"
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_DEEPSEEK_API_KEY="+localKey+"\n")
	t.Setenv(classifierDeepSeekAPIKey, "environment-deepseek")
	settings, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	model := settings.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash]
	if model.APIKey != "environment-deepseek" || model.APIKeySource != "environment" || !model.StoredLocally {
		t.Fatalf("environment priority was not reported: %#v", model)
	}
	view := classifierModelsResponse(settings).Models[0]
	if !view.EnvironmentOverride {
		t.Fatal("structured model response must expose environment override")
	}
}

func TestClassifierModelsHonorCrossGenerationPriority(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_DEEPSEEK_API_KEY=local-new\nSCIRSS_CLASSIFIER_API_KEY=local-legacy\nSCIRSS_CLASSIFIER_MODEL=deepseek-v4-flash\n")
	t.Setenv("SCIRSS_CLASSIFIER_API_KEY", "environment-legacy")
	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash].APIKey; got != "environment-legacy" {
		t.Fatalf("legacy environment key must outrank new local key, got %q", got)
	}
	if source := settings.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash].APIKeySource; source != "environment" {
		t.Fatalf("legacy environment source = %q", source)
	}

	t.Setenv(classifierDeepSeekAPIKey, "environment-new")
	settings, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := settings.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash].APIKey; got != "environment-new" {
		t.Fatalf("same-level new environment key must win, got %q", got)
	}
}

func TestClassifierModelsStructuredReleaseStoresSecretOutsideSettings(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "user-data")
	writeConfigTestFile(t, filepath.Join(root, "app.exe"), "")
	t.Setenv("FEEDMEDAILY_RUNTIME_MODE", "release")
	t.Setenv("FEEDMEDAILY_DATA_ROOT", dataRoot)

	key := "release-deepseek-key"
	if _, err := UpdateLocalSettingsWithClassifierModels(root, nil, ClassifierModelsUpdate{
		EnabledModelIDs: []string{ClassifierModelDeepSeekV4Flash},
		DefaultModelID:  ClassifierModelDeepSeekV4Flash,
		Credentials: map[string]SettingsConfigFieldUpdate{
			ClassifierModelDeepSeekV4Flash: {Value: &key},
		},
	}); err != nil {
		t.Fatal(err)
	}
	settingsJSON, err := os.ReadFile(filepath.Join(dataRoot, "config", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	secretsJSON, err := os.ReadFile(filepath.Join(dataRoot, "config", "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settingsJSON), key) || strings.Contains(string(secretsJSON), key) {
		t.Fatal("release classifier key was written in plaintext")
	}
	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash].APIKey != key {
		t.Fatalf("release key did not round-trip: %#v", settings.ClassifierModels)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
