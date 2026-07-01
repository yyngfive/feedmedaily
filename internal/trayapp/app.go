package trayapp

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

type App struct {
	layout        Layout
	settings      TraySettings
	settingsMutex sync.Mutex
	tray          *windowsTray
	stopScheduler chan struct{}
	runSyncNow    func() error
}

func NewApp(cfg AppConfig) (*App, error) {
	// 初始化托盘应用：解析布局、读取设置，并同步当前自启动状态。
	layout, err := ResolveLayout(cfg.RootDir)
	if err != nil {
		return nil, err
	}
	settings, err := LoadTraySettings(layout.TraySettingsPath)
	if err != nil {
		return nil, err
	}

	autostartEnabled, err := isAutostartEnabled(layout.RootDir)
	if err == nil {
		settings.LaunchAtLogin = autostartEnabled
	}
	if err := settings.Save(layout.TraySettingsPath); err != nil {
		return nil, err
	}

	return &App{
		layout:        layout,
		settings:      settings,
		stopScheduler: make(chan struct{}),
	}, nil
}

func (a *App) Run() error {
	// 启动 Windows 托盘消息循环，并同时打开本地调度轮询。
	tray, err := newWindowsTray(a)
	if err != nil {
		return err
	}
	a.tray = tray
	a.startSettingsRefreshLoop()
	a.startSchedulerLoop()
	defer close(a.stopScheduler)
	return tray.Run()
}

func (a *App) MenuState() trayMenuState {
	// 返回菜单渲染所需的只读状态快照。
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	if _, err := a.refreshSettingsLocked(); err != nil {
		a.logSettingsRefreshError("menu_state_refresh_failed", err)
	}
	return trayMenuState{
		ScheduleEnabled: a.settings.ScheduleEnabled,
		DailyTime:       a.settings.DailyTime,
		LaunchAtLogin:   a.settings.LaunchAtLogin,
	}
}

func (a *App) OpenApp() error {
	// 确保后台服务可用后，用默认浏览器打开本地 UI。
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "open_app",
		Message:   "Opening FeedMeDaily from tray.",
	})
	baseURL, err := ensureService(a.layout)
	if err != nil {
		_, _ = logging.Write(a.layout.LogsDir, logging.Event{
			Level:     "error",
			Component: "tray",
			Action:    "open_app_failed",
			Error:     err.Error(),
		})
		return err
	}
	return OpenBrowser(baseURL)
}

func (a *App) StartService() error {
	// 仅保证后台服务启动，不额外打开浏览器。
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "start_service",
		Message:   "Starting background service from tray.",
	})
	_, err := ensureService(a.layout)
	if err != nil {
		_, _ = logging.Write(a.layout.LogsDir, logging.Event{
			Level:     "error",
			Component: "tray",
			Action:    "start_service_failed",
			Error:     err.Error(),
		})
	}
	return err
}

func (a *App) StopService() error {
	// 停止本地后台服务，并清理 runtime 状态。
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "stop_service",
		Message:   "Stopping background service from tray.",
	})
	err := stopService(a.layout)
	if err != nil {
		_, _ = logging.Write(a.layout.LogsDir, logging.Event{
			Level:     "error",
			Component: "tray",
			Action:    "stop_service_failed",
			Error:     err.Error(),
		})
	}
	return err
}

func (a *App) RunSyncNow() error {
	// 确保服务在运行，然后触发一次“立即同步”。
	if a.runSyncNow != nil {
		return a.runSyncNow()
	}
	return a.runSyncNowDefault()
}

func (a *App) runSyncNowDefault() error {
	// 确保服务在运行，然后触发一次“立即同步”。
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "run_sync_now",
		Message:   "Triggering one sync job from tray.",
	})
	baseURL, err := ensureService(a.layout)
	if err != nil {
		_, _ = logging.Write(a.layout.LogsDir, logging.Event{
			Level:     "error",
			Component: "tray",
			Action:    "run_sync_now_failed",
			Error:     err.Error(),
		})
		return err
	}
	if err := triggerSync(baseURL); err != nil {
		_, _ = logging.Write(a.layout.LogsDir, logging.Event{
			Level:     "error",
			Component: "tray",
			Action:    "run_sync_now_failed",
			Error:     err.Error(),
		})
		return err
	}
	return nil
}

func (a *App) ToggleScheduleEnabled() (trayMenuState, error) {
	// 切换托盘自己的本地每日调度开关。
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	if _, err := a.refreshSettingsLocked(); err != nil {
		a.logSettingsRefreshError("toggle_schedule_refresh_failed", err)
		return trayMenuState{}, err
	}
	a.settings.ScheduleEnabled = !a.settings.ScheduleEnabled
	if err := a.persistSettingsLocked(); err != nil {
		return trayMenuState{}, err
	}
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "toggle_schedule",
		Message:   "Updated tray-local daily sync setting.",
		Data: map[string]any{
			"enabled":    a.settings.ScheduleEnabled,
			"daily_time": a.settings.DailyTime,
		},
	})
	return a.currentMenuStateLocked(), nil
}

func (a *App) ToggleLaunchAtLogin() (trayMenuState, error) {
	// 切换 Windows 注册表中的开机自启动项。
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	nextValue := !a.settings.LaunchAtLogin
	if err := setAutostartEnabled(a.layout.RootDir, nextValue); err != nil {
		return trayMenuState{}, err
	}
	a.settings.LaunchAtLogin = nextValue
	if err := a.persistSettingsLocked(); err != nil {
		return trayMenuState{}, err
	}
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "toggle_launch_at_login",
		Message:   "Updated launch-at-login setting.",
		Data: map[string]any{
			"enabled": a.settings.LaunchAtLogin,
		},
	})
	return a.currentMenuStateLocked(), nil
}

