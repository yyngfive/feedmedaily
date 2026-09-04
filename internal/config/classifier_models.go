package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const (
	ClassifierModelDeepSeekV4Flash = "deepseek-v4-flash"
	ClassifierModelGLM53Flash      = "glm-5.3-flash"
	ClassifierModelQwen38Flash     = "qwen3.8-flash"
	ClassifierModelMiMoV25         = "mimo-v2.5"
	ClassifierModelMiMoZenFree     = "mimo-v2.5-free"

	classifierEnabledModelsKey = "SCIRSS_CLASSIFIER_ENABLED_MODELS"
	classifierDefaultModelKey  = "SCIRSS_CLASSIFIER_DEFAULT_MODEL"
	classifierDeepSeekAPIKey   = "SCIRSS_DEEPSEEK_API_KEY"
	classifierGLMAPIKey        = "SCIRSS_GLM_API_KEY"
	classifierQwenAPIKey       = "QWEN_API_KEY"
	classifierMiMoAPIKey       = "MIMO_API_KEY"
	classifierZenAPIKey        = "SCIRSS_ZEN_API_KEY"
)

// OpenCodeZenPublicAPIKey is the anonymous token the OpenCode Zen gateway accepts
// for its free tier; catalog entries marked KeyOptional fall back to it.
const OpenCodeZenPublicAPIKey = "public"

// ClassifierModelSpec is the fixed provider contract used by every classifier request.
type ClassifierModelSpec struct {
	ID              string
	Provider        string
	Label           string
	BaseURL         string
	Thinking        string
	ReasoningEffort string
	KeyOptional     bool
}

// ClassifierModelConfig is a resolved model entry. APIKey is kept internal to the backend.
type ClassifierModelConfig struct {
	ClassifierModelSpec
	APIKey        string
	APIKeySource  string
	StoredLocally bool
}

// ClassifierModels is the resolved classifier registry and its selected default.
type ClassifierModels struct {
	EnabledModelIDs []string                         `json:"enabled_model_ids"`
	DefaultModelID  string                           `json:"default_model_id"`
	Models          map[string]ClassifierModelConfig `json:"-"`
}

// ClassifierModelView is the secret-free representation returned to the web client.
type ClassifierModelView struct {
	ID                  string `json:"id"`
	Provider            string `json:"provider"`
	Label               string `json:"label"`
	BaseURL             string `json:"base_url"`
	Thinking            string `json:"thinking"`
	ReasoningEffort     string `json:"reasoning_effort,omitempty"`
	Enabled             bool   `json:"enabled"`
	Default             bool   `json:"default"`
	Configured          bool   `json:"configured"`
	KeyOptional         bool   `json:"key_optional"`
	Source              string `json:"source"`
	StoredLocally       bool   `json:"stored_locally"`
	EnvironmentOverride bool   `json:"environment_override"`
}

// ClassifierModelsResponse keeps the catalog small and never returns API key material.
type ClassifierModelsResponse struct {
	Models          []ClassifierModelView `json:"models"`
	EnabledModelIDs []string              `json:"enabled_model_ids"`
	DefaultModelID  string                `json:"default_model_id"`
}

// ClassifierModelsUpdate changes selection and optionally updates provider secrets.
type ClassifierModelsUpdate struct {
	EnabledModelIDs            []string                             `json:"enabled_model_ids"`
	DefaultModelID             string                               `json:"default_model_id"`
	Credentials                map[string]SettingsConfigFieldUpdate `json:"credentials"`
	ReuseDeepSeekKeyForProfile bool                                 `json:"reuse_deepseek_key_for_profile"`
}

