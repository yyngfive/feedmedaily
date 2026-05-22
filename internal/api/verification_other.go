//go:build !windows

package api

import "os/exec"

func isWindowsRuntime() bool {
	return false
}

func hideVerificationLauncherWindow(_ *exec.Cmd) {}
