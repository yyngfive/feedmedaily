package config

import (
	"fmt"
	"strings"
)

const (
	// ProfileModelDeepSeekV4Pro is DeepSeek's Pro call name and the long-standing
	// profile default. Its billing stays on the Pro rate card (see internal/llmusage).
	ProfileModelDeepSeekV4Pro = "deepseek-v4-pro"
	ProfileModelGLM53         = "glm-5.3"
	ProfileModelQwen38Max0902 = "qwen3.8-max-0902"
	ProfileModelMiMoV25Pro    = "mimo-v2.5-pro"

	profileDefaultModelKey = "SCIRSS_PROFILE_DEFAULT_MODEL"
	profileLegacyAPIKey    = "SCIRSS_PROFILE_API_KEY"
	profileLegacyBaseURL   = "SCIRSS_PROFILE_BASE_URL"
	profileLegacyModelKey  = "SCIRSS_PROFILE_MODEL"
)

// ProfileModelSpec is the fixed provider contract for one profile model. The
// profile role shares the classifier's per-provider API keys, so it defines no
// credential keys of its own.
type ProfileModelSpec struct {
	ID       string
	Provider string
	Label    string
	BaseURL  string
}

// ProfileModelConfig is a resolved profile model entry. APIKey is kept internal to the backend.
type ProfileModelConfig struct {
	ProfileModelSpec
	APIKey        string
	APIKeySource  string
	StoredLocally bool
}

// ProfileModels is the resolved profile registry and its selected default.
type ProfileModels struct {
	DefaultModelID string                        `json:"default_model_id"`
	Models         map[string]ProfileModelConfig `json:"-"`
}

// ProfileModelView is the secret-free representation returned to the web client.
type ProfileModelView struct {
	ID                  string `json:"id"`
	Provider            string `json:"provider"`
	Label               string `json:"label"`
	BaseURL             string `json:"base_url"`
	Default             bool   `json:"default"`
	Configured          bool   `json:"configured"`
	Source              string `json:"source"`
	StoredLocally       bool   `json:"stored_locally"`
	EnvironmentOverride bool   `json:"environment_override"`
}

// ProfileModelsResponse keeps the catalog small and never returns API key material.
type ProfileModelsResponse struct {
	Models         []ProfileModelView `json:"models"`
	DefaultModelID string             `json:"default_model_id"`
}

var profileModelCatalog = []ProfileModelSpec{
	{
		ID:       ProfileModelDeepSeekV4Pro,
		Provider: "deepseek",
		Label:    "DeepSeek V4 Pro",
		BaseURL:  "https://api.deepseek.com",
	},
	{
		ID:       ProfileModelGLM53,
		Provider: "zhipu",
		Label:    "GLM-5.3",
		BaseURL:  "https://open.bigmodel.cn/api/paas/v4",
	},
	{
		ID:       ProfileModelQwen38Max0902,
		Provider: "qwen",
		Label:    "Qwen3.8-Max-0902",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
	},
	{
		ID:       ProfileModelMiMoV25Pro,
		Provider: "mimo",
		Label:    "MiMo-V2.5-Pro",
		BaseURL:  "https://api.xiaomimimo.com/v1",
	},
}

func profileModelSpec(modelID string) (ProfileModelSpec, bool) {
	id := strings.TrimSpace(modelID)
	for _, spec := range profileModelCatalog {
		if spec.ID == id {
			return spec, true
		}
	}
	return ProfileModelSpec{}, false
}

// profileCredentialKey maps a profile provider onto the classifier credential
// slot it shares, so both roles manage exactly one key per provider.
func profileCredentialKey(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek":
		return classifierDeepSeekAPIKey
	case "zhipu":
		return classifierGLMAPIKey
	case "qwen":
		return classifierQwenAPIKey
	case "mimo":
		return classifierMiMoAPIKey
	default:
		return ""
	}
}

// EffectiveProfileModel resolves the selected profile model. An empty result
// means the installation still runs on an unmanaged legacy profile provider,
// and the flat Profile* settings stay authoritative.
func (s Settings) EffectiveProfileModel() ProfileModelConfig {
	if s.ProfileModels.DefaultModelID == "" {
		return ProfileModelConfig{}
	}
	if model, ok := s.ProfileModels.Models[s.ProfileModels.DefaultModelID]; ok {
		return model
	}
	if spec, ok := profileModelSpec(s.ProfileModels.DefaultModelID); ok {
		return ProfileModelConfig{ProfileModelSpec: spec, APIKey: s.ProfileAPIKey}
	}
	return ProfileModelConfig{}
}

// EffectiveProfileModelName is used by usage summaries without exposing registry internals.
func (s Settings) EffectiveProfileModelName() string {
	if model := s.EffectiveProfileModel(); strings.TrimSpace(model.ID) != "" {
		return model.ID
	}
	return s.ProfileModel
}