var classifierModelCatalog = []ClassifierModelSpec{
	{
		ID:              ClassifierModelDeepSeekV4Flash,
		Provider:        "deepseek",
		Label:           "DeepSeek V4 Flash",
		BaseURL:         "https://api.deepseek.com",
		Thinking:        "disabled",
		ReasoningEffort: "",
	},
	{
		ID:              ClassifierModelGLM53Flash,
		Provider:        "zhipu",
		Label:           "GLM-5.3-Flash",
		BaseURL:         "https://open.bigmodel.cn/api/paas/v4",
		Thinking:        "enabled",
		ReasoningEffort: "low",
	},
	{
		ID:              ClassifierModelQwen38Flash,
		Provider:        "qwen",
		Label:           "Qwen3.8-Flash",
		BaseURL:         "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Thinking:        "disabled",
		ReasoningEffort: "none",
	},
	{
		ID:              ClassifierModelMiMoV25,
		Provider:        "mimo",
		Label:           "MiMo-V2.5",
		BaseURL:         "https://api.xiaomimimo.com/v1",
		Thinking:        "disabled",
		ReasoningEffort: "",
	},
	{
		ID:          ClassifierModelMiMoZenFree,
		Provider:    "opencode",
		Label:       "OpenCode MiMo V2.5 (Free)",
		BaseURL:     "https://opencode.ai/zen/v1",
		Thinking:    "disabled",
		KeyOptional: true,
	},
}

func classifierModelSpec(modelID string) (ClassifierModelSpec, bool) {
	for _, spec := range classifierModelCatalog {
		if spec.ID == modelID {
			return spec, true
		}
	}
	return ClassifierModelSpec{}, false
}

// ClassifierModelCatalog returns a copy so callers cannot mutate the provider contract.
func ClassifierModelCatalog() []ClassifierModelSpec {
	return append([]ClassifierModelSpec(nil), classifierModelCatalog...)
}

// EffectiveClassifierModel resolves the selected model while preserving old test/config callers.
func (s Settings) EffectiveClassifierModel() ClassifierModelConfig {
	if model, ok := s.ClassifierModels.Models[s.ClassifierModels.DefaultModelID]; ok {
		return applyClassifierKeyFallback(classifierModelWithThinkingPreference(model, s.ClassifierThinking))
	}
	if spec, ok := classifierModelSpec(s.ClassifierModels.DefaultModelID); ok {
		return applyClassifierKeyFallback(classifierModelWithThinkingPreference(ClassifierModelConfig{ClassifierModelSpec: spec, APIKey: s.ClassifierAPIKey}, s.ClassifierThinking))
	}
	if model, ok := s.ClassifierModels.Models[s.ClassifierModel]; ok {
		return applyClassifierKeyFallback(classifierModelWithThinkingPreference(model, s.ClassifierThinking))
	}
	if spec, ok := classifierModelSpec(s.ClassifierModel); ok {
		return applyClassifierKeyFallback(classifierModelWithThinkingPreference(ClassifierModelConfig{ClassifierModelSpec: spec, APIKey: s.ClassifierAPIKey}, s.ClassifierThinking))
	}
	return ClassifierModelConfig{
		ClassifierModelSpec: ClassifierModelSpec{
			ID:       s.ClassifierModel,
			BaseURL:  s.ClassifierBaseURL,
			Thinking: s.ClassifierThinking,
		},
		APIKey: s.ClassifierAPIKey,
	}
}

func applyClassifierKeyFallback(model ClassifierModelConfig) ClassifierModelConfig {
	if model.KeyOptional && strings.TrimSpace(model.APIKey) == "" {
		model.APIKey = OpenCodeZenPublicAPIKey
	}
	return model
}

func classifierModelWithThinkingPreference(model ClassifierModelConfig, preference string) ClassifierModelConfig {
	enabled := strings.EqualFold(strings.TrimSpace(preference), "enabled")
	switch model.Provider {
	case "zhipu":
		model.Thinking = "enabled"
		model.ReasoningEffort = "low"
	case "deepseek":
		if enabled {
			model.Thinking = "enabled"
			model.ReasoningEffort = "low"
		} else {
			model.Thinking = "disabled"
			model.ReasoningEffort = ""
		}
	case "qwen":
		if enabled {
			model.Thinking = "enabled"
			model.ReasoningEffort = "low"
		} else {
			model.Thinking = "disabled"
			model.ReasoningEffort = "none"
		}
	case "mimo":
		if enabled {
			model.Thinking = "enabled"
		} else {
			model.Thinking = "disabled"
		}
		model.ReasoningEffort = ""
	}
	return model
}

