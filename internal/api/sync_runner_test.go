package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
)

// 验证窗口在启动宽限期内就抓完并投递 XML 时，startVerificationFlow 仍可能报错
// （见 verifierStartFailure 的快速退出路径）。此时必须按已投递的结果继续同步，
// 否则抓到的 XML 会被一起丢掉，该源在滚动窗口里就永久缺文章。
func TestVerifyFeedHostKeepsDeliveredXMLWhenStartReportsFailure(t *testing.T) {
	previousStart := startVerificationFlowFunc
	defer func() { startVerificationFlowFunc = previousStart }()

	settings := testSettings(t.TempDir())
	feedURL := "https://pubs.acs.org/action/showFeed?type=axatoc&feed=rss&jc=jacsat"
	feedXML := "<rss><channel><title>JACS</title><item><title>One</title><link>https://example.com/1</link></item></channel></rss>"
	startVerificationFlowFunc = func(_ config.Settings, pending *pendingVerification) error {
		if pending == nil {
			t.Fatal("expected a pending verification")
		}
		if _, err := processVerificationCallback(settings, verificationCallbackPayload{
			VerificationID:   pending.ID,
			VerificationHost: pending.Host,
			FeedURL:          feedURL,
			Status:           "success",
			ContentType:      "application/xml",
			FeedXML:          feedXML,
		}); err != nil {
			t.Fatal(err)
		}
		return errors.New("open verification browser: the verification window exited before RSS XML was captured")
	}

	result := verifyFeedHost(settings, "job-delivered", "http://127.0.0.1:8000/api/feeds/verification/callback", []feeds.VerificationRequest{{
		URL: feedURL, Target: "cloudflare", Reason: "challenge", Journal: "JACS",
	}}, verificationAwareSyncCallbacks{})

	if strings.TrimSpace(result.Warning) != "" {
		t.Fatalf("delivered XML must not be reported as a verification warning: %s", result.Warning)
	}
	if got := string(result.FeedBodies[feedURL]); got != feedXML {
		t.Fatalf("verified feed body was dropped: %#v", result.FeedBodies)
	}
}
