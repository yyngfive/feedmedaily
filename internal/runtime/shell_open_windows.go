//go:build windows

package appruntime

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	runtimeShell32          = syscall.NewLazyDLL("shell32.dll")
	runtimeProcShellExecute = runtimeShell32.NewProc("ShellExecuteW")
	shellExecuteOpenFunc    = shellExecuteOpen
)

func openWithSystemShell(target string) error {
	return shellExecuteOpenFunc("open", target)
}

func shellExecuteOpen(verb string, target string) error {
	verbPtr, err := syscall.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := runtimeProcShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		0,
		0,
		1,
	)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed for %s: %v", target, callErr)
	}
	return nil
}