func profileModelsFromResolvedValues(values []ResolvedValue) ProfileModels {
	byKey := make(map[string]ResolvedValue, len(values))
	for _, value := range values {
		byKey[value.Option.Key] = value
	}

	newDefault := byKey[profileDefaultModelKey]
	legacyModel := strings.TrimSpace(byKey[profileLegacyModelKey].Value)
	legacyBaseURL := strings.TrimSpace(byKey[profileLegacyBaseURL].Value)
	legacyKey := byKey[profileLegacyAPIKey]
	legacyConfigured := isNonDefaultResolvedValue(legacyKey) || isNonDefaultResolvedValue(byKey[profileLegacyModelKey]) || isNonDefaultResolvedValue(byKey[profileLegacyBaseURL])
	legacyID := profileModelIDFromLegacy(legacyModel, legacyBaseURL)
	legacyPriority := strongestResolvedValuePriority(legacyKey, byKey[profileLegacyModelKey], byKey[profileLegacyBaseURL])

	models := make(map[string]ProfileModelConfig, len(profileModelCatalog))
	for _, spec := range profileModelCatalog {
		key := ResolvedValue{}
		if storageKey := profileCredentialKey(spec.Provider); storageKey != "" {
			key = byKey[storageKey]
		}
		if spec.Provider == "deepseek" {
			// The legacy profile key was DeepSeek-only; keep honoring whichever of
			// the provider key and the legacy key resolves at the stronger level.
			key = preferredClassifierCredential(key, legacyKey)
		}
		models[spec.ID] = ProfileModelConfig{
			ProfileModelSpec: spec,
			APIKey:           strings.TrimSpace(key.Value),
			APIKeySource:     key.Source,
			StoredLocally:    key.StoredLocally,
		}
	}

	defaultID := strings.TrimSpace(newDefault.Value)
	newSelectionWins := isNonDefaultResolvedValue(newDefault) && resolvedValuePriority(newDefault.Source) >= legacyPriority
	_, newDefaultKnown := profileModelSpec(defaultID)
	switch {
	case newSelectionWins && newDefaultKnown:
		// Keep the managed selection.
	case legacyConfigured && legacyID != "":
		defaultID = legacyID
	case legacyConfigured:
		// Unknown legacy provider config cannot be represented by the managed
		// catalog; an empty default keeps the flat legacy settings authoritative.
		defaultID = ""
	default:
		defaultID = ProfileModelDeepSeekV4Pro
	}
	if defaultID != "" {
		if _, ok := profileModelSpec(defaultID); !ok {
			defaultID = ProfileModelDeepSeekV4Pro
		}
	}
	return ProfileModels{DefaultModelID: defaultID, Models: models}
}

// profileModelIDFromLegacy maps the flat legacy profile settings onto a managed
// catalog entry. The legacy profile role was DeepSeek-only, but users may have
// pointed the legacy keys at any OpenAI-compatible provider; unknown shapes
// return "" so they keep running untouched.
func profileModelIDFromLegacy(model string, baseURL string) string {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	normalizedBaseURL := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case normalizedModel == ProfileModelDeepSeekV4Pro || strings.Contains(normalizedModel, "deepseek") || strings.Contains(normalizedBaseURL, "api.deepseek.com"):
		return ProfileModelDeepSeekV4Pro
	case normalizedModel == ProfileModelGLM53 || strings.Contains(normalizedModel, "glm-5.3") || strings.Contains(normalizedBaseURL, "bigmodel.cn"):
		return ProfileModelGLM53
	case normalizedModel == ProfileModelQwen38Max0902 || strings.Contains(normalizedModel, "qwen3.8-max") || strings.Contains(normalizedBaseURL, "dashscope.aliyuncs.com"):
		return ProfileModelQwen38Max0902
	case normalizedModel == ProfileModelMiMoV25Pro || strings.Contains(normalizedModel, "mimo-v2.5") || strings.Contains(normalizedBaseURL, "xiaomimimo.com"):
		return ProfileModelMiMoV25Pro
	default:
		return ""
	}
}

func profileModelsResponse(settings Settings) ProfileModelsResponse {
	models := make([]ProfileModelView, 0, len(profileModelCatalog))
	for _, spec := range profileModelCatalog {
		resolved := settings.ProfileModels.Models[spec.ID]
		if resolved.ID == "" {
			resolved = ProfileModelConfig{ProfileModelSpec: spec}
		}
		models = append(models, ProfileModelView{
			ID:                  spec.ID,
			Provider:            spec.Provider,
			Label:               spec.Label,
			BaseURL:             spec.BaseURL,
			Default:             settings.ProfileModels.DefaultModelID == spec.ID,
			Configured:          strings.TrimSpace(resolved.APIKey) != "",
			Source:              firstNonEmptyClassifierSource(resolved.APIKeySource),
			StoredLocally:       resolved.StoredLocally,
			EnvironmentOverride: resolved.APIKeySource == "environment",
		})
	}
	return ProfileModelsResponse{
		Models:         models,
		DefaultModelID: settings.ProfileModels.DefaultModelID,
	}
}

// ProfileModelsForSettings is intentionally small for API handlers and tests.
func ProfileModelsForSettings(settings Settings) ProfileModelsResponse {
	return profileModelsResponse(settings)
}

// ProfileModelForID resolves a fixed catalog entry for connection tests.
func ProfileModelForID(settings Settings, modelID string) (ProfileModelConfig, error) {
	spec, ok := profileModelSpec(strings.TrimSpace(modelID))
	if !ok {
		return ProfileModelConfig{}, fmt.Errorf("unsupported profile model: %s", strings.TrimSpace(modelID))
	}
	model := settings.ProfileModels.Models[spec.ID]
	if model.ID == "" {
		model = ProfileModelConfig{ProfileModelSpec: spec}
	}
	if strings.TrimSpace(model.APIKey) == "" && settings.EffectiveProfileModelName() == spec.ID {
		model.APIKey = settings.ProfileAPIKey
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return ProfileModelConfig{}, fmt.Errorf("API key is required for profile model %s; configure the shared %s classifier key first", spec.ID, profileProviderLabel(spec.Provider))
	}
	return model, nil
}

func profileProviderLabel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "deepseek":
		return "DeepSeek"
	case "zhipu":
		return "GLM"
	case "qwen":
		return "Qwen"
	case "mimo":
		return "MiMo"
	default:
		return strings.TrimSpace(provider)
	}
}