// EffectiveClassifierModelName is used by usage summaries without exposing registry internals.
func (s Settings) EffectiveClassifierModelName() string {
	model := s.EffectiveClassifierModel()
	if strings.TrimSpace(model.ID) != "" {
		return model.ID
	}
	return s.ClassifierModel
}

func classifierModelsResponse(settings Settings) ClassifierModelsResponse {
	models := make([]ClassifierModelView, 0, len(classifierModelCatalog))
	enabled := make(map[string]struct{}, len(settings.ClassifierModels.EnabledModelIDs))
	for _, id := range settings.ClassifierModels.EnabledModelIDs {
		enabled[id] = struct{}{}
	}
	for _, spec := range classifierModelCatalog {
		resolved := settings.ClassifierModels.Models[spec.ID]
		if resolved.ID == "" {
			resolved.ClassifierModelSpec = spec
		}
		_, isEnabled := enabled[spec.ID]
		source := firstNonEmptyClassifierSource(resolved.APIKeySource)
		if spec.KeyOptional && strings.TrimSpace(resolved.APIKey) == "" {
			source = "builtin"
		}
		models = append(models, ClassifierModelView{
			ID:                  spec.ID,
			Provider:            spec.Provider,
			Label:               spec.Label,
			BaseURL:             spec.BaseURL,
			Thinking:            spec.Thinking,
			ReasoningEffort:     spec.ReasoningEffort,
			Enabled:             isEnabled,
			Default:             settings.ClassifierModels.DefaultModelID == spec.ID,
			Configured:          spec.KeyOptional || strings.TrimSpace(resolved.APIKey) != "",
			KeyOptional:         spec.KeyOptional,
			Source:              source,
			StoredLocally:       resolved.StoredLocally,
			EnvironmentOverride: resolved.APIKeySource == "environment",
		})
	}
	return ClassifierModelsResponse{
		Models:          models,
		EnabledModelIDs: append([]string(nil), settings.ClassifierModels.EnabledModelIDs...),
		DefaultModelID:  settings.ClassifierModels.DefaultModelID,
	}
}

func classifierModelsFromResolvedValues(values []ResolvedValue) ClassifierModels {
	byKey := make(map[string]ResolvedValue, len(values))
	for _, value := range values {
		byKey[value.Option.Key] = value
	}

	legacyModel := valueOrDefault(strings.TrimSpace(byKey["SCIRSS_CLASSIFIER_MODEL"].Value), ClassifierModelDeepSeekV4Flash)
	legacyBaseURL := valueOrDefault(strings.TrimSpace(byKey["SCIRSS_CLASSIFIER_BASE_URL"].Value), "https://api.deepseek.com")
	legacyKey := byKey["SCIRSS_CLASSIFIER_API_KEY"]
	legacyConfigured := isNonDefaultResolvedValue(legacyKey) || isNonDefaultResolvedValue(byKey["SCIRSS_CLASSIFIER_MODEL"]) || isNonDefaultResolvedValue(byKey["SCIRSS_CLASSIFIER_BASE_URL"])
	legacyID := classifierModelIDFromLegacy(legacyModel, legacyBaseURL)
	legacySelectionPriority := strongestResolvedValuePriority(
		legacyKey,
		byKey["SCIRSS_CLASSIFIER_MODEL"],
		byKey["SCIRSS_CLASSIFIER_BASE_URL"],
	)

	newEnabled := byKey[classifierEnabledModelsKey]
	newDefault := byKey[classifierDefaultModelKey]
	newCredentials := make(map[string]ResolvedValue, len(classifierModelCatalog))
	selectionValues := []ResolvedValue{newEnabled, newDefault}
	for _, spec := range classifierModelCatalog {
		key, _ := classifierCredentialKey(spec.ID)
		credential := byKey[key]
		newCredentials[spec.ID] = credential
		selectionValues = append(selectionValues, credential)
	}
	newConfigured := false
	for _, value := range selectionValues {
		if isNonDefaultResolvedValue(value) {
			newConfigured = true
			break
		}
	}
	newSelectionPriority := strongestResolvedValuePriority(selectionValues...)

	models := make(map[string]ClassifierModelConfig, len(classifierModelCatalog))
	for _, spec := range classifierModelCatalog {
		key := newCredentials[spec.ID]
		if legacyConfigured && legacyID == spec.ID {
			// Resolve aliases with the same environment > local > default order as
			// ordinary settings. A same-level new provider key wins over the old
			// flat key, while an old system-environment key still overrides a new
			// local value for backwards-compatible deployments.
			key = preferredClassifierCredential(key, legacyKey)
		}
		models[spec.ID] = ClassifierModelConfig{
			ClassifierModelSpec: spec,
			APIKey:              strings.TrimSpace(key.Value),
			APIKeySource:        key.Source,
			StoredLocally:       key.StoredLocally,
		}
	}
	enabled := []string{}
	configuredDefault := ""
	// A new-generation selection value wins whenever it is at least as strong as
	// the legacy selection. This also makes a new default or provider key useful
	// before the first structured save has materialized enabled_model_ids.
	newSelectionWins := newConfigured && newSelectionPriority >= legacySelectionPriority
	if newSelectionWins {
		if isNonDefaultResolvedValue(newEnabled) && strings.TrimSpace(newEnabled.Value) != "" {
			enabled = parseClassifierModelIDs(newEnabled.Value)
		}
		if len(enabled) == 0 && isNonDefaultResolvedValue(newDefault) {
			if _, ok := classifierModelSpec(strings.TrimSpace(newDefault.Value)); ok {
				enabled = []string{strings.TrimSpace(newDefault.Value)}
			}
		}
		if len(enabled) == 0 {
			for _, spec := range classifierModelCatalog {
				if strings.TrimSpace(models[spec.ID].APIKey) != "" {
					enabled = append(enabled, spec.ID)
				}
			}
		}
		configuredDefault = strings.TrimSpace(newDefault.Value)
	} else if legacyConfigured {
		enabled = []string{legacyID}
		configuredDefault = legacyID
	} else {
		for _, spec := range classifierModelCatalog {
			if strings.TrimSpace(models[spec.ID].APIKey) != "" {
				enabled = append(enabled, spec.ID)
			}
		}
	}
	enabled, defaultID := normalizeResolvedClassifierModels(enabled, configuredDefault, legacyID, legacyConfigured && !newSelectionWins)
	return ClassifierModels{EnabledModelIDs: enabled, DefaultModelID: defaultID, Models: models}
}

