package feeds

import (
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
