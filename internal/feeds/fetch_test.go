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

func TestFetchAllParsesRSSAndContinuesOnFeedFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rss":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:prism="http://prismstandard.org/namespaces/basic/2.0/">
  <channel>
    <title>Nature</title>
    <item>
      <title>Paper One</title>
      <link>https://example.com/paper-1</link>
      <guid>doi:10.1000/xyz-1</guid>
      <pubDate>Fri, 16 May 2026 10:00:00 +0000</pubDate>
      <prism:publicationName>Nature</prism:publicationName>
      <content:encoded><![CDATA[<p>ABSTRACT: Useful abstract.</p><img src="https://example.com/fig.png">]]></content:encoded>
      <dc:creator>Alice</dc:creator>
    </item>
  </channel>
</rss>`))
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[
  {"journal":"Nature","url":"`+server.URL+`/rss"},
  {"journal":"Broken","url":"`+server.URL+`/broken"}
]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 1 {
		t.Fatalf("fetched = %d", result.Fetched)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "/broken") {
		t.Fatalf("errors = %#v", result.Errors)
	}
	paper := result.Papers[0]
	if paper.Title != "Paper One" || paper.URL != "https://example.com/paper-1" {
		t.Fatalf("unexpected paper identity: %#v", paper)
	}
	if paper.DOI == nil || *paper.DOI != "10.1000/xyz-1" {
		t.Fatalf("unexpected doi: %#v", paper.DOI)
	}
	if paper.Abstract == nil || !strings.Contains(*paper.Abstract, "Useful abstract") {
		t.Fatalf("unexpected abstract: %#v", paper.Abstract)
	}
	if paper.AbstractHTML == nil || !strings.Contains(*paper.AbstractHTML, "<img") {
		t.Fatalf("unexpected abstract html: %#v", paper.AbstractHTML)
	}
	if len(paper.AbstractImages) != 1 || paper.AbstractImages[0].Src != "https://example.com/fig.png" {
		t.Fatalf("unexpected abstract images: %#v", paper.AbstractImages)
	}
}

func TestFetchAllParsesNatureRDFRSS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:prism="http://prismstandard.org/namespaces/basic/2.0/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns="http://purl.org/rss/1.0/">
  <channel rdf:about="http://feeds.nature.com/natcatal/rss/current">
    <title>Nature Catalysis</title>
    <link>http://feeds.nature.com/natcatal/rss/current</link>
    <prism:publicationName>Nature Catalysis</prism:publicationName>
  </channel>
  <item rdf:about="https://www.nature.com/articles/s41929-026-01535-6">
    <title><![CDATA[Direct electrochemical propylene epoxidation over amorphized perovskite oxide in non-halogenated aqueous electrolyte]]></title>
    <link>https://www.nature.com/articles/s41929-026-01535-6</link>
    <content:encoded><![CDATA[<p>Nature Catalysis, Published online: 12 May 2026; <a href="https://www.nature.com/articles/s41929-026-01535-6">doi:10.1038/s41929-026-01535-6</a></p>Direct electrocatalytic oxidation of propylene in aqueous solution has been limited to precious-metal catalysts in halogenated electrolytes.]]></content:encoded>
    <dc:creator>Kalipada Koner</dc:creator>
    <dc:creator>Jason S. Adams</dc:creator>
    <dc:creator>Kalipada Koner</dc:creator>
    <dc:identifier>doi:10.1038/s41929-026-01535-6</dc:identifier>
    <dc:source>Nature Catalysis, Published online: 2026-05-12; | doi:10.1038/s41929-026-01535-6</dc:source>
    <dc:date>2026-05-12</dc:date>
    <prism:publicationName>Nature Catalysis</prism:publicationName>
    <prism:doi>10.1038/s41929-026-01535-6</prism:doi>
  </item>
</rdf:RDF>`))
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Nature Catalysis","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fetched != 1 {
		t.Fatalf("fetched = %d", result.Fetched)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}

	paper := result.Papers[0]
	if paper.Title != "Direct electrochemical propylene epoxidation over amorphized perovskite oxide in non-halogenated aqueous electrolyte" {
		t.Fatalf("unexpected title: %#v", paper.Title)
	}
	if paper.URL != "https://www.nature.com/articles/s41929-026-01535-6" {
		t.Fatalf("unexpected url: %#v", paper.URL)
	}
	if paper.FeedTitle == nil || *paper.FeedTitle != "Nature Catalysis" {
		t.Fatalf("unexpected feed title: %#v", paper.FeedTitle)
	}
	if paper.DOI == nil || *paper.DOI != "10.1038/s41929-026-01535-6" {
		t.Fatalf("unexpected doi: %#v", paper.DOI)
	}
	if paper.Journal == nil || *paper.Journal != "Nature Catalysis" {
		t.Fatalf("unexpected journal: %#v", paper.Journal)
	}
	if paper.PublishedDate == nil || *paper.PublishedDate != "2026-05-12" {
		t.Fatalf("unexpected published date: %#v", paper.PublishedDate)
	}
	if len(paper.Authors) != 2 || paper.Authors[0] != "Kalipada Koner" || paper.Authors[1] != "Jason S. Adams" {
		t.Fatalf("unexpected authors: %#v", paper.Authors)
	}
	if paper.Abstract == nil || strings.Contains(*paper.Abstract, "Published online") || !strings.Contains(*paper.Abstract, "Direct electrocatalytic oxidation") {
		t.Fatalf("unexpected abstract: %#v", paper.Abstract)
	}
}

