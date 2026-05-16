//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yyngfive/scirssagent/internal/trayapp"
)

func main() {
	// 先推断默认 root：源码模式下指向仓库根目录，发布模式下指向安装目录。
	defaultRoot, err := detectDefaultRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to resolve default root:", err)
		os.Exit(1)
	}

	// 托盘只需要 root；其余路径和设置都由内部布局解析完成。
	root := flag.String("root", defaultRoot, "Project root or installed app directory.")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to resolve root:", err)
		os.Exit(1)
	}

	// 托盘应用负责菜单、调度、自启动和后台服务控制。
	app, err := trayapp.NewApp(trayapp.AppConfig{RootDir: absRoot})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func detectDefaultRoot() (string, error) {
	// 从当前可执行文件位置反推出运行根目录。
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	execDir := filepath.Dir(executable)
	parentDir := filepath.Dir(execDir)

	if looksLikeSourceRoot(parentDir) && filepath.Base(execDir) == "build" {
		return parentDir, nil
	}
	return execDir, nil
}

func looksLikeSourceRoot(path string) bool {
	// 用仓库根目录标志作为“这是源码仓库”的判断，不要求 Python 源码目录仍然存在。
	_, pyprojectErr := os.Stat(filepath.Join(path, "pyproject.toml"))
	_, gomodErr := os.Stat(filepath.Join(path, "go.mod"))
	return pyprojectErr == nil || gomodErr == nil
}
