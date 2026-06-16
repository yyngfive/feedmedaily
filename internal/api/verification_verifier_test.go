package api

import (
	"path/filepath"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
)

func TestVerificationProfileHostUsesFeedHostname(t *testing.T) {
	host := verificationProfileHost("https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat")
	if host != "pubs.acs.org" {
		t.Fatalf("host = %q", host)
	}
}

func TestVerificationProfileHostFallsBackToDefault(t *testing.T) {
	host := verificationProfileHost("not a url")
	if host != "default" {
		t.Fatalf("host = %q", host)
	}
}

func TestVerificationUserDataDirUsesHostScopedPersistentPath(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{
		DataDir: filepath.Join(root, "data"),
	}
	path, err := verificationUserDataDir(settings, "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat")
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(settings.DataDir, "verification-profiles", "pubs.acs.org")
	if path != expected {
		t.Fatalf("path = %q, want %q", path, expected)
	}
}

func TestGroupVerificationRequestsGroupsACSFeedsByHost(t *testing.T) {
	grouped := groupVerificationRequests([]feeds.VerificationRequest{
		{URL: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat"},
		{URL: "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=ancham"},
		{URL: "https://example.com/feed.xml"},
	})
	if len(grouped) != 2 {
		t.Fatalf("len(grouped) = %d", len(grouped))
	}
}
