//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	shutdown := flag.Bool("shutdown", false, "Stop an existing FeedMeDaily instance before an update.")
	dataRoot := flag.String("data-root", "", "User data directory used by the running instance.")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to resolve root:", err)
		os.Exit(1)
	}

	if *shutdown {
		if strings.TrimSpace(*dataRoot) != "" {
			absDataRoot, err := filepath.Abs(*dataRoot)
			if err != nil {
				fmt.Fprintln(os.Stderr, "failed to resolve data root:", err)
				os.Exit(1)
			}
			if err := os.Setenv("FEEDMEDAILY_DATA_ROOT", absDataRoot); err != nil {
				fmt.Fprintln(os.Stderr, "failed to set data root:", err)
				os.Exit(1)
			}
		}
		if err := trayapp.ShutdownRunningApp(absRoot); err != nil {
			fmt.Fprintln(os.Stderr, "failed to shut down FeedMeDaily:", err)
			os.Exit(1)
		}
		return
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
