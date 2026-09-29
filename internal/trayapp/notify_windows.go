//go:build windows

package trayapp

import "fmt"

func OpenRunningApp(configDir string) (bool, error) {
	// 桌面快捷方式复用当前用户的托盘窗口，不再启动第二个托盘。
	hwnd := findTrayWindowCall(configDir)
	if hwnd == 0 {
		return false, nil
	}
	if !postMessageCall(hwnd, trayMsgOpenApp, 0, 0) {
		return true, fmt.Errorf("post tray open message failed")
	}
	return true, nil
}

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