func TestFetchAllSplitsCombinedRSSAuthorStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Angewandte Chemie International Edition</title>
    <item>
      <title>Combined author sample</title>
      <link>https://example.com/paper-authors</link>
      <guid>doi:10.1000/combined-authors</guid>
      <pubDate>Thu, 22 May 2026 10:00:00 +0000</pubDate>
      <author>Alice Smith, Bob Q. Jones, Carol Tan</author>
      <description><![CDATA[<p>Abstract text.</p>]]></description>
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
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Angew","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Papers) != 1 {
		t.Fatalf("papers = %d", len(result.Papers))
	}

	paper := result.Papers[0]
	want := []string{"Alice Smith", "Bob Q. Jones", "Carol Tan"}
	if len(paper.Authors) != len(want) {
		t.Fatalf("unexpected authors: %#v", paper.Authors)
	}
	for i := range want {
		if paper.Authors[i] != want[i] {
			t.Fatalf("unexpected authors: %#v", paper.Authors)
		}
	}
}

func TestFetchAllKeepsSingleLastFirstAuthorString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Format preservation sample</title>
    <item>
      <title>Single author format sample</title>
      <link>https://example.com/paper-last-first</link>
      <guid>doi:10.1000/last-first</guid>
      <author>Smith, John</author>
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
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Sample","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Papers) != 1 {
		t.Fatalf("papers = %d", len(result.Papers))
	}
	if len(result.Papers[0].Authors) != 1 || result.Papers[0].Authors[0] != "Smith, John" {
		t.Fatalf("unexpected authors: %#v", result.Papers[0].Authors)
	}
}

func TestFetchAllSplitsBioRxivCreatorPairs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns="http://purl.org/rss/1.0/">
  <channel rdf:about="https://connect.biorxiv.org/biorxiv_xml.php?subject=biochemistry">
    <title>bioRxiv Biochemistry</title>
  </channel>
  <item rdf:about="https://www.biorxiv.org/content/10.64898/2026.05.19.726321v1?rss=1">
    <title><![CDATA[Pair-formatted authors]]></title>
    <link>https://www.biorxiv.org/content/10.64898/2026.05.19.726321v1?rss=1</link>
    <description><![CDATA[Example abstract.]]></description>
    <dc:creator><![CDATA[ Powell, W., Yan, N., Tse, E. ]]></dc:creator>
    <dc:date>2026-05-21</dc:date>
    <dc:identifier>doi:10.64898/2026.05.19.726321</dc:identifier>
  </item>
