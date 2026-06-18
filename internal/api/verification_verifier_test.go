package api

import (
	"path/filepath"
	"reflect"
	"sync"
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

func TestGroupVerificationRequestsGroupsFeedsByHost(t *testing.T) {
	grouped := groupVerificationRequests([]feeds.VerificationRequest{
		{URL: "https://chemrxiv.org/action/showFeed?type=latest&format=rss"},
		{URL: "https://chemrxiv.org/action/showFeed?type=current&format=rss"},
		{URL: "https://example.com/feed.xml"},
	})
	if len(grouped) != 2 {
		t.Fatalf("len(grouped) = %d", len(grouped))
	}
}

func TestBeginVerifierProcessStartBlocksDuplicateActiveRequest(t *testing.T) {
	verifierProcesses = struct {
		mu    sync.Mutex
		items map[string]*verifierProcess
	}{
		items: map[string]*verifierProcess{},
	}

	started, existing := beginVerifierProcessStart("verify-1")
	if !started || existing != nil {
		t.Fatalf("first begin = %v %#v", started, existing)
	}
	finishVerifierProcessStart(&verifierProcess{VerificationID: "verify-1", PID: 42})

	started, existing = beginVerifierProcessStart("verify-1")
	if started || existing == nil || existing.PID != 42 {
		t.Fatalf("second begin = %v %#v", started, existing)
	}
}

func TestNewVerifierCommandKeepsWindowVisible(t *testing.T) {
	cmd := newVerifierCommand("FeedMeDailyProtectedVerifier.exe", []string{"--verification-id", "verify-1"})
	if cmd.SysProcAttr != nil {
		t.Fatalf("verifier command should not hide the visible verification window: %#v", cmd.SysProcAttr)
	}
}

func TestProtectedFeedVerificationBuildArgsUseGoNativeHelper(t *testing.T) {
	binaryPath := filepath.Join("build", "FeedMeDailyProtectedVerifier", "FeedMeDailyProtectedVerifier.exe")
	args := protectedFeedVerificationBuildArgs(binaryPath, "0.3.3")
	expected := []string{
		"build",
		"-tags", "production",
		"-ldflags", "-H=windowsgui -X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=0.3.3",
		"-o", binaryPath,
		".\\cmd\\feedmedaily-protected-verifier",
	}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("args = %#v, want %#v", args, expected)
	}
}
