//go:build windows

package appruntime

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTrayLaunchCommandReleaseUsesPackagedTray(t *testing.T) {
	root := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "FeedMeDailyTray.exe"))

	command, cwd, err := trayLaunchCommand(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, "FeedMeDailyTray.exe"),
		"--root",
		root,
	}
	if cwd != root || !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v cwd=%q", command, cwd)
	}
}

func TestTrayLaunchCommandSourceFallsBackToGoRun(t *testing.T) {
	restore := replaceTrayLookPath(func(name string) (string, error) {
		if name == "go" {
			return `C:\Go\bin\go.exe`, nil
		}
		return "", errors.New("not found")
	})
	defer restore()

	root := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "pyproject.toml"))
	writeRuntimeTestFile(t, filepath.Join(root, "src", "scirssagent", "__init__.py"))

	command, cwd, err := trayLaunchCommand(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`C:\Go\bin\go.exe`,
		"run",
		"./cmd/feedmedaily-tray",
		"--root",
		root,
	}
	if cwd != root || !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v cwd=%q", command, cwd)
	}
}

func TestTrayLaunchCommandErrorsWhenNothingCanLaunch(t *testing.T) {
	restore := replaceTrayLookPath(func(string) (string, error) {
		return "", errors.New("not found")
	})
	defer restore()

	root := t.TempDir()
	_, _, err := trayLaunchCommand(root)
	if err == nil || !strings.Contains(err.Error(), "tray launcher not found") {
		t.Fatalf("err = %v", err)
	}
}

func writeRuntimeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceTrayLookPath(next func(string) (string, error)) func() {
	previous := trayLookPath
	trayLookPath = next
	return func() {
		trayLookPath = previous
	}
}
