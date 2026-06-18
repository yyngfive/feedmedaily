package main

import (
	"encoding/json"
	"testing"
)

func TestParseOptionsDeduplicatesFeedURLs(t *testing.T) {
	opts, err := parseOptions([]string{
		"--verification-id", "verify-1",
		"--job-id", "job-1",
		"--verification-host", "pubs.acs.org",
		"--callback-url", "http://127.0.0.1:8000/api/feeds/verification/callback",
		"--user-data-dir", "data/profile",
		"--logs-dir", "logs",
		"--app-version", "0.3.3",
		"--feed-url", "https://pubs.acs.org/feed-a",
		"--feed-url", "https://pubs.acs.org/feed-a",
		"--feed-url", "https://pubs.acs.org/feed-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if opts.VerificationID != "verify-1" || opts.VerificationHost != "pubs.acs.org" {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if len(opts.FeedURLs) != 2 {
		t.Fatalf("feed urls = %#v", opts.FeedURLs)
	}
}

func TestParseOptionsRequiresCoreFields(t *testing.T) {
	if _, err := parseOptions([]string{"--verification-id", "verify-1"}); err == nil {
		t.Fatal("expected missing fields to fail")
	}
}

func TestLooksLikeXML(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		want        bool
	}{
		{name: "xml content type", contentType: "application/xml;charset=UTF-8", body: "<html></html>", want: true},
		{name: "rss prefix", contentType: "text/plain", body: " \n<rss><channel></channel></rss>", want: true},
		{name: "atom prefix", contentType: "text/plain", body: "<feed></feed>", want: true},
		{name: "html", contentType: "text/html", body: "<html></html>", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeXML(tc.contentType, tc.body); got != tc.want {
				t.Fatalf("looksLikeXML() = %v", got)
			}
		})
	}
}

func TestLooksLikeChallenge(t *testing.T) {
	if !looksLikeChallenge("text/html; charset=UTF-8", "<html>Just a moment __cf_chl_</html>") {
		t.Fatal("expected Cloudflare challenge")
	}
	if looksLikeChallenge("application/xml", "<rss></rss>") {
		t.Fatal("XML should not be challenge")
	}
}

func TestCallbackPayloadShape(t *testing.T) {
	payload := callbackPayload{
		VerificationID:   "verify-1",
		VerificationHost: "pubs.acs.org",
		FeedURL:          "https://pubs.acs.org/feed",
		Status:           "success",
		ContentType:      "application/xml",
		SessionVerified:  true,
		CapturedFeeds: []capturedFeed{{
			FeedURL:     "https://pubs.acs.org/feed",
			ContentType: "application/xml",
			FeedXML:     "<rss></rss>",
		}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"verification_id", "verification_host", "feed_url", "status", "content_type", "feed_xml", "error", "session_verified", "captured_feeds"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing callback key %q in %s", key, string(data))
		}
	}
}
