//go:build !windows

package trayapp

func NotifySettingsChanged() error {
	// 非 Windows 平台当前没有托盘消息窗口，保存设置即可。
	return nil
}
