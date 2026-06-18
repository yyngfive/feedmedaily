package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const (
	defaultHost              = "127.0.0.1"
	defaultPort              = 8000
	DefaultUpdateManifestURL = "https://github.com/yyngfive/feedmedaily/releases/latest/download/update.json"
)

type Settings struct {
	Mode                string
	RootDir             string
	AppDir              string
	UserDataDir         string
	ConfigDir           string
	SettingsStorePath   string
	SecretsStorePath    string
	DataDir             string
	DatabasePath        string
	LogsDir             string
	ReportsDir          string
	RuntimeStatePath    string
	WebDistDir          string
	FeedsPath           string
	ProfilePath         string
	UpdateManifestURL   string
	ClassifierAPIKey    string
	ClassifierBaseURL   string
	ClassifierModel     string
	ClassifierThinking  string
	ClassifierBatchSize int
	ProfileAPIKey       string
	ProfileBaseURL      string
	ProfileModel        string
	ProfileThinking     string
	ZoteroAPIKey        string
	ZoteroLibraryType   string
	ZoteroLibraryID     string
	ZoteroCollectionKey string
	ServerHost          string
	ServerPort          int
}

type Option struct {
	Key         string
	Label       string
	Description string
	Section     string
	InputType   string
	Default     string
	Secret      bool
	Options     []SettingOption
}

type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ResolvedValue struct {
	Option          Option
	Value           string
	Source          string
	StoredLocally   bool
	Configured      bool
	StorageLabel    string
	DefaultValue    string
	HasDefaultValue bool
}

type SettingsConfigField struct {
	Key            string          `json:"key"`
	Label          string          `json:"label"`
	Description    string          `json:"description"`
	Section        string          `json:"section"`
	InputType      string          `json:"input_type"`
	Secret         bool            `json:"secret"`
	Configured     bool            `json:"configured"`
	Source         string          `json:"source"`
	StoredInDotenv bool            `json:"stored_in_dotenv"`
	StorageLabel   *string         `json:"storage_label"`
	Value          *string         `json:"value"`
	DefaultValue   *string         `json:"default_value"`
	Options        []SettingOption `json:"options"`
}

type SettingsConfigResponse struct {
	Fields []SettingsConfigField `json:"fields"`
}

type SettingsConfigFieldUpdate struct {
	Value *string `json:"value"`
	Clear bool    `json:"clear"`
}

type SettingsConfigUpdateRequest struct {
	Fields map[string]SettingsConfigFieldUpdate `json:"fields"`
}

