//go:build windows

package trayapp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const autostartValueName = "FeedMeDailyTray"

func autostartCommandLine(rootDir string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("\"%s\" --root \"%s\"", executable, rootDir), nil
}

func isAutostartEnabled(rootDir string) (bool, error) {
	expected, err := autostartCommandLine(rootDir)
	if err != nil {
		return false, err
	}

	cmd := exec.Command(
		"reg",
		"query",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v",
		autostartValueName,
	)
	cmd.SysProcAttr = hiddenSysProcAttr()
	output, err := cmd.CombinedOutput()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("query autostart registry: %w", err)
	}

	return strings.Contains(string(output), expected), nil
}

func setAutostartEnabled(rootDir string, enabled bool) error {
	if !enabled {
		cmd := exec.Command(
			"reg",
			"delete",
			`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
			"/v",
			autostartValueName,
			"/f",
		)
		cmd.SysProcAttr = hiddenSysProcAttr()
		_, err := cmd.CombinedOutput()
		if err != nil {
			exitErr, ok := err.(*exec.ExitError)
			if ok && exitErr.ExitCode() == 1 {
				return nil
			}
			return fmt.Errorf("disable autostart: %w", err)
		}
		return nil
	}

	commandLine, err := autostartCommandLine(rootDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(
		"reg",
		"add",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		"/v",
		autostartValueName,
		"/t",
		"REG_SZ",
		"/d",
		commandLine,
		"/f",
	)
	cmd.SysProcAttr = hiddenSysProcAttr()
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("enable autostart: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