func (a *App) OpenTraySettings() error {
	// 用系统默认处理器打开 tray-settings.json，方便手动查看和调试。
	if _, err := os.Stat(a.layout.TraySettingsPath); err != nil && os.IsNotExist(err) {
		if err := a.snapshotSettings().Save(a.layout.TraySettingsPath); err != nil {
			return err
		}
	}
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "info",
		Component: "tray",
		Action:    "open_tray_settings",
		Message:   "Opening tray settings file.",
		Data:      map[string]any{"path": a.layout.TraySettingsPath},
	})
	return OpenPath(a.layout.TraySettingsPath)
}

func (a *App) OpenDataDir() error {
	// 打开应用数据目录。
	return OpenPath(a.layout.DataDir)
}

func (a *App) OpenLogsDir() error {
	// 打开日志目录。
	return OpenPath(a.layout.LogsDir)
}

func (a *App) SchedulerLabel() string {
	// 根据当前调度状态生成菜单项文案。
	state := a.MenuState()
	if state.ScheduleEnabled {
		return fmt.Sprintf("Disable Daily Sync (%s)", state.DailyTime)
	}
	return fmt.Sprintf("Enable Daily Sync (%s)", state.DailyTime)
}

func (a *App) startSettingsRefreshLoop() {
	// 定期吸收 Web UI 写入的 tray-settings.json，保持托盘内存态和菜单状态同步。
	ticker := time.NewTicker(2 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.refreshSettingsFromDisk("settings_poll_refresh_failed")
			case <-a.stopScheduler:
				return
			}
		}
	}()
}

func (a *App) startSchedulerLoop() {
	// 每 30 秒检查一次是否到了托盘本地调度的触发时间。
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		a.maybeRunScheduledSync(time.Now())
		for {
			select {
			case <-ticker.C:
				a.maybeRunScheduledSync(time.Now())
			case <-a.stopScheduler:
				return
			}
		}
	}()
}

func (a *App) maybeRunScheduledSync(now time.Time) {
	// 如果到点且今天还没跑过，就触发一次同步。
	a.settingsMutex.Lock()
	if _, err := a.refreshSettingsLocked(); err != nil {
		a.logSettingsRefreshError("scheduled_sync_refresh_failed", err)
		a.settingsMutex.Unlock()
		return
	}
	settings := a.settings
	a.settingsMutex.Unlock()

	if !settings.ScheduleEnabled {
		return
	}

	scheduledAt, ok := scheduledTimeForDate(settings.DailyTime, now)
	if !ok || now.Before(scheduledAt) {
		return
	}
	today := now.Format("2006-01-02")
	if settings.LastRunDate == today {
		return
	}

	if err := a.RunSyncNow(); err != nil {
		if a.tray != nil {
			a.tray.ShowError("FeedMeDaily Tray", "Scheduled sync failed: "+err.Error())
		}
		return
	}

	a.settingsMutex.Lock()
	if _, err := a.refreshSettingsLocked(); err != nil {
		a.logSettingsRefreshError("scheduled_sync_save_refresh_failed", err)
		a.settingsMutex.Unlock()
		return
	}
	a.settings.LastRunDate = today
	_ = a.persistSettingsLocked()
	a.settingsMutex.Unlock()

	if a.tray != nil {
		a.tray.ShowInfo("FeedMeDaily Tray", "Scheduled sync started.")
	}
}

func scheduledTimeForDate(dailyTime string, now time.Time) (time.Time, bool) {
	// 把 HH:MM 转成“今天的这个时刻”。
	normalized := appruntime.NormalizeDailyTime(dailyTime)
	if normalized == "" {
		return time.Time{}, false
	}
	parts := strings.Split(normalized, ":")
	hour := mustAtoi(parts[0])
	minute := mustAtoi(parts[1])
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()), true
}

func mustAtoi(value string) int {
	// 在前面已经做过格式校验的前提下，把字符串转成整数。
	result, _ := strconv.Atoi(value)
	return result
}

func (a *App) snapshotSettings() TraySettings {
	// 复制一份托盘设置，避免调用方直接持有锁。
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	return a.settings
}

func (a *App) persistSettingsLocked() error {
	// 在调用方已经持锁的前提下保存设置。
	return a.settings.Save(a.layout.TraySettingsPath)
}

func (a *App) refreshSettingsFromDisk(action string) {
	a.settingsMutex.Lock()
	changed, err := a.refreshSettingsLocked()
	a.settingsMutex.Unlock()
	if err != nil {
		a.logSettingsRefreshError(action, err)
		return
	}
	if changed && a.tray != nil {
		a.tray.requestRefreshIcon(false)
	}
}

func (a *App) refreshSettingsLocked() (bool, error) {
	// 在调用方已经持锁的前提下，从共享 tray-settings.json 重新加载最新设置。
	settings, err := LoadTraySettings(a.layout.TraySettingsPath)
	if err != nil {
		return false, err
	}
	changed := settings != a.settings
	a.settings = settings
	return changed, nil
}

func (a *App) logSettingsRefreshError(action string, err error) {
	_, _ = logging.Write(a.layout.LogsDir, logging.Event{
		Level:     "error",
		Component: "tray",
		Action:    action,
		Message:   "Refreshing tray settings from disk failed.",
		Error:     err.Error(),
	})
}

func (a *App) currentMenuStateLocked() trayMenuState {
	// 在调用方已经持锁的前提下构造菜单状态。
	return trayMenuState{
		ScheduleEnabled: a.settings.ScheduleEnabled,
		DailyTime:       a.settings.DailyTime,
		LaunchAtLogin:   a.settings.LaunchAtLogin,
	}
}
