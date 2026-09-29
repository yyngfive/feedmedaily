package feeds

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchAllUsesBrowserLikeUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("User-Agent"), "Mozilla/5.0") || !strings.Contains(r.Header.Get("User-Agent"), "SciRSSAgent/0.1") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>ChemRxiv</title>
    <item>
      <title>Browser-like UA sample</title>
      <link>https://example.com/browser-ua</link>
      <guid>doi:10.1000/browser-ua</guid>
      <description>Abstract text after browser-style user agent.</description>
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
	if len(result.Errors) != 0 || len(result.Papers) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Papers[0].Title != "Browser-like UA sample" {
		t.Fatalf("unexpected paper: %#v", result.Papers[0])
	}
}

func TestFetchAllStopsAfterRetryableFailuresAndContinues(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0, 0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	brokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/broken":
			brokenRequests++
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		case "/rss":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Nature</title>
    <item>
      <title>Healthy feed sample</title>
      <link>https://example.com/healthy</link>
      <guid>doi:10.1000/healthy</guid>
    </item>
  </channel>
</rss>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[
  {"journal":"Broken","url":"`+server.URL+`/broken"},
  {"journal":"Nature","url":"`+server.URL+`/rss"}
]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if brokenRequests != 3 {
		t.Fatalf("brokenRequests = %d", brokenRequests)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "/broken") {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "Healthy feed sample" {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

func (t rewriteFeedTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = request.URL.Host
	return t.base.RoundTrip(cloned)
}

type flakyDNSTransport struct {
	base        http.RoundTripper
	failForPath string
	remaining   int
	requests    int
}

func (t *flakyDNSTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == t.failForPath {
		t.requests++
		if t.remaining > 0 {
			t.remaining--
			return nil, &net.DNSError{Err: "no such host", Name: request.URL.Hostname(), IsNotFound: true}
		}
	}
	return t.base.RoundTrip(request)
}

func writeFetchTestFeeds(t *testing.T, entries string) string {
	t.Helper()
	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(entries), 0o644); err != nil {
		t.Fatal(err)
	}
	return feedsPath
}

func rssBodyFor(title string) []byte {
	return []byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Test journal</title>
    <item>
      <title>` + title + `</title>
      <link>https://example.com/` + title + `</link>
      <guid>doi:10.1000/` + title + `</guid>
    </item>
  </channel>
</rss>`)
}

func TestFetchAllRetriesTransientDNSFailuresAtEndOfRun(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	oldPassDelays := fetchRetryPassDelays
	oldClient := fetchHTTPClient
	fetchRetryBackoffs = []time.Duration{0, 0}
	fetchRetryPassDelays = []time.Duration{0, 0}
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second}
	defer func() {
		fetchRetryBackoffs = oldBackoffs
		fetchRetryPassDelays = oldPassDelays
		fetchHTTPClient = oldClient
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		switch r.URL.Path {
		case "/flaky":
			_, _ = w.Write(rssBodyFor("Recovered feed sample"))
		default:
			_, _ = w.Write(rssBodyFor("Steady feed sample"))
		}
	}))
	defer server.Close()

	transport := &flakyDNSTransport{base: http.DefaultTransport, failForPath: "/flaky", remaining: 3}
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}

	feedsPath := writeFetchTestFeeds(t, `[
  {"journal":"Flaky","url":"`+server.URL+`/flaky"},
  {"journal":"Steady","url":"`+server.URL+`/rss"}
]`)

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if transport.requests != 4 {
		t.Fatalf("flaky requests = %d, want 4 (3 first-pass attempts + 1 retry)", transport.requests)
	}
	titles := map[string]bool{}
	for _, paper := range result.Papers {
		titles[paper.Title] = true
	}
	if !titles["Recovered feed sample"] || !titles["Steady feed sample"] {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

func TestFetchAllBoundsRetriesForPersistentDNSFailure(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	oldPassDelays := fetchRetryPassDelays
	oldClient := fetchHTTPClient
	fetchRetryBackoffs = []time.Duration{0, 0}
	fetchRetryPassDelays = []time.Duration{0, 0}
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second}
	defer func() {
		fetchRetryBackoffs = oldBackoffs
		fetchRetryPassDelays = oldPassDelays
		fetchHTTPClient = oldClient
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rssBodyFor("Steady feed sample"))
	}))
	defer server.Close()

	transport := &flakyDNSTransport{base: http.DefaultTransport, failForPath: "/flaky", remaining: 999}
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}

	feedsPath := writeFetchTestFeeds(t, `[
  {"journal":"Flaky","url":"`+server.URL+`/flaky"},
  {"journal":"Steady","url":"`+server.URL+`/rss"}
]`)

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if transport.requests != 9 {
		t.Fatalf("flaky requests = %d, want 9 (3 attempts x 3 passes)", transport.requests)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "/flaky") {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "Steady feed sample" {
		t.Fatalf("papers = %#v", result.Papers)
	}
}

func TestFetchAllDoesNotRetryNonTransientFailures(t *testing.T) {
	oldClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second}
	defer func() { fetchHTTPClient = oldClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("this is not xml at all"))
	}))
	defer server.Close()

	transport := &flakyDNSTransport{base: http.DefaultTransport, failForPath: "/none", remaining: 0}
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: transport}

	feedsPath := writeFetchTestFeeds(t, `[{"journal":"Broken","url":"`+server.URL+`/broken"}]`)

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if transport.requests != 0 {
		t.Fatalf("requests through flaky transport = %d, want 0", transport.requests)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "/broken") {
		t.Fatalf("errors = %#v", result.Errors)
	}
}

func TestFetchAllResolvesEmailFeedURLFromOptions(t *testing.T) {
	oldClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second}
	defer func() { fetchHTTPClient = oldClient }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rssBodyFor("Email alert sample"))
	}))
	defer server.Close()

	feedsPath := writeFetchTestFeeds(t, `[{"journal":"RSC journals (email alerts)","url":"","private":true,"email_source":"rsc-email-alerts"}]`)

	result, err := FetchAll(feedsPath, FetchOptions{EmailFeedURL: server.URL, SelectedFeedURLs: []string{"email:rsc-email-alerts"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "Email alert sample" {
		t.Fatalf("papers = %#v", result.Papers)
	}
	if len(result.FeedURLs) != 1 || result.FeedURLs[0] != "email:rsc-email-alerts" {
		t.Fatalf("feedURLs = %#v", result.FeedURLs)
	}
}

func TestFetchAllReportsUnconfiguredEmailFeed(t *testing.T) {
	oldClient := fetchHTTPClient
	fetchHTTPClient = &http.Client{Timeout: 30 * time.Second}
	defer func() { fetchHTTPClient = oldClient }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(rssBodyFor("Steady feed sample"))
	}))
	defer server.Close()

	feedsPath := writeFetchTestFeeds(t, `[
  {"journal":"RSC journals (email alerts)","url":"","private":true,"email_source":"rsc-email-alerts"},
  {"journal":"Nature","url":"`+server.URL+`/rss"}
]`)

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("unexpected HTTP requests: %d, want 1 (only the healthy feed)", requests)
	}
	if len(result.Papers) != 1 || result.Papers[0].Title != "Steady feed sample" {
		t.Fatalf("papers = %#v", result.Papers)
	}
	if len(result.Errors) != 1 || !strings.HasPrefix(result.Errors[0], "email:rsc-email-alerts: ") || strings.Contains(result.Errors[0], "http") {
		t.Fatalf("errors = %#v", result.Errors)
	}
}