func isNonDefaultResolvedValue(value ResolvedValue) bool {
	return value.Source != "" && value.Source != "default" && value.Source != "unset"
}

func preferredClassifierCredential(newValue ResolvedValue, legacyValue ResolvedValue) ResolvedValue {
	newPriority := resolvedValuePriority(newValue.Source)
	legacyPriority := resolvedValuePriority(legacyValue.Source)
	if newPriority >= legacyPriority {
		return newValue
	}
	return legacyValue
}

func resolvedValuePriority(source string) int {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "environment":
		return 3
	case "dotenv", "settings", "secret_store":
		return 2
	case "default":
		return 1
	default:
		return 0
	}
}

func strongestResolvedValuePriority(values ...ResolvedValue) int {
	strongest := 0
	for _, value := range values {
		if priority := resolvedValuePriority(value.Source); priority > strongest {
			strongest = priority
		}
	}
	return strongest
}

func parseClassifierModelIDs(raw string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(classifierModelCatalog))
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		id := strings.TrimSpace(part)
		if _, ok := classifierModelSpec(id); !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.SliceStable(result, func(i, j int) bool { return classifierCatalogIndex(result[i]) < classifierCatalogIndex(result[j]) })
	return result
}

func normalizeResolvedClassifierModels(enabled []string, configuredDefault string, legacyID string, legacyConfigured bool) ([]string, string) {
	validated := make([]string, 0, len(enabled))
	seen := map[string]struct{}{}
	for _, id := range enabled {
		if _, ok := classifierModelSpec(id); !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		validated = append(validated, id)
	}
	if len(validated) == 0 {
		if legacyConfigured && legacyID != "" {
			validated = []string{legacyID}
		} else {
			// Fresh setups get the keyless OpenCode Zen entry available next to
			// DeepSeek while keeping DeepSeek as the initial default classifier.
			validated = []string{ClassifierModelDeepSeekV4Flash, ClassifierModelMiMoZenFree}
		}
	}
	defaultID := strings.TrimSpace(configuredDefault)
	if _, ok := seen[defaultID]; !ok {
		defaultID = ""
	}
	if defaultID == "" {
		defaultID = validated[0]
	}
	sort.SliceStable(validated, func(i, j int) bool {
		return classifierCatalogIndex(validated[i]) < classifierCatalogIndex(validated[j])
	})
	return validated, defaultID
}

