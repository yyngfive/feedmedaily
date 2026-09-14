package api

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

// 持久 profile 里的 Cloudflare 放行还有效时，验证窗口抓完 XML 就会退出，实测
// 500-600ms——比 900ms 的启动宽限期还短。这种"快速退出"是成功，不是启动失败。
func TestVerifierStartFailureAcceptsFastExitThatDeliveredXML(t *testing.T) {
	seedPendingVerificationDeliveryState(t, "verify-delivered", true, true)
	if err := verifierStartFailure("verify-delivered", 0, nil); err != nil {
		t.Fatalf("delivered XML must count as a successful start: %v", err)
	}
}

func TestVerifierStartFailureKeepsDeliveredXMLDespiteNonZeroExit(t *testing.T) {
	seedPendingVerificationDeliveryState(t, "verify-delivered-nonzero", false, true)
	if err := verifierStartFailure("verify-delivered-nonzero", 1, errors.New("exit status 1")); err != nil {
		t.Fatalf("a delivered body wins over a non-zero exit code: %v", err)
	}
}

// 没有投递 XML 才算启动失败，而且错误文案里不能出现 WebView2 的信息级日志。
func TestVerifierStartFailureReportsReadableReasonWithoutXML(t *testing.T) {
	seedPendingVerificationDeliveryState(t, "verify-empty", false, false)
	err := verifierStartFailure("verify-empty", 0, nil)
	if err == nil {
		t.Fatal("expected a start failure when no XML was delivered")
	}
	if !strings.Contains(err.Error(), "before RSS XML was captured") {
		t.Fatalf("unexpected message: %s", err.Error())
	}
	if strings.Contains(err.Error(), "WebView2") {
		t.Fatalf("WebView2 stderr must not become the user-facing reason: %s", err.Error())
	}
}

func seedPendingVerificationDeliveryState(t *testing.T, id string, callbackReceived bool, delivered bool) {
	t.Helper()
	apiVerifications.mu.Lock()
	previous, existed := apiVerifications.items[id]
	apiVerifications.items[id] = &pendingVerification{
		ID:               id,
		JobID:            "job-" + id,
		CallbackReceived: callbackReceived,
		Delivered:        delivered,
	}
	apiVerifications.mu.Unlock()
	t.Cleanup(func() {
		apiVerifications.mu.Lock()
		defer apiVerifications.mu.Unlock()
		if existed {
			apiVerifications.items[id] = previous
			return
		}
		delete(apiVerifications.items, id)
	})
}
