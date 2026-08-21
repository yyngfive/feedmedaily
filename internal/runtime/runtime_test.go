package appruntime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultTraySchedulerSettingsUsesOffPeakDailyTime(t *testing.T) {
	settings := DefaultTraySchedulerSettings()
	if settings.DailyTime != "12:30" {
		t.Fatalf("daily time = %q, want 12:30", settings.DailyTime)
	}
}

func TestOpenExternalTargetRejectsMissingLocalPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	err := OpenExternalTarget(missing)
	if err == nil || !strings.Contains(err.Error(), "local open target does not exist") {
		t.Fatalf("missing path error = %v", err)
	}
}

func TestOpenExternalTargetRejectsBlankTarget(t *testing.T) {
	err := OpenExternalTarget("  ")
	if err == nil || !strings.Contains(err.Error(), "open target cannot be blank") {
		t.Fatalf("blank target error = %v", err)
	}
}

func TestOpenWithShellCommandDarwin(t *testing.T) {
	cmd := openWithShellCommand("darwin", "https://example.com/feed.xml")
	if cmd.Path != "open" {
		t.Fatalf("command path = %q", cmd.Path)
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "https://example.com/feed.xml" {
		t.Fatalf("args = %#v", cmd.Args)
	}
}

func TestOpenWithShellCommandLinux(t *testing.T) {
	cmd := openWithShellCommand("linux", "/tmp/feed.xml")
	if cmd.Path != "xdg-open" {
		t.Fatalf("command path = %q", cmd.Path)
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "/tmp/feed.xml" {
		t.Fatalf("args = %#v", cmd.Args)
	}
}
