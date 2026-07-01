package trayapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMenuStateRefreshesSchedulerSettingsFromDisk(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	writeTraySettings(t, settingsPath, TraySettings{
		ScheduleEnabled: true,
		DailyTime:       "08:15",
		LaunchAtLogin:   false,
	})

	state := app.MenuState()
	if !state.ScheduleEnabled || state.DailyTime != "08:15" {
		t.Fatalf("state = %#v", state)
	}
}

func TestBackgroundRefreshUpdatesSchedulerSettingsFromDisk(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	writeTraySettings(t, settingsPath, TraySettings{
		ScheduleEnabled: true,
		DailyTime:       "08:15",
		LaunchAtLogin:   false,
	})

	app.refreshSettingsFromDisk("test_refresh_failed")
	state := app.currentMenuStateForTest()
	if !state.ScheduleEnabled || state.DailyTime != "08:15" {
		t.Fatalf("state = %#v", state)
	}
}

func (a *App) currentMenuStateForTest() trayMenuState {
	a.settingsMutex.Lock()
	defer a.settingsMutex.Unlock()
	return a.currentMenuStateLocked()
}

func TestToggleScheduleEnabledPreservesDiskDailyTime(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	writeTraySettings(t, settingsPath, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "08:15",
		LaunchAtLogin:   false,
	})

	state, err := app.ToggleScheduleEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if !state.ScheduleEnabled || state.DailyTime != "08:15" {
		t.Fatalf("state = %#v", state)
	}
	saved, err := LoadTraySettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.ScheduleEnabled || saved.DailyTime != "08:15" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestToggleScheduleEnabledDoesNotOverwriteInvalidDiskSettings(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: false,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	if err := os.WriteFile(settingsPath, []byte(`{"daily_time":`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := app.ToggleScheduleEnabled()
	if err == nil || !strings.Contains(err.Error(), "parse tray scheduler settings") {
		t.Fatalf("toggle error = %v", err)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"daily_time":` {
		t.Fatalf("invalid settings were overwritten: %s", data)
	}
}

func TestMaybeRunScheduledSyncUsesLatestDiskTime(t *testing.T) {
	app, settingsPath := testSchedulerApp(t, TraySettings{
		ScheduleEnabled: true,
		DailyTime:       "10:30",
		LaunchAtLogin:   false,
	})
	writeTraySettings(t, settingsPath, TraySettings{
		ScheduleEnabled: true,
		DailyTime:       "08:15",
		LaunchAtLogin:   false,
	})
	runCalls := 0
	app.runSyncNow = func() error {
		runCalls++
		return nil
	}

	app.maybeRunScheduledSync(time.Date(2026, 6, 25, 8, 20, 0, 0, time.UTC))
	if runCalls != 1 {
		t.Fatalf("runCalls = %d", runCalls)
	}
	saved, err := LoadTraySettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LastRunDate != "2026-06-25" || saved.DailyTime != "08:15" {
		t.Fatalf("saved = %#v", saved)
	}
}

func testSchedulerApp(t *testing.T, memorySettings TraySettings) (*App, string) {
	t.Helper()
	root := t.TempDir()
	settingsPath := filepath.Join(root, "tray-settings.json")
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTraySettings(t, settingsPath, memorySettings)
	return &App{
		layout: Layout{
			ConfigDir:        root,
			LogsDir:          filepath.Join(root, "logs"),
			TraySettingsPath: settingsPath,
		},
		settings: memorySettings,
	}, settingsPath
}

func writeTraySettings(t *testing.T, path string, settings TraySettings) {
	t.Helper()
	if err := settings.Save(path); err != nil {
		t.Fatal(err)
	}
}