func classifierModelIDFromLegacy(model string, baseURL string) string {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	if normalizedModel == ClassifierModelDeepSeekV4Flash || normalizedModel == "deepseek-chat" || normalizedModel == "deepseek-reasoner" || strings.Contains(normalizedModel, "deepseek") {
		return ClassifierModelDeepSeekV4Flash
	}
	if normalizedModel == ClassifierModelGLM53Flash || strings.Contains(normalizedModel, "glm-5.3-flash") || strings.Contains(strings.ToLower(baseURL), "bigmodel.cn") {
		return ClassifierModelGLM53Flash
	}
	if normalizedModel == ClassifierModelQwen38Flash || strings.Contains(normalizedModel, "qwen3.8-flash") || strings.Contains(strings.ToLower(baseURL), "dashscope.aliyuncs.com") {
		return ClassifierModelQwen38Flash
	}
	if normalizedModel == ClassifierModelMiMoZenFree || strings.Contains(normalizedModel, "mimo-v2.5-free") || strings.Contains(strings.ToLower(baseURL), "opencode.ai") {
		return ClassifierModelMiMoZenFree
	}
	if normalizedModel == ClassifierModelMiMoV25 || strings.Contains(normalizedModel, "mimo-v2.5") || strings.Contains(strings.ToLower(baseURL), "xiaomimimo.com") {
		return ClassifierModelMiMoV25
	}
	// Unknown legacy OpenAI-compatible classifier models cannot be represented by
	// the managed catalog; keep the migration deterministic by selecting DeepSeek.
	return ClassifierModelDeepSeekV4Flash
}

// UpdateLocalSettingsWithClassifierModels applies the structured classifier update and
// optionally copies a resolved DeepSeek key into the legacy Profile key for first-run onboarding.
func UpdateLocalSettingsWithClassifierModels(root string, fields map[string]SettingsConfigFieldUpdate, update ClassifierModelsUpdate) (SettingsConfigResponse, error) {
	current, err := Load(root)
	if err != nil {
		return SettingsConfigResponse{}, err
	}
	enabled, defaultID, err := validateClassifierModelsUpdate(update)
	if err != nil {
		return SettingsConfigResponse{}, err
	}
	if update.ReuseDeepSeekKeyForProfile && !containsClassifierModel(enabled, ClassifierModelDeepSeekV4Flash) {
		return SettingsConfigResponse{}, fmt.Errorf("DeepSeek classifier model must be enabled to reuse its key for Profile generation")
	}
	for _, modelID := range enabled {
		if strings.TrimSpace(classifierCredentialValue(current, modelID, update.Credentials)) == "" {
			if spec, ok := classifierModelSpec(modelID); ok && spec.KeyOptional {
				continue
			}
			return SettingsConfigResponse{}, fmt.Errorf("API key is required for enabled classifier model %s", modelID)
		}
	}
	merged := make(map[string]SettingsConfigFieldUpdate, len(fields)+2+len(update.Credentials))
	for key, field := range fields {
		merged[key] = field
	}
	enabledValue := strings.Join(enabled, ",")
	merged[classifierEnabledModelsKey] = SettingsConfigFieldUpdate{Value: &enabledValue}
	merged[classifierDefaultModelKey] = SettingsConfigFieldUpdate{Value: &defaultID}

	for modelID, credentialUpdate := range update.Credentials {
		key, ok := classifierCredentialKey(modelID)
		if !ok {
			return SettingsConfigResponse{}, fmt.Errorf("unsupported classifier model credential: %s", modelID)
		}
		merged[key] = credentialUpdate
	}

	// Materialize legacy local credentials into their new provider-specific slots before
	// cleaning legacy keys. Environment overrides remain authoritative and are not rewritten.
	for _, spec := range classifierModelCatalog {
		key, _ := classifierCredentialKey(spec.ID)
		if _, explicitlyUpdated := merged[key]; explicitlyUpdated {
			continue
		}
		resolved := current.ClassifierModels.Models[spec.ID]
		if strings.TrimSpace(resolved.APIKey) == "" || resolved.APIKeySource == "environment" {
			continue
		}
		secret := resolved.APIKey
		merged[key] = SettingsConfigFieldUpdate{Value: &secret}
	}

	if _, err := UpdateLocalSettings(root, merged); err != nil {
		return SettingsConfigResponse{}, err
	}
	if err := removeLegacyClassifierLocalValues(root); err != nil {
		return SettingsConfigResponse{}, err
	}

	if update.ReuseDeepSeekKeyForProfile {
		updated, loadErr := Load(root)
		if loadErr != nil {
			return SettingsConfigResponse{}, loadErr
		}
		if strings.TrimSpace(updated.ProfileAPIKey) == "" {
			deepSeek := updated.ClassifierModels.Models[ClassifierModelDeepSeekV4Flash]
			if strings.TrimSpace(deepSeek.APIKey) == "" {
				return SettingsConfigResponse{}, fmt.Errorf("cannot reuse the DeepSeek classifier key because it is not configured")
			}
			profileKey := deepSeek.APIKey
			if _, err := UpdateLocalSettings(root, map[string]SettingsConfigFieldUpdate{
				"SCIRSS_PROFILE_API_KEY": {Value: &profileKey},
			}); err != nil {
				return SettingsConfigResponse{}, err
			}
		}
	}
	return SettingsConfig(root)
}

