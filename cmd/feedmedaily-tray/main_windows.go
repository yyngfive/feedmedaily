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
	defaultRoot, err := detectDefaultRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to resolve default root:", err)
		os.Exit(1)
	}

	root := flag.String("root", defaultRoot, "Project root or installed app directory.")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to resolve root:", err)
		os.Exit(1)
	}

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
	_, pyprojectErr := os.Stat(filepath.Join(path, "pyproject.toml"))
	_, srcErr := os.Stat(filepath.Join(path, "src", "scirssagent"))
	return pyprojectErr == nil && srcErr == nil
}
