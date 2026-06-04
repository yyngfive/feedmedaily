package trayapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLayoutSourceUsesDotEnvServerSettings(t *testing.T) {
	root := t.TempDir()
	writeTrayTestFile(t, filepath.Join(root, "go.mod"))
	writeTrayTestFile(t, filepath.Join(root, ".env"))
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SCIRSS_SERVER_HOST=127.0.0.2\nSCIRSS_SERVER_PORT=8123\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	layout, err := ResolveLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	if layout.Mode != runtimeModeSource {
		t.Fatalf("mode = %q", layout.Mode)
	}
	if layout.ServerHost != "127.0.0.2" {
		t.Fatalf("server host = %q", layout.ServerHost)
	}
	if layout.ServerPort != 8123 {
		t.Fatalf("server port = %d", layout.ServerPort)
	}
}
