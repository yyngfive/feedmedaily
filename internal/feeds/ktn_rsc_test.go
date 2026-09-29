package feeds

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/journals"
)

func TestKTNRSCIssueEmailsBecomePapers(t *testing.T) {
	const source = "https://example.org/rss/private"
	issue := func(journal, title, doi string) string {
		return fmt.Sprintf(`<item><title>%s Table of Contents for 24 September 2026: Volume 18, Issue 36</title><content:encoded><![CDATA[
<table><tr><td>%s Latest issue Alert</td></tr>
<tr><td style="font-size:22px"><a href="https://tracker.example/cover">Front Cover</a></td></tr>
<tr><td>DOI: 10.1039/D6COVER01</td></tr>
<tr><td style="font-size: 22px"><a href="https://tracker.example/article">%s</a></td></tr>
<tr><td><a style="color:#006fb7">Alice</a> and <a style="color:#006fb7">Bob</a></td></tr>
<tr><td>2026, 18, 123; DOI: %s</td></tr></table>]]></content:encoded></item>`, journal, journal, title, doi)
	}
	xml := `<rss xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>Royal Society of Chemistry</title><generator>kill-the-news</generator>` +
		`<item><title>One-Time Passcode</title><content:encoded><![CDATA[<p>123456</p>]]></content:encoded></item>` +
		issue("Soft Matter", "Coalescence of liquid crystal drops", "10.1039/D6SM00001A") +
		issue("Chemical Communications", "Catalytic carbon dioxide reduction", "10.1039/D6CC00002B") + `</channel></rss>`
	papers, err := parseFeedBody(source, 0, []byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != 2 {
		t.Fatalf("got %d papers, want 2", len(papers))
	}
	for i, want := range []string{"Soft Matter", "Chemical Communications"} {
		paper := papers[i]
		if paper.Journal == nil || *paper.Journal != want || journals.Resolve(paper.SourceURL, paper.Journal, paper.FeedTitle).Label != want {
			t.Fatalf("journal identity for paper %d: %#v", i, paper)
		}
		if paper.DOI == nil || !strings.HasPrefix(paper.URL, "https://doi.org/") || len(paper.Authors) != 2 || paper.PublishedDate != nil {
			t.Fatalf("paper metadata for paper %d: %#v", i, paper)
		}
	}
}

func TestKTNRSCMalformedIssueFails(t *testing.T) {
	xml := `<rss xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>Royal Society of Chemistry</title><generator>kill-the-news</generator><item><title>Soft Matter Table of Contents for 24 September 2026: Volume 18, Issue 36</title><content:encoded><![CDATA[<p>Soft Matter Latest issue Alert</p>]]></content:encoded></item></channel></rss>`
	if _, err := parseFeedBody("https://example.org/rss/private", 0, []byte(xml)); err == nil {
		t.Fatal("malformed issue alert was silently accepted")
	}
}

func TestPrivateFeedURLIsRedactedInDiagnostics(t *testing.T) {
	const private = "https://news.example/rss/abcdefghijklmnopqrstu"
	if got := SafeFeedURL(private); got != "https://news.example/rss/private" {
		t.Fatalf("private URL was not redacted: %q", got)
	}
	const public = "http://feeds.rsc.org/rss/cc"
	if got := SafeFeedURL(public); got != public {
		t.Fatalf("public journal feed URL changed: %q", got)
	}
}

func TestKTNRSCFeedLive(t *testing.T) {
	feedURL := os.Getenv("KTN_RSS_URL")
	if feedURL == "" {
		t.Skip("set KTN_RSS_URL to check a live feed")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(feedURL)
	if err != nil {
		t.Fatalf("fetch live KTN RSS failed: %T", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("live KTN RSS returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		t.Fatalf("read live KTN RSS failed: %T", err)
	}
	papers, err := parseFeedBody("https://example.org/rss/private", 0, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) == 0 {
		t.Fatal("live KTN RSS yielded no papers")
	}
	journalsSeen := map[string]bool{}
	for _, paper := range papers {
		identity := journals.Resolve(paper.SourceURL, paper.Journal, paper.FeedTitle)
		if paper.DOI == nil || identity.Label == "Royal Society of Chemistry" || identity.Label == "Unknown journal" {
			t.Fatalf("invalid live paper metadata: title=%q journal=%q", paper.Title, identity.Label)
		}
		journalsSeen[identity.Label] = true
	}
	t.Logf("parsed %d DOI papers across %d journals: %v", len(papers), len(journalsSeen), journalsSeen)
}
