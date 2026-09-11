//go:build windows

package trayapp

import (
	"errors"
	"fmt"
	"time"
)

const updateShutdownTimeout = 8 * time.Second

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