var Options = []Option{
	{
		Key:         "SCIRSS_CLASSIFIER_API_KEY",
		Label:       "Classifier API key",
		Description: "Used only for paper classification requests.",
		Section:     "Classifier model",
		InputType:   "password",
		Secret:      true,
	},
	{
		Key:         "SCIRSS_CLASSIFIER_BASE_URL",
		Label:       "Classifier base URL",
		Description: "Base URL for the classifier model provider.",
		Section:     "Classifier model",
		InputType:   "url",
		Default:     "https://api.deepseek.com",
	},
	{
		Key:         "SCIRSS_CLASSIFIER_MODEL",
		Label:       "Classifier model",
		Description: "Model name used for paper classification.",
		Section:     "Classifier model",
		InputType:   "text",
		Default:     "deepseek-v4-flash",
	},
	{
		Key:         "SCIRSS_CLASSIFIER_THINKING",
		Label:       "Classifier thinking",
		Description: "Whether the classifier role requests provider reasoning mode.",
		Section:     "Classifier model",
		InputType:   "select",
		Default:     "disabled",
		Options: []SettingOption{
			{Value: "disabled", Label: "Disabled"},
			{Value: "enabled", Label: "Enabled"},
		},
	},
	{
		Key:         "SCIRSS_CLASSIFIER_BATCH_SIZE",
		Label:       "Classifier batch size",
		Description: "How many papers are sent to the classifier per batch.",
		Section:     "Classifier model",
		InputType:   "number",
		Default:     "10",
	},
	{
		Key:         "SCIRSS_PROFILE_API_KEY",
		Label:       "Profile API key",
		Description: "Used for onboarding, profile generation, and profile revision.",
		Section:     "Profile model",
		InputType:   "password",
		Secret:      true,
	},
	{
		Key:         "SCIRSS_PROFILE_BASE_URL",
		Label:       "Profile base URL",
		Description: "Base URL for the profile-generation model provider.",
		Section:     "Profile model",
		InputType:   "url",
		Default:     "https://api.deepseek.com",
	},
	{
		Key:         "SCIRSS_PROFILE_MODEL",
		Label:       "Profile model",
		Description: "Model name used for initial and feedback-driven profile proposals.",
		Section:     "Profile model",
		InputType:   "text",
		Default:     "deepseek-v4-pro",
	},
	{
		Key:         "SCIRSS_PROFILE_THINKING",
		Label:       "Profile thinking",
		Description: "Whether the profile role requests provider reasoning mode.",
		Section:     "Profile model",
		InputType:   "select",
		Default:     "enabled",
		Options: []SettingOption{
			{Value: "disabled", Label: "Disabled"},
			{Value: "enabled", Label: "Enabled"},
		},
	},
	{
		Key:         "SCIRSS_PROFILE_PATH",
		Label:       "Profile file path",
		Description: "Path for the active classification profile file.",
		Section:     "Local files",
		InputType:   "text",
	},
	{
		Key:         "SCIRSS_ZOTERO_API_KEY",
		Label:       "Zotero API key",
		Description: "Used for Zotero collection lookup and paper save operations.",
		Section:     "Zotero",
		InputType:   "password",
		Secret:      true,
	},
	{
		Key:         "SCIRSS_ZOTERO_LIBRARY_TYPE",
		Label:       "Zotero library type",
		Description: "Select whether Zotero saves target a personal or group library.",
		Section:     "Zotero",
		InputType:   "select",
		Default:     "user",
		Options: []SettingOption{
			{Value: "user", Label: "User"},
			{Value: "group", Label: "Group"},
		},
	},
	{
		Key:         "SCIRSS_ZOTERO_LIBRARY_ID",
		Label:       "Zotero library ID",
		Description: "User ID for personal libraries or group ID for group libraries.",
		Section:     "Zotero",
		InputType:   "text",
	},
	{
		Key:         "SCIRSS_ZOTERO_COLLECTION_KEY",
		Label:       "Default Zotero collection key",
		Description: "Optional default collection for Save to Zotero.",
		Section:     "Zotero",
		InputType:   "text",
	},
	{
		Key:         "SCIRSS_SERVER_HOST",
		Label:       "Server host",
		Description: "Host interface for the local backend service.",
		Section:     "Local app",
		InputType:   "text",
		Default:     defaultHost,
	},
	{
		Key:         "SCIRSS_SERVER_PORT",
		Label:       "Server port",
		Description: "Preferred port for the local backend service.",
		Section:     "Local app",
		InputType:   "number",
		Default:     strconv.Itoa(defaultPort),
	},
	{
		Key:         "FEEDMEDAILY_UPDATE_MANIFEST_URL",
		Label:       "Update manifest URL",
		Description: "Remote JSON manifest used for in-app update checks.",
		Section:     "Release",
		InputType:   "url",
		Default:     DefaultUpdateManifestURL,
	},
}

var optionsByKey = func() map[string]Option {
	result := make(map[string]Option, len(Options))
	for _, option := range Options {
		result[option.Key] = option
	}
	return result
}()

