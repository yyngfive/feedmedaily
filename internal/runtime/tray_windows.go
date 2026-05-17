//go:build windows

package appruntime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const TrayMutexName = "FeedMeDailyTrayMutex"

const (
	trayDetachedProcess       = 0x00000008
	trayCreateNewProcessGroup = 0x00000200
	synchronizeAccess         = 0x00100000
)

var (
	kernel32Tray        = syscall.NewLazyDLL("kernel32.dll")
	procOpenMutexW      = kernel32Tray.NewProc("OpenMutexW")
	procCloseHandleTray = kernel32Tray.NewProc("CloseHandle")
	ensureTrayBinary    = EnsureSourceBinary
)

func IsTrayRunning() bool {
	// 通过和托盘相同的命名互斥锁判断托盘是否已经在运行。
	namePtr, err := syscall.UTF16PtrFromString(TrayMutexName)
	if err != nil {
		return false
	}
	handle, _, _ := procOpenMutexW.Call(synchronizeAccess, 0, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		return false
	}
	procCloseHandleTray.Call(handle)
	return true
}

func EnsureTrayRunning(root string) error {
	// 如果托盘未运行，则在后台拉起一个新的托盘实例。
	if IsTrayRunning() {
		return nil
	}
	command, cwd, err := trayLaunchCommand(root)
	if err != nil {
		return err
	}
	return launchTrayProcess(command, cwd)
}

func trayLaunchCommand(root string) ([]string, string, error) {
	// 发布模式优先找打包好的托盘 exe；源码模式则复用本地缓存二进制。
	candidates := []string{
		filepath.Join(root, "FeedMeDailyTray.exe"),
		filepath.Join(root, "build", "feedmedaily-tray.exe"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return []string{candidate, "--root", root}, root, nil
		}
	}

	if LooksLikeSourceRoot(root) {
		binaryPath, err := ensureTrayBinary(root, "./cmd/feedmedaily-tray", "feedmedaily-tray.exe")
		if err == nil {
			return []string{binaryPath, "--root", root}, root, nil
		}
	}

	return nil, "", fmt.Errorf("tray launcher not found for root %s", root)
}

func launchTrayProcess(command []string, cwd string) error {
	// 用无窗口后台进程方式启动托盘，避免服务把终端带出来。
	if len(command) == 0 {
		return fmt.Errorf("tray command is empty")
	}
	stdinFile, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer stdinFile.Close()

	stdoutFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer stdoutFile.Close()

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = stdoutFile
	cmd.Stderr = stdoutFile
	cmd.Stdin = stdinFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: trayDetachedProcess | trayCreateNewProcessGroup,
	}
	return cmd.Start()
}
