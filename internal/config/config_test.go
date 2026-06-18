package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

func TestLoadSourceSettingsFromDotEnv(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_SERVER_HOST=127.0.0.2\nSCIRSS_SERVER_PORT=8123\n")

	t.Setenv("FEEDMEDAILY_RUNTIME_MODE", "")

	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != appruntime.ModeSource {
		t.Fatalf("mode = %q", settings.Mode)
	}
	if settings.ServerHost != "127.0.0.2" || settings.ServerPort != 8123 {
		t.Fatalf("server = %s:%d", settings.ServerHost, settings.ServerPort)
	}
	if settings.DataDir != filepath.Join(root, "data") {
		t.Fatalf("data dir = %q", settings.DataDir)
	}
	if settings.FeedsPath != filepath.Join(root, "data", "rss_feeds.json") {
		t.Fatalf("feeds path = %q", settings.FeedsPath)
	}
	if settings.UpdateManifestURL != DefaultUpdateManifestURL {
		t.Fatalf("update manifest = %q", settings.UpdateManifestURL)
	}
}

func TestLoadEnvironmentOverridesDotEnv(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_SERVER_PORT=8123\n")

	t.Setenv("SCIRSS_SERVER_PORT", "9001")

	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.ServerPort != 9001 {
		t.Fatalf("server port = %d", settings.ServerPort)
	}
}

func TestSettingsConfigMasksSecretsAndWritesDotEnv(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_CLASSIFIER_API_KEY=super-secret\nSCIRSS_CLASSIFIER_MODEL=deepseek-v4-flash\n")

	response, err := SettingsConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	apiKey := fieldByKey(t, response.Fields, "SCIRSS_CLASSIFIER_API_KEY")
	if !apiKey.Secret || !apiKey.Configured || apiKey.Source != "dotenv" || apiKey.Value != nil {
		t.Fatalf("unexpected api key field: %#v", apiKey)
	}

	replacement := "replacement-secret"
	model := "deepseek-v4-pro"
	batchSize := "12"
	updated, err := UpdateLocalSettings(root, map[string]SettingsConfigFieldUpdate{
		"SCIRSS_CLASSIFIER_API_KEY":    {Value: &replacement},
		"SCIRSS_CLASSIFIER_MODEL":      {Value: &model},
		"SCIRSS_CLASSIFIER_BATCH_SIZE": {Value: &batchSize},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fieldByKey(t, updated.Fields, "SCIRSS_CLASSIFIER_API_KEY").Value != nil {
		t.Fatal("secret value leaked through settings response")
	}
	envText, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SCIRSS_CLASSIFIER_API_KEY='replacement-secret'",
		"SCIRSS_CLASSIFIER_MODEL='deepseek-v4-pro'",
		"SCIRSS_CLASSIFIER_BATCH_SIZE='12'",
	} {
		if !strings.Contains(string(envText), want) {
			t.Fatalf(".env did not contain %q:\n%s", want, envText)
		}
	}
}

func TestSettingsConfigShowsEnvironmentOverrideSource(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	writeConfigTestFile(t, filepath.Join(root, ".env"), "SCIRSS_PROFILE_MODEL=local-profile-model\n")
	t.Setenv("SCIRSS_PROFILE_MODEL", "system-profile-model")

	response, err := SettingsConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	field := fieldByKey(t, response.Fields, "SCIRSS_PROFILE_MODEL")
	if field.Value == nil || *field.Value != "system-profile-model" || field.Source != "environment" || !field.StoredInDotenv {
		t.Fatalf("unexpected profile model field: %#v", field)
	}
}

func TestSettingsConfigShowsGitHubUpdateManifestDefault(t *testing.T) {
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")

	response, err := SettingsConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	field := fieldByKey(t, response.Fields, "FEEDMEDAILY_UPDATE_MANIFEST_URL")
	if field.Value == nil || *field.Value != DefaultUpdateManifestURL {
		t.Fatalf("unexpected update manifest field: %#v", field)
	}
	if field.DefaultValue == nil || *field.DefaultValue != DefaultUpdateManifestURL || field.Source != "default" {
		t.Fatalf("unexpected update manifest default metadata: %#v", field)
	}
}

func TestUpdateLocalSettingsReleaseUsesSettingsAndSecretStores(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "user-data")
	writeConfigTestFile(t, filepath.Join(root, "app.exe"), "")
	t.Setenv("FEEDMEDAILY_RUNTIME_MODE", "release")
	t.Setenv("FEEDMEDAILY_DATA_ROOT", dataRoot)

	secret := "release-secret"
	model := "release-profile-model"
	libraryID := "123456"
	_, err := UpdateLocalSettings(root, map[string]SettingsConfigFieldUpdate{
		"SCIRSS_CLASSIFIER_API_KEY": {Value: &secret},
		"SCIRSS_PROFILE_MODEL":      {Value: &model},
		"SCIRSS_ZOTERO_LIBRARY_ID":  {Value: &libraryID},
	})
	if err != nil {
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
	if !strings.Contains(string(settingsJSON), "release-profile-model") || !strings.Contains(string(settingsJSON), "123456") {
		t.Fatalf("settings store missing ordinary values:\n%s", settingsJSON)
	}
	if strings.Contains(string(settingsJSON), "release-secret") || strings.Contains(string(secretsJSON), "release-secret") {
		t.Fatal("release secret was stored in plaintext")
	}
	settings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Mode != "release" || settings.UserDataDir != dataRoot {
		t.Fatalf("unexpected release settings: %#v", settings)
	}
}

func fieldByKey(t *testing.T, fields []SettingsConfigField, key string) SettingsConfigField {
	t.Helper()
	for _, field := range fields {
		if field.Key == key {
			return field
		}
	}
	t.Fatalf("field %q not found", key)
	return SettingsConfigField{}
}

func writeConfigTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