func Load(root string) (Settings, error) {
	// 解析应用运行目录、用户数据目录和当前生效的服务器配置。
	appRoot, err := appruntime.ResolveAppRoot(root)
	if err != nil {
		return Settings{}, err
	}
	mode := appruntime.DetectMode(appRoot)
	userDataDir, configDir, dataDir, logsDir, reportsDir := layoutDirs(appRoot, mode)
	settings := Settings{
		Mode:              mode,
		RootDir:           appRoot,
		AppDir:            appRoot,
		UserDataDir:       userDataDir,
		ConfigDir:         configDir,
		SettingsStorePath: releaseSettingsPath(),
		SecretsStorePath:  releaseSecretsPath(),
		DataDir:           dataDir,
		DatabasePath:      filepath.Join(dataDir, "literature.sqlite"),
		LogsDir:           logsDir,
		ReportsDir:        reportsDir,
		RuntimeStatePath:  filepath.Join(configDir, "runtime.json"),
		WebDistDir:        appruntime.ResolveWebDistDir(appRoot),
		FeedsPath:         filepath.Join(dataDir, "rss_feeds.json"),
	}

	for _, dir := range []string{settings.ConfigDir, settings.DataDir, settings.LogsDir, settings.ReportsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Settings{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}

	values, err := ResolvedValues(appRoot)
	if err != nil {
		return Settings{}, err
	}
	valueMap := resolvedValueMap(values)
	settings.ProfilePath = resolveProfilePath(appRoot, valueMap["SCIRSS_PROFILE_PATH"])
	settings.UpdateManifestURL = valueOrDefault(valueMap["FEEDMEDAILY_UPDATE_MANIFEST_URL"], DefaultUpdateManifestURL)
	settings.ClassifierAPIKey = optionalValue(valueMap["SCIRSS_CLASSIFIER_API_KEY"])
	settings.ClassifierBaseURL = valueOrDefault(valueMap["SCIRSS_CLASSIFIER_BASE_URL"], "https://api.deepseek.com")
	settings.ClassifierModel = valueOrDefault(valueMap["SCIRSS_CLASSIFIER_MODEL"], "deepseek-v4-flash")
	settings.ClassifierThinking = valueOrDefault(strings.ToLower(strings.TrimSpace(valueMap["SCIRSS_CLASSIFIER_THINKING"])), "disabled")
	settings.ClassifierBatchSize = positiveInt(valueMap["SCIRSS_CLASSIFIER_BATCH_SIZE"], 10)
	settings.ProfileAPIKey = optionalValue(valueMap["SCIRSS_PROFILE_API_KEY"])
	settings.ProfileBaseURL = valueOrDefault(valueMap["SCIRSS_PROFILE_BASE_URL"], "https://api.deepseek.com")
	settings.ProfileModel = valueOrDefault(valueMap["SCIRSS_PROFILE_MODEL"], "deepseek-v4-pro")
	settings.ProfileThinking = valueOrDefault(strings.ToLower(strings.TrimSpace(valueMap["SCIRSS_PROFILE_THINKING"])), "enabled")
	settings.ZoteroAPIKey = optionalValue(valueMap["SCIRSS_ZOTERO_API_KEY"])
	settings.ZoteroLibraryType = valueOrDefault(strings.ToLower(strings.TrimSpace(valueMap["SCIRSS_ZOTERO_LIBRARY_TYPE"])), "user")
	settings.ZoteroLibraryID = optionalValue(valueMap["SCIRSS_ZOTERO_LIBRARY_ID"])
	settings.ZoteroCollectionKey = optionalValue(valueMap["SCIRSS_ZOTERO_COLLECTION_KEY"])
	settings.ServerHost = valueOrDefault(valueMap["SCIRSS_SERVER_HOST"], defaultHost)
	settings.ServerPort = positiveInt(valueMap["SCIRSS_SERVER_PORT"], defaultPort)
	return settings, nil
}

func ResolvedValues(root string) ([]ResolvedValue, error) {
	// 按 environment > 本地存储 > 默认值 的优先级解析全部设置项。
	appRoot, err := appruntime.ResolveAppRoot(root)
	if err != nil {
		return nil, err
	}
	mode := appruntime.DetectMode(appRoot)
	dotenvItems := map[string]string{}
	if mode == appruntime.ModeSource {
		dotenvItems = readDotEnv(projectEnvPath(appRoot))
	}
	releaseItems, releaseSecretItems := map[string]string{}, map[string]string{}
	if mode == appruntime.ModeRelease {
		releaseItems = readReleaseSettings(releaseSettingsPath())
		var secretErr error
		releaseSecretItems, secretErr = loadSecretValues(releaseSecretsPath())
		if secretErr != nil {
			return nil, secretErr
		}
	}

	resolved := make([]ResolvedValue, 0, len(Options))
	for _, option := range Options {
		defaultValue, hasDefault := defaultValueForOption(option, appRoot, mode)
		value, source, storageLabel, storedLocally := "", "unset", "", false
		if raw, ok := os.LookupEnv(option.Key); ok {
			value = raw
			source = "environment"
			storageLabel = "System environment"
			_, inDotenv := dotenvItems[option.Key]
			_, inSettings := releaseItems[option.Key]
			_, inSecrets := releaseSecretItems[option.Key]
			storedLocally = inDotenv || inSettings || inSecrets
		} else if mode == appruntime.ModeSource {
			if raw, ok := dotenvItems[option.Key]; ok {
				value = raw
				source = "dotenv"
				storageLabel = ".env"
				storedLocally = true
			} else if hasDefault {
				value = defaultValue
				source = "default"
				storageLabel = "Built-in default"
			}
		} else if mode == appruntime.ModeRelease && option.Secret {
			if raw, ok := releaseSecretItems[option.Key]; ok {
				value = raw
				source = "secret_store"
				storageLabel = appruntime.AppPublicName + " secure store"
				storedLocally = true
			} else if hasDefault {
				value = defaultValue
				source = "default"
				storageLabel = "Built-in default"
			}
		} else if mode == appruntime.ModeRelease {
			if raw, ok := releaseItems[option.Key]; ok {
				value = raw
				source = "settings"
				storageLabel = appruntime.AppPublicName + " settings.json"
				storedLocally = true
			} else if hasDefault {
				value = defaultValue
				source = "default"
				storageLabel = "Built-in default"
			}
		}

		resolved = append(resolved, ResolvedValue{
			Option:          option,
			Value:           value,
			Source:          source,
			StoredLocally:   storedLocally,
			Configured:      value != "",
			StorageLabel:    storageLabel,
			DefaultValue:    defaultValue,
			HasDefaultValue: hasDefault,
		})
	}
	return resolved, nil
}

func SettingsConfig(root string) (SettingsConfigResponse, error) {
	// 把解析后的设置转换成前端设置页可直接消费的结构。
	values, err := ResolvedValues(root)
	if err != nil {
		return SettingsConfigResponse{}, err
	}
	return SettingsConfigResponse{Fields: fieldsFromResolvedValues(values)}, nil
}

func UpdateLocalSettings(root string, updates map[string]SettingsConfigFieldUpdate) (SettingsConfigResponse, error) {
	// 根据当前模式把设置写回 .env 或 release 配置/密钥存储。
	appRoot, err := appruntime.ResolveAppRoot(root)
	if err != nil {
		return SettingsConfigResponse{}, err
	}
	mode := appruntime.DetectMode(appRoot)
	if mode == appruntime.ModeRelease {
		ordinaryValues := readReleaseSettings(releaseSettingsPath())
		secretValues, err := loadSecretValues(releaseSecretsPath())
		if err != nil {
			return SettingsConfigResponse{}, err
		}
		for key, update := range updates {
			option, ok := optionsByKey[key]
			if !ok {
				return SettingsConfigResponse{}, fmt.Errorf("unsupported setting: %s", key)
			}
			normalized, hasValue, err := normalizeSettingValue(option, update.Value)
			if err != nil {
				return SettingsConfigResponse{}, err
			}
			if option.Secret {
				if hasValue {
					secretValues[key] = normalized
				} else if update.Clear {
					delete(secretValues, key)
				}
				continue
			}
			if hasValue {
				ordinaryValues[key] = normalized
			} else {
				delete(ordinaryValues, key)
			}
		}
		if err := writeReleaseSettings(releaseSettingsPath(), ordinaryValues); err != nil {
			return SettingsConfigResponse{}, err
		}
		if err := storeSecretValues(releaseSecretsPath(), secretValues); err != nil {
			return SettingsConfigResponse{}, err
		}
		return SettingsConfig(appRoot)
	}

	envPath := projectEnvPath(appRoot)
	envValues := readDotEnv(envPath)
	for key, update := range updates {
		option, ok := optionsByKey[key]
		if !ok {
			return SettingsConfigResponse{}, fmt.Errorf("unsupported setting: %s", key)
		}
		normalized, hasValue, err := normalizeSettingValue(option, update.Value)
		if err != nil {
			return SettingsConfigResponse{}, err
		}
		if hasValue {
			envValues[key] = normalized
		} else if update.Clear || update.Value != nil {
			delete(envValues, key)
		}
	}
	if err := writeDotEnv(envPath, envValues); err != nil {
		return SettingsConfigResponse{}, err
	}
	return SettingsConfig(appRoot)
}

func fieldsFromResolvedValues(values []ResolvedValue) []SettingsConfigField {
	// 把内部解析结果转成 API 返回字段，并隐藏 secret 的真实值。
	fields := make([]SettingsConfigField, 0, len(values))
	for _, value := range values {
		var storageLabel *string
		if value.StorageLabel != "" {
			label := value.StorageLabel
			storageLabel = &label
		}
		var fieldValue *string
		if !value.Option.Secret {
			v := value.Value
			fieldValue = &v
		}
		var defaultValue *string
		if value.HasDefaultValue {
			v := value.DefaultValue
			defaultValue = &v
		}
		fields = append(fields, SettingsConfigField{
			Key:            value.Option.Key,
			Label:          value.Option.Label,
			Description:    value.Option.Description,
			Section:        value.Option.Section,
			InputType:      value.Option.InputType,
			Secret:         value.Option.Secret,
			Configured:     value.Configured,
			Source:         value.Source,
			StoredInDotenv: value.StoredLocally,
			StorageLabel:   storageLabel,
			Value:          fieldValue,
			DefaultValue:   defaultValue,
			Options:        value.Option.Options,
		})
	}
	return fields
}

func layoutDirs(appRoot string, mode string) (string, string, string, string, string) {
	// 统一定义 source/release 两种模式下的数据目录布局。
	if mode == appruntime.ModeRelease {
		userDataDir := appruntime.DefaultUserDataDir()
		return userDataDir,
			filepath.Join(userDataDir, "config"),
			filepath.Join(userDataDir, "data"),
			filepath.Join(userDataDir, "logs"),
			filepath.Join(userDataDir, "reports")
	}
	return appRoot, appRoot, filepath.Join(appRoot, "data"), filepath.Join(appRoot, "logs"), filepath.Join(appRoot, "reports")
}

func projectEnvPath(root string) string {
	// source 模式的可编辑配置文件固定为仓库根目录下的 .env。
	return filepath.Join(root, ".env")
}

func releaseSettingsPath() string {
	// release 模式的普通配置固定写到用户目录下的 settings.json。
	return filepath.Join(appruntime.DefaultUserDataDir(), "config", "settings.json")
}

func releaseSecretsPath() string {
	// release 模式的密钥配置固定写到用户目录下的 secrets.json。
	return filepath.Join(appruntime.DefaultUserDataDir(), "config", "secrets.json")
}

func defaultValueForOption(option Option, root string, mode string) (string, bool) {
	// 某些字段的默认值依赖当前模式和目录结构，例如 profile 路径。
	if option.Key == "SCIRSS_PROFILE_PATH" {
		_, _, dataDir, _, _ := layoutDirs(root, mode)
		if mode == appruntime.ModeRelease {
			return filepath.Join(dataDir, "classification_profile.json"), true
		}
		return filepath.Join("data", "classification_profile.json"), true
	}
	if option.Default == "" {
		return "", false
	}
	return option.Default, true
}

func resolveProfilePath(root string, value string) string {
	// 允许用户传绝对路径；相对路径则相对应用根目录解析。
	if strings.TrimSpace(value) == "" {
		value = filepath.Join("data", "classification_profile.json")
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, filepath.Clean(value))
}

func resolvedValueMap(values []ResolvedValue) map[string]string {
	// 便于后续按 key 快速读取已经解析好的值。
	result := make(map[string]string, len(values))
	for _, value := range values {
		result[value.Option.Key] = value.Value
	}
	return result
}

func readReleaseSettings(path string) map[string]string {
	// 读取 release 模式下的普通配置文件；失败时回退到空 map。
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	var payload struct {
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.Values == nil {
		return map[string]string{}
	}
	return payload.Values
}

func writeReleaseSettings(path string, values map[string]string) error {
	// 把 release 普通配置写成稳定的 JSON 文件。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(map[string]map[string]string{"values": values}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

func readDotEnv(path string) map[string]string {
	// 读取简单的 KEY=VALUE 形式 .env，不尝试完整兼容所有 shell 语法。
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		clean := strings.TrimSpace(line)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		key, value, ok := strings.Cut(clean, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = trimEnvValue(value)
	}
	return values
}

func writeDotEnv(path string, values map[string]string) error {
	// 以固定顺序写回 .env，减少无意义 diff。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(Options))
	written := map[string]struct{}{}
	for _, option := range Options {
		if _, ok := values[option.Key]; ok {
			keys = append(keys, option.Key)
			written[option.Key] = struct{}{}
		}
	}
	unknownKeys := make([]string, 0)
	for key := range values {
		if _, ok := written[key]; !ok {
			unknownKeys = append(unknownKeys, key)
		}
	}
	sort.Strings(unknownKeys)
	keys = append(keys, unknownKeys...)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(quoteEnvValue(values[key]))
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func trimEnvValue(value string) string {
	// 去掉 .env 中可能存在的首尾引号，并恢复简单转义。
	clean := strings.TrimSpace(value)
	if len(clean) >= 2 {
		first := clean[0]
		last := clean[len(clean)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return strings.ReplaceAll(clean[1:len(clean)-1], `\'`, `'`)
		}
	}
	return clean
}

func quoteEnvValue(value string) string {
	// 为写回 .env 的值加引号，避免空格和特殊字符破坏解析。
	return "'" + strings.ReplaceAll(value, "'", `\'`) + "'"
}

func normalizeSettingValue(option Option, value *string) (string, bool, error) {
	// 根据字段类型做最小必要校验和标准化。
	if value == nil {
		return "", false, nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return "", false, nil
	}
	switch option.InputType {
	case "number":
		numeric, err := strconv.Atoi(clean)
		if err != nil || numeric < 1 {
			return "", false, fmt.Errorf("%s must be at least 1", option.Label)
		}
		return strconv.Itoa(numeric), true, nil
	case "url":
		if !strings.HasPrefix(clean, "http://") && !strings.HasPrefix(clean, "https://") {
			return "", false, fmt.Errorf("%s must start with http:// or https://", option.Label)
		}
		return clean, true, nil
	case "select":
		normalized := strings.ToLower(clean)
		for _, allowed := range option.Options {
			if normalized == allowed.Value {
				return normalized, true, nil
			}
		}
		return "", false, fmt.Errorf("%s must be one of the supported options", option.Label)
	default:
		return clean, true, nil
	}
}

func optionalValue(value string) string {
	// 仅做 trim，供可选字符串字段使用。
	return strings.TrimSpace(value)
}

func valueOrDefault(value string, fallback string) string {
	// 如果解析结果为空，则回退到给定默认值。
	clean := strings.TrimSpace(value)
	if clean == "" {
		return fallback
	}
	return clean
}

func positiveInt(value string, fallback int) int {
	// 把字符串转成正整数；非法时回退到默认值。
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}
