package trayapp

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBackendCommandReleaseUsesGoDaemon(t *testing.T) {
	root := t.TempDir()
	daemonPath := filepath.Join(root, "feedmedailyd.exe")
	writeTrayTestFile(t, daemonPath)

	layout := testLayout(root, runtimeModeRelease)
	command, err := backendCommand(layout, 8123)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		daemonPath,
		"--root",
		root,
		"--host",
		"127.0.0.1",
		"--port",
		"8123",
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v, want %#v", command, want)
	}
}

func TestBackendCommandReleaseDoesNotFallBackToPythonBundle(t *testing.T) {
	root := t.TempDir()
	writeTrayTestFile(t, filepath.Join(root, "FeedMeDaily.exe"))

	layout := testLayout(root, runtimeModeRelease)
	_, err := backendCommand(layout, 8123)
	if err == nil {
		t.Fatal("expected an error when only the legacy Python bundle exists")
	}
	if !strings.Contains(err.Error(), "feedmedailyd.exe") {
		t.Fatalf("error = %q", err)
	}
}

func TestBackendCommandSourceUsesGoRun(t *testing.T) {
	restore := replaceLookPath(func(name string) (string, error) {
		if name == "go" {
			return `C:\Go\bin\go.exe`, nil
		}
		return "", errors.New("not found")
	})
	defer restore()

	root := t.TempDir()
	layout := testLayout(root, runtimeModeSource)
	command, err := backendCommand(layout, 9001)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		`C:\Go\bin\go.exe`,
		"run",
		"./cmd/feedmedailyd",
		"--root",
		root,
		"--host",
		"127.0.0.1",
		"--port",
		"9001",
	}
	if !reflect.DeepEqual(command, want) {
		t.Fatalf("command = %#v, want %#v", command, want)
	}
}

func TestBackendCommandSourceRequiresGoToolchain(t *testing.T) {
	restore := replaceLookPath(func(string) (string, error) {
		return "", errors.New("not found")
	})
	defer restore()

	_, err := backendCommand(testLayout(t.TempDir(), runtimeModeSource), 9001)
	if err == nil {
		t.Fatal("expected an error when Go is unavailable")
	}
	if !strings.Contains(err.Error(), "go command not found") {
		t.Fatalf("error = %q", err)
	}
}

func testLayout(root string, mode string) Layout {
	return Layout{
		Mode:             mode,
		RootDir:          root,
		ConfigDir:        root,
		RuntimeStatePath: filepath.Join(root, "runtime.json"),
		ServerHost:       "127.0.0.1",
		ServerPort:       8000,
	}
}

func writeTrayTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replaceLookPath(next func(string) (string, error)) func() {
	previous := lookPath
	lookPath = next
	return func() {
		lookPath = previous
	}
}
