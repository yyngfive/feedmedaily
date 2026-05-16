package trayapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	appName            = "FeedMeDaily"
	defaultHost        = "127.0.0.1"
	defaultPort        = 8000
	defaultDailyTime   = "10:00"
	runtimeModeSource  = "source"
	runtimeModeRelease = "release"
)

type AppConfig struct {
	RootDir string
}

type Layout struct {
	Mode             string
	RootDir          string
	AppDir           string
	UserDataDir      string
	ConfigDir        string
	DataDir          string
	LogsDir          string
	ReportsDir       string
	RuntimeStatePath string
	TraySettingsPath string
	IconPath         string
	ServerHost       string
	ServerPort       int
}

type TraySettings struct {
	ScheduleEnabled bool   `json:"schedule_enabled"`
	DailyTime       string `json:"daily_time"`
	LastRunDate     string `json:"last_run_date,omitempty"`
	LaunchAtLogin   bool   `json:"launch_at_login"`
}

func ResolveLayout(root string) (Layout, error) {
	// 根据 root 和运行模式，推导托盘、数据目录、图标和服务默认地址。
	if root == "" {
		return Layout{}, errors.New("root directory is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve root: %w", err)
	}

	mode := detectRuntimeMode(absRoot)
	serverHost := strings.TrimSpace(os.Getenv("SCIRSS_SERVER_HOST"))
	if serverHost == "" {
		serverHost = defaultHost
	}

	serverPort := defaultPort
	if raw := strings.TrimSpace(os.Getenv("SCIRSS_SERVER_PORT")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			serverPort = parsed
		}
	}

	layout := Layout{
		Mode:       mode,
		RootDir:    absRoot,
		AppDir:     absRoot,
		ServerHost: serverHost,
		ServerPort: serverPort,
	}

	if mode == runtimeModeRelease {
		userDataDir := defaultUserDataDir()
		layout.UserDataDir = userDataDir
		layout.ConfigDir = filepath.Join(userDataDir, "config")
		layout.DataDir = filepath.Join(userDataDir, "data")
		layout.LogsDir = filepath.Join(userDataDir, "logs")
		layout.ReportsDir = filepath.Join(userDataDir, "reports")
		layout.IconPath = filepath.Join(absRoot, "feedmedaily.ico")
	} else {
		layout.UserDataDir = absRoot
		layout.ConfigDir = absRoot
		layout.DataDir = filepath.Join(absRoot, "data")
		layout.LogsDir = filepath.Join(absRoot, "logs")
		layout.ReportsDir = filepath.Join(absRoot, "reports")
		layout.IconPath = filepath.Join(absRoot, "assets", "branding", "feedmedaily.ico")
	}

	layout.RuntimeStatePath = filepath.Join(layout.ConfigDir, "runtime.json")
	layout.TraySettingsPath = filepath.Join(layout.ConfigDir, "tray-settings.json")

	for _, dir := range []string{layout.ConfigDir, layout.DataDir, layout.LogsDir, layout.ReportsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Layout{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}

	return layout, nil
}

func detectRuntimeMode(root string) string {
	// 优先读环境变量覆盖；否则根据目录结构猜测 source/release。
	override := strings.ToLower(strings.TrimSpace(os.Getenv("FEEDMEDAILY_RUNTIME_MODE")))
	switch override {
	case runtimeModeRelease:
		return runtimeModeRelease
	case runtimeModeSource:
		return runtimeModeSource
	}

	if looksLikeSourceRoot(root) {
		return runtimeModeSource
	}
	if looksLikeReleaseRoot(root) {
		return runtimeModeRelease
	}
	return runtimeModeRelease
}

func looksLikeSourceRoot(path string) bool {
	// 用仓库根目录标志判定 source 模式，不要求 Python 源码目录仍然存在。
	_, pyprojectErr := os.Stat(filepath.Join(path, "pyproject.toml"))
	_, gomodErr := os.Stat(filepath.Join(path, "go.mod"))
	return pyprojectErr == nil || gomodErr == nil
}

func looksLikeReleaseRoot(path string) bool {
	// 用安装产物的典型文件组合判定 release 模式。
	candidates := []string{
		filepath.Join(path, "feedmedailyd.exe"),
		filepath.Join(path, "FeedMeDailyTray.exe"),
		filepath.Join(path, "feedmedaily.ico"),
		filepath.Join(path, "web", "dist", "index.html"),
	}
	matched := 0
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			matched++
		}
	}
	return matched >= 2
}

func defaultUserDataDir() string {
	// 托盘在 release 模式下使用用户本地目录保存状态和数据。
	if override := strings.TrimSpace(os.Getenv("FEEDMEDAILY_DATA_ROOT")); override != "" {
		return override
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, appName)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", appName)
	}
	return filepath.Join(homeDir, "AppData", "Local", appName)
}

func LoadTraySettings(path string) (TraySettings, error) {
	// 读取托盘自己的本地配置文件；缺失时返回默认设置。
	settings := defaultTraySettings()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settings, nil
		}
		return TraySettings{}, fmt.Errorf("read tray settings: %w", err)
	}

	if err := json.Unmarshal(data, &settings); err != nil {
		return TraySettings{}, fmt.Errorf("parse tray settings: %w", err)
	}

	if err := settings.Normalize(); err != nil {
		return TraySettings{}, err
	}
	return settings, nil
}

func defaultTraySettings() TraySettings {
	// 托盘配置默认值：不自动调度，默认时间 10:00，不开机自启。
	return TraySettings{
		ScheduleEnabled: false,
		DailyTime:       defaultDailyTime,
		LaunchAtLogin:   false,
	}
}

func (s *TraySettings) Normalize() error {
	// 把托盘配置中的时间字段规范成稳定格式。
	if strings.TrimSpace(s.DailyTime) == "" {
		s.DailyTime = defaultDailyTime
	}
	s.DailyTime = normalizeDailyTime(s.DailyTime)
	if s.DailyTime == "" {
		return errors.New("tray settings daily_time must be in HH:MM format")
	}
	return nil
}

func (s TraySettings) Save(path string) error {
	// 用临时文件替换的方式保存 tray-settings.json。
	if err := s.Normalize(); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tray settings: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write tray settings temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace tray settings: %w", err)
	}
	return nil
}

func normalizeDailyTime(value string) string {
	// 把时间输入规范化为 HH:MM，非法时返回空字符串。
	clean := strings.TrimSpace(value)
	parts := strings.Split(clean, ":")
	if len(parts) != 2 {
		return ""
	}
	hour, errHour := strconv.Atoi(parts[0])
	minute, errMinute := strconv.Atoi(parts[1])
	if errHour != nil || errMinute != nil {
		return ""
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", hour, minute)
}