</rdf:RDF>`))
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"bioRxiv","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Papers) != 1 {
		t.Fatalf("papers = %d", len(result.Papers))
	}
	want := []string{"Powell, W.", "Yan, N.", "Tse, E."}
	if len(result.Papers[0].Authors) != len(want) {
		t.Fatalf("unexpected authors: %#v", result.Papers[0].Authors)
	}
	for i := range want {
		if result.Papers[0].Authors[i] != want[i] {
			t.Fatalf("unexpected authors: %#v", result.Papers[0].Authors)
		}
	}
}

func TestFetchAllKeepsACSTOCGraphicWhenAbstractTextIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Journal of the American Chemical Society: Latest Articles (ACS Publications)</title>
    <item>
      <title>[ASAP] TOC-only sample</title>
      <link>http://dx.doi.org/10.1021/jacs.5c22299</link>
      <description>&lt;p&gt;&lt;img src="https://pubs.acs.org/cms/sample/asset/images/medium/sample.gif" alt="TOC Graphic" /&gt;&lt;/p&gt;&lt;div&gt;&lt;cite&gt;Journal of the American Chemical Society&lt;/cite&gt;&lt;/div&gt;&lt;div&gt;DOI: 10.1021/jacs.5c22299&lt;/div&gt;</description>
      <pubDate>Thu, 21 May 2026 04:40:42 PDT</pubDate>
      <guid isPermaLink="false">http://dx.doi.org/10.1021/jacs.5c22299</guid>
      <dc:creator>Yanjing Gao, Guangrui Chen, and Jihong Yu</dc:creator>
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
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"JACS","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Papers) != 1 {
		t.Fatalf("papers = %d", len(result.Papers))
	}

	paper := result.Papers[0]
	if paper.Abstract != nil {
		t.Fatalf("expected nil abstract, got %#v", paper.Abstract)
	}
	if paper.AbstractHTML == nil || !strings.Contains(*paper.AbstractHTML, "sample.gif") {
		t.Fatalf("unexpected abstract html: %#v", paper.AbstractHTML)
	}
	if len(paper.AbstractImages) != 1 || paper.AbstractImages[0].Src != "https://pubs.acs.org/cms/sample/asset/images/medium/sample.gif" {
		t.Fatalf("unexpected abstract images: %#v", paper.AbstractImages)
	}
	if paper.AbstractSource != "rss" {
		t.Fatalf("unexpected abstract source: %q", paper.AbstractSource)
	}
}

func TestFetchAllRetriesAfterForbiddenAndParsesRDFRSS(t *testing.T) {
	oldBackoffs := fetchRetryBackoffs
	fetchRetryBackoffs = []time.Duration{0}
	defer func() { fetchRetryBackoffs = oldBackoffs }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns="http://purl.org/rss/1.0/">
  <channel rdf:about="https://www.cell.com/cell/current.rss">
    <title>Cell</title>
  </channel>
  <item rdf:about="https://www.cell.com/cell/fulltext/S0092-8674(26)00394-6?rss=yes">
    <title>Retry success sample</title>
    <link>https://www.cell.com/cell/fulltext/S0092-8674(26)00394-6?rss=yes</link>
    <description>Cell abstract text.</description>
    <dc:creator>Alice Smith</dc:creator>
    <dc:identifier>10.1016/j.cell.2026.04.005</dc:identifier>
    <dc:date>2026-05-14</dc:date>
    <prism:publicationName xmlns:prism="http://prismstandard.org/namespaces/basic/2.0/">Cell</prism:publicationName>
  </item>
</rdf:RDF>`))
	}))
	defer server.Close()

	root := t.TempDir()
	feedsPath := filepath.Join(root, "data", "rss_feeds.json")
	if err := os.MkdirAll(filepath.Dir(feedsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(feedsPath, []byte(`[{"journal":"Cell","url":"`+server.URL+`"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := FetchAll(feedsPath, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
	if len(result.Errors) != 0 || len(result.Papers) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Papers[0].Title != "Retry success sample" {
		t.Fatalf("unexpected title: %#v", result.Papers[0].Title)
	}
}

func TestFetchAllRetriesAfterChallengePageThenParsesRSS(t *testing.T) {
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
	if requests != 2 {
		t.Fatalf("requests = %d", requests)
	}
	if len(result.Errors) != 0 || len(result.Papers) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Papers[0].Title != "Challenge retry sample" {
		t.Fatalf("unexpected title: %#v", result.Papers[0].Title)
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
