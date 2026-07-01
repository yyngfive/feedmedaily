package trayapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	appconfig "github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const (
	appName            = "FeedMeDaily"
	defaultHost        = "127.0.0.1"
	defaultPort        = 8000
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

type TraySettings = appruntime.TraySchedulerSettings

func ResolveLayout(root string) (Layout, error) {
	// 根据 root 和运行模式，推导托盘、数据目录、图标和服务默认地址。
	if root == "" {
		return Layout{}, errors.New("root directory is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("resolve root: %w", err)
	}

	settings, err := appconfig.Load(absRoot)
	if err != nil {
		return Layout{}, fmt.Errorf("load app settings: %w", err)
	}
	mode := settings.Mode

	layout := Layout{
		Mode:       mode,
		RootDir:    absRoot,
		AppDir:     absRoot,
		ServerHost: settings.ServerHost,
		ServerPort: settings.ServerPort,
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
	// 托盘和 Web API 共用同一个 tray-settings.json 模型。
	return appruntime.LoadTraySchedulerSettings(path)
}
