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

func TestEnrichPaperFillsAuthorsAndAbstractFromCrossref(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1093/nar/gkag494" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1093/nar/gkag494","container-title":["Nucleic Acids Research"],"abstract":"<jats:title>Abstract</jats:title><jats:p>Chromatin remodeling abstract.</jats:p>","author":[{"given":"Alice","family":"Ng"},{"given":"Bob","family":"Chen"}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = "http://127.0.0.1:1"
	crossrefBaseURL = server.URL

	paper := store.Paper{Title: "NAR paper", DOI: stringPtr("10.1093/nar/gkag494")}
	enriched := EnrichPaper(paper)
	if enriched.Abstract == nil || *enriched.Abstract != "Chromatin remodeling abstract." || enriched.AbstractSource != "crossref" {
		t.Fatalf("unexpected abstract result: %#v", enriched)
	}
	if len(enriched.Authors) != 2 || enriched.Authors[0] != "Alice Ng" || enriched.Authors[1] != "Bob Chen" {
		t.Fatalf("unexpected authors: %#v", enriched.Authors)
	}
	if enriched.Journal == nil || *enriched.Journal != "Nucleic Acids Research" {
		t.Fatalf("unexpected journal: %#v", enriched.Journal)
	}
}

func TestEnrichPaperKeepsRSSAbstractWhileBackfillingAuthors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1021/jacs.5c22299" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1021/jacs.5c22299","container-title":["Journal of the American Chemical Society"],"author":[{"given":"Yanjing","family":"Gao"},{"given":"Guangrui","family":"Chen"}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = "http://127.0.0.1:1"
	crossrefBaseURL = server.URL

	paper := store.Paper{
		Title:          "JACS paper",
		DOI:            stringPtr("10.1021/jacs.5c22299"),
		Abstract:       stringPtr("rss abstract"),
		AbstractSource: "rss",
	}
	enriched := EnrichPaper(paper)
	if enriched.Abstract == nil || *enriched.Abstract != "rss abstract" || enriched.AbstractSource != "rss" {
		t.Fatalf("unexpected abstract result: %#v", enriched)
	}
	if len(enriched.Authors) != 2 || enriched.Authors[0] != "Yanjing Gao" || enriched.Authors[1] != "Guangrui Chen" {
		t.Fatalf("unexpected authors: %#v", enriched.Authors)
	}
	if enriched.Journal == nil || *enriched.Journal != "Journal of the American Chemical Society" {
		t.Fatalf("unexpected journal: %#v", enriched.Journal)
	}
}