func classifierCredentialKey(modelID string) (string, bool) {
	switch modelID {
	case ClassifierModelDeepSeekV4Flash:
		return classifierDeepSeekAPIKey, true
	case ClassifierModelGLM53Flash:
		return classifierGLMAPIKey, true
	case ClassifierModelQwen38Flash:
		return classifierQwenAPIKey, true
	case ClassifierModelMiMoV25:
		return classifierMiMoAPIKey, true
	case ClassifierModelMiMoZenFree:
		return classifierZenAPIKey, true
	default:
		return "", false
	}
}

func classifierCredentialValue(current Settings, modelID string, updates map[string]SettingsConfigFieldUpdate) string {
	resolved := current.ClassifierModels.Models[modelID]
	if update, ok := updates[modelID]; ok {
		if update.Value != nil && strings.TrimSpace(*update.Value) != "" {
			return strings.TrimSpace(*update.Value)
		}
		if update.Clear && resolved.APIKeySource != "environment" {
			return ""
		}
	}
	return strings.TrimSpace(resolved.APIKey)
}

func removeLegacyClassifierLocalValues(root string) error {
	appRoot, err := appruntime.ResolveAppRoot(root)
	if err != nil {
		return err
	}
	if appruntime.DetectMode(appRoot) == appruntime.ModeSource {
		path := filepath.Join(appRoot, ".env")
		values := readDotEnv(path)
		for _, key := range []string{"SCIRSS_CLASSIFIER_API_KEY", "SCIRSS_CLASSIFIER_BASE_URL", "SCIRSS_CLASSIFIER_MODEL"} {
			delete(values, key)
		}
		return writeDotEnv(path, values)
	}
	settings := readReleaseSettings(filepath.Join(appruntime.DefaultUserDataDir(), "config", "settings.json"))
	secretsPath := filepath.Join(appruntime.DefaultUserDataDir(), "config", "secrets.json")
	secrets, err := loadSecretValues(secretsPath)
	if err != nil {
		return err
	}
	delete(secrets, "SCIRSS_CLASSIFIER_API_KEY")
	for _, key := range []string{"SCIRSS_CLASSIFIER_BASE_URL", "SCIRSS_CLASSIFIER_MODEL"} {
		delete(settings, key)
	}
	if err := writeReleaseSettings(filepath.Join(appruntime.DefaultUserDataDir(), "config", "settings.json"), settings); err != nil {
		return err
	}
	if err := storeSecretValues(secretsPath, secrets); err != nil {
		return err
	}
	return nil
}

