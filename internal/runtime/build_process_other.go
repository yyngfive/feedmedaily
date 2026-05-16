//go:build !windows

package appruntime

import "syscall"

func hiddenBuildSysProcAttr() *syscall.SysProcAttr {
	return nil
}
