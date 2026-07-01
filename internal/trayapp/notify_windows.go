//go:build windows

package trayapp

import "fmt"

func NotifySettingsChanged(configDir string) error {
	// 通知正在运行的托盘窗口立刻重载 tray-settings.json。
	hwnd := findTrayWindowCall(configDir)
	if hwnd == 0 {
		return fmt.Errorf("tray window not found for config dir %s", configDir)
	}
	if !postMessageCall(hwnd, trayMsgReloadSetting, 0, 0) {
		return fmt.Errorf("post tray settings reload message failed")
	}
	return nil
}