// ClassifierModelsForSettings is intentionally small for API handlers and tests.
func ClassifierModelsForSettings(settings Settings) ClassifierModelsResponse {
	return classifierModelsResponse(settings)
}

// ClassifierModelForID resolves a fixed catalog entry for connection tests.
func ClassifierModelForID(settings Settings, modelID string) (ClassifierModelConfig, error) {
	model, err := ClassifierModelConfigForID(settings, modelID)
	if err != nil {
		return ClassifierModelConfig{}, err
	}
	if strings.TrimSpace(model.APIKey) == "" {
		return ClassifierModelConfig{}, fmt.Errorf("API key is required for classifier model %s", strings.TrimSpace(modelID))
	}
	return model, nil
}

// ClassifierModelConfigForID resolves a fixed catalog entry without requiring a saved key.
// Connection tests use this seam so an unsaved key can be supplied in the request body.
func ClassifierModelConfigForID(settings Settings, modelID string) (ClassifierModelConfig, error) {
	spec, ok := classifierModelSpec(strings.TrimSpace(modelID))
	if !ok {
		return ClassifierModelConfig{}, fmt.Errorf("unsupported classifier model: %s", modelID)
	}
	model := settings.ClassifierModels.Models[spec.ID]
	if model.ID == "" {
		model.ClassifierModelSpec = spec
	} else {
		if model.Provider == "" {
			model.Provider = spec.Provider
		}
		if model.Label == "" {
			model.Label = spec.Label
		}
		if model.BaseURL == "" {
			model.BaseURL = spec.BaseURL
		}
		if model.Thinking == "" {
			model.Thinking = spec.Thinking
		}
		if model.ReasoningEffort == "" {
			model.ReasoningEffort = spec.ReasoningEffort
		}
	}
	if strings.TrimSpace(model.APIKey) == "" && settings.EffectiveClassifierModelName() == spec.ID {
		model.APIKey = settings.ClassifierAPIKey
	}
	return applyClassifierKeyFallback(classifierModelWithThinkingPreference(model, settings.ClassifierThinking)), nil
}

// ClassifierModelStorageKey is exposed only to keep tests and adapters from duplicating key rules.
func ClassifierModelStorageKey(modelID string) string {
	key, _ := classifierCredentialKey(modelID)
	return key
}

func firstNonEmptyClassifierSource(source string) string {
	if strings.TrimSpace(source) == "" {
		return "unset"
	}
	return source
}

func validateClassifierModelsUpdate(update ClassifierModelsUpdate) ([]string, string, error) {
	if len(update.EnabledModelIDs) == 0 {
		return nil, "", fmt.Errorf("at least one classifier model must be enabled")
	}
	seen := make(map[string]struct{}, len(update.EnabledModelIDs))
	enabled := make([]string, 0, len(update.EnabledModelIDs))
	for _, rawID := range update.EnabledModelIDs {
		id := strings.TrimSpace(rawID)
		if _, ok := classifierModelSpec(id); !ok {
			return nil, "", fmt.Errorf("unsupported classifier model: %s", id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		enabled = append(enabled, id)
	}
	if len(enabled) == 0 {
		return nil, "", fmt.Errorf("at least one classifier model must be enabled")
	}
	defaultID := strings.TrimSpace(update.DefaultModelID)
	if len(enabled) == 1 {
		// A single enabled model is always the default, even when the caller is
		// removing the previous default in the same update.
		defaultID = enabled[0]
	} else if defaultID == "" {
		defaultID = enabled[0]
	}
	if _, ok := seen[defaultID]; !ok {
		return nil, "", fmt.Errorf("default classifier model must be enabled")
	}
	// Keep the catalog order stable in persisted files and API responses.
	sort.SliceStable(enabled, func(i, j int) bool { return classifierCatalogIndex(enabled[i]) < classifierCatalogIndex(enabled[j]) })
	return enabled, defaultID, nil
}

func containsClassifierModel(models []string, wanted string) bool {
	for _, model := range models {
		if model == wanted {
			return true
		}
	}
	return false
}

func classifierCatalogIndex(id string) int {
	for index, spec := range classifierModelCatalog {
		if spec.ID == id {
			return index
		}
	}
	return len(classifierModelCatalog)
}
