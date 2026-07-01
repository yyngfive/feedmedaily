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

func TestTrayInstanceIDIsConfigScoped(t *testing.T) {
	configA := filepath.Join(t.TempDir(), "config")
	configB := filepath.Join(t.TempDir(), "config")
	if TrayInstanceID(configA) != TrayInstanceID(configA) {
		t.Fatal("same config dir produced different tray instance ids")
	}
	if TrayInstanceID(configA) == TrayInstanceID(configB) {
		t.Fatal("different config dirs produced the same tray instance id")
	}
	if !strings.HasPrefix(TrayMutexName(configA), TrayInstanceID(configA)) {
		t.Fatalf("mutex name %q does not include instance id %q", TrayMutexName(configA), TrayInstanceID(configA))
	}
}

func TestEnsureTrayRunningIgnoresOtherConfigDirInstance(t *testing.T) {
	root := t.TempDir()
	otherRoot := t.TempDir()
	writeRuntimeTestFile(t, filepath.Join(root, "FeedMeDailyTray.exe"))
	writeRuntimeTestFile(t, filepath.Join(otherRoot, "go.mod"))
	otherConfig, err := ConfigDirForRoot(otherRoot)
	if err != nil {
		t.Fatal(err)
	}

	checkedConfigDirs := []string{}
	restoreRunning := replaceIsTrayRunningForConfigDir(func(configDir string) bool {
		checkedConfigDirs = append(checkedConfigDirs, configDir)
		return configDir == otherConfig
	})
	defer restoreRunning()

	var launchedCommand []string
	restoreLaunch := replaceLaunchTrayProcess(func(command []string, cwd string) error {
		launchedCommand = append([]string(nil), command...)
		return nil
	})
	defer restoreLaunch()

	if err := EnsureTrayRunning(root); err != nil {
		t.Fatal(err)
	}
	if len(checkedConfigDirs) != 1 || checkedConfigDirs[0] == otherConfig {
		t.Fatalf("checked config dirs = %#v, other=%q", checkedConfigDirs, otherConfig)
	}
	if len(launchedCommand) == 0 {
		t.Fatal("expected current root tray to launch")
	}
}

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

func replaceIsTrayRunningForConfigDir(next func(string) bool) func() {
	previous := isTrayRunningForConfigDir
	isTrayRunningForConfigDir = next
	return func() {
		isTrayRunningForConfigDir = previous
	}
}

func replaceLaunchTrayProcess(next func([]string, string) error) func() {
	previous := launchTrayProcessFunc
	launchTrayProcessFunc = next
	return func() {
		launchTrayProcessFunc = previous
	}
}
