//go:build windows

package trayapp

import (
	"errors"
	"fmt"
	"time"
)

const updateShutdownTimeout = 8 * time.Second

// IsRunningApp reports whether the installed FeedMeDaily instance is still active.
func IsRunningApp(root string) (bool, error) {
	// Resolve the same layout used by the running tray so the check targets its user data.
	layout, err := ResolveLayout(root)
	if err != nil {
		return false, fmt.Errorf("resolve app layout: %w", err)
	}
	return isRunningApp(layout)
}

func isRunningApp(layout Layout) (bool, error) {
	// Check the recorded backend PID and the config-scoped tray window independently.
	state, err := ReadRuntimeState(layout.RuntimeStatePath)
	if err != nil {
		return false, err
	}
	if state != nil {
		if processRunningCall(state.PID) {
			return true, nil
		}
		baseURL := fmt.Sprintf("http://%s:%d", layout.ServerHost, state.Port)
		if WaitForHealthcheck(baseURL+"/api/app/health", 1200*time.Millisecond) {
			return true, nil
		}
	}
	return findTrayWindowCall(layout.ConfigDir) != 0, nil
}

// ShutdownRunningApp stops the installed FeedMeDaily processes before an update.
func ShutdownRunningApp(root string) error {
	// Resolve the same layout used by the running tray so the shutdown request targets its user data.
	layout, err := ResolveLayout(root)
	if err != nil {
		return fmt.Errorf("resolve app layout: %w", err)
	}
	return shutdownRunningApp(layout)
}

func shutdownRunningApp(layout Layout) error {
	// Stop the HTTP service first because the tray's close message alone cannot stop its child process.
	serviceErr := stopService(layout)
	trayErr := requestTrayQuitForUpdate(layout.ConfigDir)
	if serviceErr != nil && trayErr != nil {
		return fmt.Errorf("stop service: %v; close tray: %w", serviceErr, trayErr)
	}
	if serviceErr != nil {
		return fmt.Errorf("stop service: %w", serviceErr)
	}
	return trayErr
}

func requestTrayQuitForUpdate(configDir string) error {
	// Ask the existing hidden tray window to exit, then wait until its message loop is gone.
	hwnd := findTrayWindowCall(configDir)
	if hwnd == 0 {
		return nil
	}
	if !postMessageCall(hwnd, wmClose, 0, 0) && findTrayWindowCall(configDir) != 0 {
		return errors.New("post tray close message failed")
	}

	deadline := time.Now().Add(updateShutdownTimeout)
	for time.Now().Before(deadline) {
		if findTrayWindowCall(configDir) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("tray did not exit before update timeout")
}
