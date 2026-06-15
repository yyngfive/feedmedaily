package appruntime

import (
	"path/filepath"
	"testing"
)

func TestOpenWithShellCommandWindowsPreservesFullURL(t *testing.T) {
	target := "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat"
	cmd := openWithShellCommand("windows", target)
	if filepath.Base(cmd.Path) != "rundll32.exe" {
		t.Fatalf("command path = %q", cmd.Path)
	}
	if len(cmd.Args) != 3 {
		t.Fatalf("args = %#v", cmd.Args)
	}
	if cmd.Args[1] != "url.dll,FileProtocolHandler" || cmd.Args[2] != target {
		t.Fatalf("args = %#v", cmd.Args)
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
