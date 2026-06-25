//go:build !windows

package appruntime

import goruntime "runtime"

func openWithSystemShell(target string) error {
	cmd := openWithShellCommand(goruntime.GOOS, target)
	return cmd.Start()
}
