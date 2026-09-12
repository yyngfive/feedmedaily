package config

import (
	"path/filepath"
	"testing"
)

func writeProfileModelTestRoot(t *testing.T, env string) string {
	t.Helper()
	root := t.TempDir()
	writeConfigTestFile(t, filepath.Join(root, "go.mod"), "module example.com/test\n\ngo 1.25.0\n")
	if env != "" {
		writeConfigTestFile(t, filepath.Join(root, ".env"), env)
	}
	return root
}

func TestProfileModelsFreshInstallDefaultsToDeepSeek(t *testing.T) {
	settings, err := Load(writeProfileModelTestRoot(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProfileModels.DefaultModelID != ProfileModelDeepSeekV4Pro {
		t.Fatalf("default profile model = %q", settings.ProfileModels.DefaultModelID)
	}
	if settings.ProfileModel != ProfileModelDeepSeekV4Pro || settings.ProfileBaseURL != "https://api.deepseek.com" || settings.ProfileProvider != "deepseek" {
		t.Fatalf("flat profile settings not resolved from the catalog: %#v", settings)
	}
	response := ProfileModelsForSettings(settings)
	if len(response.Models) != 4 {
		t.Fatalf("catalog size = %d", len(response.Models))
	}
	for _, model := range response.Models {
		if model.Configured {
			t.Fatalf("fresh install must not report configured models: %#v", model)
		}
	}
}

func TestProfileModelsLegacyKeyStillFeedsDeepSeekEntry(t *testing.T) {
	settings, err := Load(writeProfileModelTestRoot(t, "SCIRSS_PROFILE_API_KEY=legacy-profile-key\n"))
	if err != nil {
		t.Fatal(err)
	}
	deepSeek := settings.ProfileModels.Models[ProfileModelDeepSeekV4Pro]
	if deepSeek.APIKey != "legacy-profile-key" {
		t.Fatalf("legacy profile key was not honored: %#v", deepSeek)
	}
	if settings.ProfileAPIKey != "legacy-profile-key" {
		t.Fatalf("flat profile key = %q", settings.ProfileAPIKey)
	}
}

func TestProfileModelsShareClassifierKeysAndDefaultSelection(t *testing.T) {
	env := "SCIRSS_DEEPSEEK_API_KEY=deepseek-key\nSCIRSS_GLM_API_KEY=glm-key\nQWEN_API_KEY=qwen-key\n" +
		"SCIRSS_PROFILE_DEFAULT_MODEL=glm-5.3\n"
	settings, err := Load(writeProfileModelTestRoot(t, env))
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProfileModel != ProfileModelGLM53 || settings.ProfileBaseURL != "https://open.bigmodel.cn/api/paas/v4" || settings.ProfileProvider != "zhipu" {
		t.Fatalf("glm-5.3 default was not applied: %#v", settings.EffectiveProfileModel())
	}
	if settings.ProfileAPIKey != "glm-key" {
		t.Fatalf("profile must share the GLM classifier key, got %q", settings.ProfileAPIKey)
	}
	if settings.ProfileModels.Models[ProfileModelQwen38Max0902].APIKey != "qwen-key" {
		t.Fatalf("qwen profile entry must share QWEN_API_KEY: %#v", settings.ProfileModels.Models[ProfileModelQwen38Max0902])
	}
	if settings.ProfileModels.Models[ProfileModelMiMoV25Pro].APIKey != "" {
		t.Fatal("mimo profile entry must stay unconfigured without MIMO_API_KEY")
	}
	response := ProfileModelsForSettings(settings)
	for _, model := range response.Models {
		if model.Default != (model.ID == ProfileModelGLM53) {
			t.Fatalf("unexpected default flag: %#v", model)
		}
	}
}

func TestProfileModelsUnknownLegacyProviderStaysUnmanaged(t *testing.T) {
	env := "SCIRSS_PROFILE_API_KEY=custom-key\nSCIRSS_PROFILE_BASE_URL=https://llm.example.com/v1\nSCIRSS_PROFILE_MODEL=custom-model\n"
	settings, err := Load(writeProfileModelTestRoot(t, env))
	if err != nil {
		t.Fatal(err)
	}
	if settings.ProfileModels.DefaultModelID != "" {
		t.Fatalf("unknown legacy provider must stay unmanaged, got %q", settings.ProfileModels.DefaultModelID)
	}
	if settings.ProfileModel != "custom-model" || settings.ProfileBaseURL != "https://llm.example.com/v1" || settings.ProfileAPIKey != "custom-key" || settings.ProfileProvider != "" {
		t.Fatalf("flat legacy profile settings were overridden: %#v", settings)
	}
	if effective := settings.EffectiveProfileModel(); effective.ID != "" {
		t.Fatalf("effective profile model must be empty for unmanaged setups: %#v", effective)
	}
}

func TestProfileModelForIDRequiresSharedKey(t *testing.T) {
	settings, err := Load(writeProfileModelTestRoot(t, "SCIRSS_DEEPSEEK_API_KEY=deepseek-key\nSCIRSS_PROFILE_DEFAULT_MODEL=mimo-v2.5-pro\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProfileModelForID(settings, "unknown-model"); err == nil {
		t.Fatal("unknown profile model must be rejected")
	}
	if _, err := ProfileModelForID(settings, ProfileModelMiMoV25Pro); err == nil {
		t.Fatal("unconfigured profile model must be rejected")
	}
	model, err := ProfileModelForID(settings, ProfileModelDeepSeekV4Pro)
	if err != nil {
		t.Fatal(err)
	}
	if model.APIKey != "deepseek-key" || model.Provider != "deepseek" {
		t.Fatalf("unexpected deepseek profile model: %#v", model)
	}
}
