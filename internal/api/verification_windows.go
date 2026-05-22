//go:build windows

package api

import (
	"os/exec"
	"syscall"
)

func isWindowsRuntime() bool {
	return true
}

func hideVerificationLauncherWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
