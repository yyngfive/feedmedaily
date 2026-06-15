//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

func TestParseArgsKeepsProvidedPersistentUserDataDir(t *testing.T) {
	root := t.TempDir()
	profileDir := filepath.Join(root, "profiles", "pubs.acs.org")
	cfg, err := parseArgs([]string{
		"--verification-id", "v1",
		"--job-id", "job-1",
		"--feed-url", "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat",
		"--callback-url", "http://127.0.0.1:8000/api/feeds/verification/callback",
		"--user-data-dir", profileDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserDataDir != profileDir {
		t.Fatalf("user data dir = %q", cfg.UserDataDir)
	}
	if cfg.CleanupProfile {
		t.Fatal("expected persistent profile dir to survive shutdown")
	}
}
