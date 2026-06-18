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

func TestTrayLaunchCommandSourceUsesBuiltBinary(t *testing.T) {
	restore := replaceEnsureTrayBinary(func(root string, packagePath string, outputName string) (string, error) {
		return filepath.Join(root, ".tmp", "runtime-bin", outputName), nil
	})
	defer restore()
	root := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "go.mod"))

	command, cwd, err := trayLaunchCommand(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, ".tmp", "runtime-bin", "feedmedaily-tray.exe"),
		"--root",
		root,
	}
	if cwd != root || !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v cwd=%q", command, cwd)
	}
}

func TestTrayLaunchCommandErrorsWhenNothingCanLaunch(t *testing.T) {
	restore := replaceEnsureTrayBinary(func(string, string, string) (string, error) {
		return "", errors.New("not found")
	})
	defer restore()

	root := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "go.mod"))
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

func replaceEnsureTrayBinary(next func(string, string, string) (string, error)) func() {
	previous := ensureTrayBinary
	ensureTrayBinary = next
	return func() {
		ensureTrayBinary = previous
	}
}
