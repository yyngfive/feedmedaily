//go:build !windows

package appruntime

func IsTrayRunning(root string) bool {
	// 非 Windows 平台当前没有托盘实现。
	return false
}

func EnsureTrayRunning(root string) error {
	// 非 Windows 平台当前不做托盘自动拉起。
	return nil
}
