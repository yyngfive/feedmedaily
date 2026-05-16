package metadata

import (
	"net/http"
	"net/http/httptest"
	"testing"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestNormalizeDOIAndPaperKey(t *testing.T) {
	if got := NormalizeDOI(" DOI:10.1000/ABC. "); got != "10.1000/abc" {
		t.Fatalf("unexpected normalized doi: %s", got)
	}
	paper := store.Paper{Title: "Title", URL: "https://example.com", DOI: stringPtr("10.1000/abc")}
	if key := PaperKey(paper); key != "doi:10.1000/abc" {
		t.Fatalf("unexpected paper key: %s", key)
	}
}

func TestEnrichPaperPrefersOpenAlexAndFallsBackToRSS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/https://doi.org/10.1000/test" {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/test","abstract_inverted_index":{"RNA":[0],"biology":[1]},"primary_location":{"source":{"display_name":"Nature"}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	previousOpenAlex := openAlexBaseURL
	defer func() { openAlexBaseURL = previousOpenAlex }()
	openAlexBaseURL = server.URL

	paper := store.Paper{Title: "RNA paper", DOI: stringPtr("10.1000/test"), Abstract: stringPtr("rss abstract"), AbstractSource: "rss"}
	enriched := EnrichPaper(paper)
	if enriched.Abstract == nil || *enriched.Abstract != "RNA biology" || enriched.AbstractSource != "openalex" {
		t.Fatalf("unexpected enriched paper: %#v", enriched)
	}
}

func TestEnrichPaperKeepsRSSWhenProvidersFail(t *testing.T) {
	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = "http://127.0.0.1:1"
	crossrefBaseURL = "http://127.0.0.1:1"

	paper := store.Paper{Title: "RNA paper", Abstract: stringPtr("rss abstract"), AbstractSource: "rss"}
	enriched := EnrichPaper(paper)
	if enriched.AbstractSource != "rss" || enriched.Abstract == nil || *enriched.Abstract != "rss abstract" {
		t.Fatalf("unexpected fallback result: %#v", enriched)
	}
}
