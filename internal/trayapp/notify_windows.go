//go:build windows

package trayapp

import "fmt"

func NotifySettingsChanged() error {
	// 通知正在运行的托盘窗口立刻重载 tray-settings.json。
	hwnd := findTrayWindowCall()
	if hwnd == 0 {
		return nil
	}
	if !postMessageCall(hwnd, trayMsgReloadSetting, 0, 0) {
		return fmt.Errorf("post tray settings reload message failed")
	}
	return nil
}
