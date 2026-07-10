package feeds

import (
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchAllStopsAfterFirstCloudflare403VerificationRequest(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-Ray", "test-ray")
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()
	targetURL, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{
		Timeout: previousClient.Timeout,
		Transport: rewriteFeedTestTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}
	defer func() { fetchHTTPClient = previousClient }()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Cell","url":"https://www.cell.com/cell/current.rss"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.VerificationRequests) != 1 || result.VerificationRequests[0].Target != "cloudflare" {
		t.Fatalf("unexpected verification requests: %#v", result.VerificationRequests)
	}
}

func TestFetchAllMarksChallengePageAsVerificationRequired(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>Enable JavaScript and cookies to continue</body></html>`))
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>ChemRxiv</title>
    <item>
      <title>Challenge retry sample</title>
      <link>https://chemrxiv.org/doi/full/10.26434/chemrxiv-2025-hssg2/v3?af=R</link>
      <description>Abstract text after retry.</description>
      <guid>doi:10.26434/chemrxiv-2025-hssg2/v3</guid>
      <pubDate>Thu, 21 May 2026 11:52:10 +0000</pubDate>
      <author>Alice Smith</author>
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Chemrxiv","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(result.VerificationRequests) != 1 || result.VerificationRequests[0].Target != "cloudflare" {
		t.Fatalf("unexpected verification requests: %#v", result.VerificationRequests)
	}
}

func TestFetchAllMarksCloudflareChallengeAsVerificationRequired(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>Enable JavaScript and cookies to continue</body></html>`))
	}))
	defer server.Close()
	targetURL, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{
		Timeout: previousClient.Timeout,
		Transport: rewriteFeedTestTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}
	defer func() { fetchHTTPClient = previousClient }()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Chemrxiv","url":"https://chemrxiv.org/action/showFeed?type=latest&format=rss"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.VerificationRequests) != 1 || result.VerificationRequests[0].Target != "cloudflare" {
		t.Fatalf("unexpected verification requests: %#v", result.VerificationRequests)
	}
}

func TestFetchAllVerifiesSameHostOnceAndContinues(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Host+r.URL.Path)
		switch r.Host + r.URL.Path {
		case "a.example/rss":
			writeTestRSS(w, "A Paper", "https://a.example/paper")
		case "pubs.acs.org/acs1":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>Enable JavaScript and cookies to continue</body></html>`))
		case "pubs.acs.org/acs2":
			t.Fatalf("same-host feed should be captured by verifier, not fetched directly")
		case "z.example/rss":
			writeTestRSS(w, "Z Paper", "https://z.example/paper")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	targetURL, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{
		Timeout: previousClient.Timeout,
		Transport: rewriteFeedTestTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}
	defer func() { fetchHTTPClient = previousClient }()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[
  {"journal":"Z","url":"https://z.example/rss"},
  {"journal":"ACS One","url":"https://pubs.acs.org/acs1"},
  {"journal":"A","url":"https://a.example/rss"},
  {"journal":"ACS Two","url":"https://pubs.acs.org/acs2"}
]`), 0o644); err != nil {
		t.Fatal(err)
	}

	progress := []string{}
	result, err := FetchAll(feedsPath, FetchOptions{
		BodyCache: map[string][]byte{},
		Progress: func(current int, total int, label string) {
			if current > 0 {
				progress = append(progress, label)
			}
		},
		VerifyHost: func(requests []VerificationRequest) VerificationResult {
			if len(requests) != 2 || requests[0].URL != "https://pubs.acs.org/acs1" || requests[1].URL != "https://pubs.acs.org/acs2" {
				t.Fatalf("verification requests = %#v", requests)
			}
			return VerificationResult{FeedBodies: map[string][]byte{
				"https://pubs.acs.org/acs1": []byte(testRSS("ACS One Paper", "https://pubs.acs.org/one")),
				"https://pubs.acs.org/acs2": []byte(testRSS("ACS Two Paper", "https://pubs.acs.org/two")),
			}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := []string{"a.example/rss", "pubs.acs.org/acs1", "z.example/rss"}
	if strings.Join(requested, "|") != strings.Join(wantRequests, "|") {
		t.Fatalf("requests = %#v, want %#v", requested, wantRequests)
	}
	wantProgress := []string{"A", "ACS One", "ACS Two", "Z"}
	if strings.Join(progress, "|") != strings.Join(wantProgress, "|") {
		t.Fatalf("progress = %#v, want %#v", progress, wantProgress)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Papers) != 4 {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

func TestFetchAllVerifiesOnlySelectedSameHostFeeds(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Host+r.URL.Path)
		switch r.Host + r.URL.Path {
		case "pubs.acs.org/acs1":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>Enable JavaScript and cookies to continue</body></html>`))
		case "pubs.acs.org/acs2":
			t.Fatalf("unselected same-host feed should not be fetched or verified")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	targetURL, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{
		Timeout: previousClient.Timeout,
		Transport: rewriteFeedTestTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}
	defer func() { fetchHTTPClient = previousClient }()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[
  {"journal":"ACS One","url":"https://pubs.acs.org/acs1"},
  {"journal":"ACS Two","url":"https://pubs.acs.org/acs2"}
]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{
		SelectedFeedURLs: []string{"https://pubs.acs.org/acs1"},
		VerifyHost: func(requests []VerificationRequest) VerificationResult {
			if len(requests) != 1 || requests[0].URL != "https://pubs.acs.org/acs1" {
				t.Fatalf("verification requests = %#v", requests)
			}
			return VerificationResult{FeedBodies: map[string][]byte{
				"https://pubs.acs.org/acs1": []byte(testRSS("ACS One Paper", "https://pubs.acs.org/one")),
			}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requested, "|") != "pubs.acs.org/acs1" {
		t.Fatalf("requests = %#v", requested)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "ACS One Paper" {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

func TestFetchAllSkipsSameHostAfterVerificationFailureAndContinues(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Host+r.URL.Path)
		switch r.Host + r.URL.Path {
		case "pubs.acs.org/acs1":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>Just a moment...</title></head><body>Enable JavaScript and cookies to continue</body></html>`))
		case "pubs.acs.org/acs2":
			t.Fatalf("same-host feed should be skipped after host verification fails")
		case "z.example/rss":
			writeTestRSS(w, "Z Paper", "https://z.example/paper")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	targetURL, err := neturl.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{
		Timeout: previousClient.Timeout,
		Transport: rewriteFeedTestTransport{
			target: targetURL,
			base:   http.DefaultTransport,
		},
	}
	defer func() { fetchHTTPClient = previousClient }()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[
  {"journal":"ACS One","url":"https://pubs.acs.org/acs1"},
  {"journal":"ACS Two","url":"https://pubs.acs.org/acs2"},
  {"journal":"Z","url":"https://z.example/rss"}
]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{
		VerifyHost: func(requests []VerificationRequest) VerificationResult {
			if len(requests) != 2 {
				t.Fatalf("verification requests = %#v", requests)
			}
			return VerificationResult{Warning: "verification timed out"}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := []string{"pubs.acs.org/acs1", "z.example/rss"}
	if strings.Join(requested, "|") != strings.Join(wantRequests, "|") {
		t.Fatalf("requests = %#v, want %#v", requested, wantRequests)
	}
	if len(result.Errors) != 2 || !strings.Contains(result.Errors[0], "verification timed out") || !strings.Contains(result.Errors[1], "verification timed out") {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "Z Paper" {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

type rewriteFeedTestTransport struct {
	target *neturl.URL
	base   http.RoundTripper
}
