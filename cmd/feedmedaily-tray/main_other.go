//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	// 当前托盘实现直接绑定 Windows API，其他平台先明确退出。
	fmt.Fprintln(os.Stderr, "feedmedaily-tray is currently supported on Windows only")
	os.Exit(1)
}
