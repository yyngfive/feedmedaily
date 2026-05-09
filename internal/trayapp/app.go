package trayapp

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type App struct {
	layout        Layout
	settings      TraySettings
	settingsMutex sync.Mutex
	tray          *windowsTray
	stopScheduler chan struct{}
}

func NewApp(cfg AppConfig) (*App, error) {
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
	tray, err := newWindowsTray(a)
	if err != nil {
		return err
	}
	a.tray = tray
	a.startSchedulerLoop()
	defer close(a.stopScheduler)
	return tray.Run()
}

func (a *App) MenuState() trayMenuState {
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	return trayMenuState{
		ScheduleEnabled: a.settings.ScheduleEnabled,
		DailyTime:       a.settings.DailyTime,
		LaunchAtLogin:   a.settings.LaunchAtLogin,
	}
}

func (a *App) OpenApp() error {
	baseURL, err := ensureService(a.layout, a.snapshotSettings())
	if err != nil {
		return err
	}
	return OpenBrowser(baseURL)
}

func (a *App) StartService() error {
	_, err := ensureService(a.layout, a.snapshotSettings())
	return err
}

func (a *App) StopService() error {
	return stopService(a.layout)
}

func (a *App) RunSyncNow() error {
	baseURL, err := ensureService(a.layout, a.snapshotSettings())
	if err != nil {
		return err
	}
	if err := triggerSync(baseURL); err != nil {
		return err
	}
	return nil
}

func (a *App) ToggleScheduleEnabled() (trayMenuState, error) {
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	a.settings.ScheduleEnabled = !a.settings.ScheduleEnabled
	if err := a.persistSettingsLocked(); err != nil {
		return trayMenuState{}, err
	}
	return a.currentMenuStateLocked(), nil
}

func (a *App) ToggleLaunchAtLogin() (trayMenuState, error) {
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
	return a.currentMenuStateLocked(), nil
}

func (a *App) OpenTraySettings() error {
	if _, err := os.Stat(a.layout.TraySettingsPath); err != nil && os.IsNotExist(err) {
		if err := a.snapshotSettings().Save(a.layout.TraySettingsPath); err != nil {
			return err
		}
	}
	cmd := exec.Command("notepad.exe", a.layout.TraySettingsPath)
	cmd.SysProcAttr = hiddenSysProcAttr()
	return cmd.Start()
}

func (a *App) OpenDataDir() error {
	return OpenPath(a.layout.DataDir)
}

func (a *App) OpenLogsDir() error {
	return OpenPath(a.layout.LogsDir)
}

func (a *App) SchedulerLabel() string {
	state := a.MenuState()
	if state.ScheduleEnabled {
		return fmt.Sprintf("Disable Daily Sync (%s)", state.DailyTime)
	}
	return fmt.Sprintf("Enable Daily Sync (%s)", state.DailyTime)
}

func (a *App) startSchedulerLoop() {
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
	a.settingsMutex.Lock()
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
	a.settings.LastRunDate = today
	_ = a.persistSettingsLocked()
	a.settingsMutex.Unlock()

	if a.tray != nil {
		a.tray.ShowInfo("FeedMeDaily Tray", "Scheduled sync started.")
	}
}

func scheduledTimeForDate(dailyTime string, now time.Time) (time.Time, bool) {
	normalized := normalizeDailyTime(dailyTime)
	if normalized == "" {
		return time.Time{}, false
	}
	parts := strings.Split(normalized, ":")
	hour := mustAtoi(parts[0])
	minute := mustAtoi(parts[1])
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()), true
}

func mustAtoi(value string) int {
	result, _ := strconv.Atoi(value)
	return result
}

func (a *App) snapshotSettings() TraySettings {
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	return a.settings
}

func (a *App) persistSettingsLocked() error {
	return a.settings.Save(a.layout.TraySettingsPath)
}

func (a *App) currentMenuStateLocked() trayMenuState {
	return trayMenuState{
		ScheduleEnabled: a.settings.ScheduleEnabled,
		DailyTime:       a.settings.DailyTime,
		LaunchAtLogin:   a.settings.LaunchAtLogin,
	}
}
