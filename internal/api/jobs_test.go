package api

import (
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

func TestBackendRunCommandLinuxSourceMode(t *testing.T) {
	settings := config.Settings{
		Mode:    appruntime.ModeSource,
		RootDir: "/tmp/feedmedaily",
	}
	command, err := backendRunCommandForPlatform(settings, "linux")
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"bash", filepath.Join(settings.RootDir, "tools", "feedmedaily.sh"), "sync"}
	if len(command) != len(expected) {
		t.Fatalf("command length = %d, want %d (%#v)", len(command), len(expected), command)
	}
	for index := range expected {
		if command[index] != expected[index] {
			t.Fatalf("command[%d] = %q, want %q", index, command[index], expected[index])
		}
	}
}

func TestBackendRunCommandUnsupportedPlatforms(t *testing.T) {
	settings := config.Settings{
		Mode:    appruntime.ModeSource,
		RootDir: "/tmp/feedmedaily",
	}
	command, err := backendRunCommandForPlatform(settings, goruntime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if goruntime.GOOS == "linux" {
		if len(command) == 0 {
			t.Fatalf("expected linux to return a helper command")
		}
		return
	}
	if len(command) != 0 {
		t.Fatalf("expected non-linux platform to omit helper command, got %#v", command)
	}
}

func TestSummarizeProfileProposalRejectedResult(t *testing.T) {
	message := summarizeResult("profile-proposal", map[string]any{
		"accepted":        false,
		"hard_rejected":   true,
		"summary":         "Removed key negative boundary.",
		"blocking_issues": []string{"Surface adjacency boundary was removed."},
		"required_fixes":  []string{"Preserve the boundary."},
	})
	if !strings.Contains(message, "Profile proposal rejected by safety review.") || !strings.Contains(message, "Removed key negative boundary.") {
		t.Fatalf("unexpected summary: %s", message)
	}
}
