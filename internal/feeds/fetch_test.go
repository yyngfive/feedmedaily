package feeds

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
