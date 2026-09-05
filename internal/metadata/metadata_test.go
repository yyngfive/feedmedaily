package metadata

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestNormalizeDOIAndPaperKey(t *testing.T) {
	if got := NormalizeDOI(" DOI:10.1000/ABC. "); got != "10.1000/abc" {
		t.Fatalf("unexpected normalized doi: %s", got)
	}
	paper := store.Paper{Title: "RNA paper", DOI: stringPtr("10.1000/abc")}
	if key := PaperKey(paper); key != "doi:10.1000/abc" {
		t.Fatalf("unexpected paper key: %s", key)
	}
}

func TestTitlesMatchAndDatesMatch(t *testing.T) {
	cases := []struct {
		name        string
		paperTitle  string
		recordTitle string
		want        bool
	}{
		{"exact", "Single-cell mapping of regulatory DNA-protein interactions", "Single-cell mapping of regulatory DNA-protein interactions", true},
		{"html tags and entities", "In vitro transcribed circRNA as a therapeutic agent", "In <em>vitro</em> transcribed circRNA as a therapeutic &amp; monitoring agent", false},
		{"punctuation differences", "Excited-state orbital angular momentum enables all-optical molecular spin coherence", "Excited–state orbital angular momentum enables all-optical molecular spin coherence", true},
		{"record title truncates paper title", "A Computational Study of Gemcitabine Encapsulation in CC3 Po", "A Computational Study of Gemcitabine Encapsulation in CC3 Porous Organic Cages", true},
		{"paper title truncates record title", "Tabula Sapiens 2.0: A comprehensive transcriptomic atlas of human cell types", "Tabula Sapiens 2.0", false},
		{"short title not contained", "Cell", "Cell Reports: a very different article about something else entirely", false},
		{"unrelated", "Endogenous opioid dynamics in the dorsal striatum", "Goals and Habits in the Brain", false},
	}
	for _, tc := range cases {
		if got := titlesMatch(tc.paperTitle, tc.recordTitle); got != tc.want {
			t.Fatalf("%s: titlesMatch(%q, %q) = %v", tc.name, tc.paperTitle, tc.recordTitle, got)
		}
	}

	dateCases := []struct {
		name       string
		paperDate  string
		recordDate string
		want       bool
	}{
		{"same year and month", "2026-06-11", "2026-06-27", true},
		{"different month", "2026-06-11", "2026-07-01", false},
		{"different year", "2026-06-11", "2007-12-01", false},
		{"record year only matches", "2026-06-11", "2026", true},
		{"record year only differs", "2026-06-11", "2014", false},
		{"missing paper date", "", "2026-06-11", false},
		{"missing record date", "2026-06-11", "", false},
	}
	for _, tc := range dateCases {
		if got := datesMatch(tc.paperDate, tc.recordDate); got != tc.want {
			t.Fatalf("%s: datesMatch(%q, %q) = %v", tc.name, tc.paperDate, tc.recordDate, got)
		}
	}
}

// 校验规则：标题与发布日期必须双双一致；任一项对不上都视为错配。
func TestExternalRecordMatchesRequiresTitleAndDate(t *testing.T) {
	paper := store.Paper{Title: "RNA paper", PublishedDate: stringPtr("2026-06-11")}
	if !externalRecordMatches(paper, "RNA paper", "2026-06-20") {
		t.Fatalf("expected both matching to be accepted")
	}
	if externalRecordMatches(paper, "RNA paper", "2013-09-01") {
		t.Fatalf("title match with mismatched date must be rejected")
	}
	if externalRecordMatches(paper, "Goals and Habits in the Brain", "2026-06-20") {
		t.Fatalf("date match with mismatched title must be rejected")
	}
	if externalRecordMatches(paper, "Goals and Habits in the Brain", "2013-09-01") {
		t.Fatalf("both mismatched must be rejected")
	}
	// 论文缺少发布日期时无法完成日期校验，一律不采信。
	dateless := store.Paper{Title: "RNA paper"}
	if externalRecordMatches(dateless, "RNA paper", "2026-06-20") {
		t.Fatalf("dateless paper must not adopt a searched doi")
	}
}

func TestEnrichPaperSkipsProvidersWhenRSSIsComplete(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	defer server.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = server.URL
	crossrefBaseURL = server.URL

	paper := store.Paper{
		Title:          "RNA paper",
		DOI:            stringPtr("10.1000/test"),
		Journal:        stringPtr("Nature"),
		Authors:        []string{"Alice Smith"},
		Abstract:       stringPtr("rss abstract"),
		AbstractSource: "rss",
	}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
	if enriched.Abstract == nil || *enriched.Abstract != "rss abstract" || enriched.AbstractSource != "rss" {
		t.Fatalf("unexpected enriched paper: %#v", enriched)
	}
}

func TestEnrichPaperPrefersOpenAlexWhenMetadataIsMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/https://doi.org/10.1000/test" {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/test","title":"RNA paper","publication_date":"2026-01-15","abstract_inverted_index":{"RNA":[0],"biology":[1]},"primary_location":{"source":{"display_name":"Nature"}},"authorships":[{"author":{"display_name":"Alice Smith"}}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	previousOpenAlex := openAlexBaseURL
	defer func() { openAlexBaseURL = previousOpenAlex }()
	openAlexBaseURL = server.URL

	paper := store.Paper{Title: "RNA paper", DOI: stringPtr("10.1000/test"), PublishedDate: stringPtr("2026-01-10")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if enriched.Abstract == nil || *enriched.Abstract != "RNA biology" || enriched.AbstractSource != "openalex" {
		t.Fatalf("unexpected enriched paper: %#v", enriched)
	}
	if len(enriched.Authors) != 1 || enriched.Authors[0] != "Alice Smith" {
		t.Fatalf("unexpected authors: %#v", enriched.Authors)
	}
	if enriched.Journal == nil || *enriched.Journal != "Nature" {
		t.Fatalf("unexpected journal: %#v", enriched.Journal)
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
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if enriched.AbstractSource != "rss" || enriched.Abstract == nil || *enriched.Abstract != "rss abstract" {
		t.Fatalf("unexpected fallback result: %#v", enriched)
	}
}

func TestEnrichPaperFillsAuthorsAndAbstractFromCrossref(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1093/nar/gkag494" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1093/nar/gkag494","title":["NAR paper"],"issued":{"date-parts":[[2026,1]]},"container-title":["Nucleic Acids Research"],"abstract":"<jats:title>Abstract</jats:title><jats:p>Chromatin remodeling abstract.</jats:p>","author":[{"given":"Alice","family":"Ng"},{"given":"Bob","family":"Chen"}]}}`))
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

	paper := store.Paper{Title: "NAR paper", DOI: stringPtr("10.1093/nar/gkag494"), PublishedDate: stringPtr("2026-01-20")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
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
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1021/jacs.5c22299","title":["JACS paper"],"issued":{"date-parts":[[2026,2]]},"container-title":["Journal of the American Chemical Society"],"author":[{"given":"Yanjing","family":"Gao"},{"given":"Guangrui","family":"Chen"}]}}`))
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
		PublishedDate:  stringPtr("2026-02-11"),
		Abstract:       stringPtr("rss abstract"),
		AbstractSource: "rss",
	}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
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

func TestEnrichPaperWithoutDOIUsesOpenAlexOnly(t *testing.T) {
	openAlexRequests := 0
	crossrefRequests := 0
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAlexRequests++
		if r.URL.Path == "/works" {
			_, _ = w.Write([]byte(`{"results":[{"doi":"https://doi.org/10.1000/search-hit","title":"Search-only paper","publication_date":"2026-03-01","abstract_inverted_index":{"Useful":[0],"abstract":[1]},"primary_location":{"source":{"display_name":"Cell"}},"authorships":[{"author":{"display_name":"Alice Smith"}}]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()
	crossrefServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossrefRequests++
		http.NotFound(w, r)
	}))
	defer crossrefServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = crossrefServer.URL

	paper := store.Paper{Title: "Search-only paper", PublishedDate: stringPtr("2026-03-15")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if openAlexRequests != 1 {
		t.Fatalf("openAlexRequests = %d", openAlexRequests)
	}
	if crossrefRequests != 0 {
		t.Fatalf("crossrefRequests = %d", crossrefRequests)
	}
	if enriched.DOI == nil || *enriched.DOI != "10.1000/search-hit" {
		t.Fatalf("unexpected doi: %#v", enriched.DOI)
	}
}

func TestEnrichPaperSearchSkipsNonMatchingCandidates(t *testing.T) {
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works" {
			if !strings.Contains(r.URL.RawQuery, "per-page=5") {
				t.Errorf("expected per-page=5 in search query, got %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"results":[
				{"doi":"https://doi.org/10.1000/wrong-hit","title":"Goals and Habits in the Brain","publication_date":"2013-09-01"},
				{"doi":"https://doi.org/10.1000/title-only-hit","title":"Search-only paper with a longer title","publication_date":"2007-12-01"},
				{"doi":"https://doi.org/10.1000/right-hit","title":"Search-only paper with a longer title","publication_date":"2026-03-01","abstract_inverted_index":{"Useful":[0],"abstract":[1]},"primary_location":{"source":{"display_name":"Cell"}},"authorships":[{"author":{"display_name":"Alice Smith"}}]}
			]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()

	previousOpenAlex := openAlexBaseURL
	defer func() { openAlexBaseURL = previousOpenAlex }()
	openAlexBaseURL = openAlexServer.URL

	paper := store.Paper{Title: "Search-only paper with a longer title", PublishedDate: stringPtr("2026-03-15")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if enriched.DOI == nil || *enriched.DOI != "10.1000/right-hit" {
		t.Fatalf("unexpected doi: %#v", enriched.DOI)
	}
	if enriched.Journal == nil || *enriched.Journal != "Cell" {
		t.Fatalf("unexpected journal: %#v", enriched.Journal)
	}
}

func TestEnrichPaperSearchWithoutMatchKeepsDOIEmpty(t *testing.T) {
	openAlexRequests := 0
	crossrefRequests := 0
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		openAlexRequests++
		_, _ = w.Write([]byte(`{"results":[
			{"doi":"https://doi.org/10.1000/wrong-hit-1","title":"Goals and Habits in the Brain","publication_date":"2013-09-01"},
			{"doi":"https://doi.org/10.1000/title-only-hit","title":"Search-only paper","publication_date":"2007-12-01"}
		]}`))
	}))
	defer openAlexServer.Close()
	crossrefServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crossrefRequests++
	}))
	defer crossrefServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = crossrefServer.URL

	paper := store.Paper{Title: "Search-only paper", PublishedDate: stringPtr("2026-06-11"), Abstract: stringPtr("rss abstract"), AbstractSource: "rss"}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if openAlexRequests != 1 {
		t.Fatalf("openAlexRequests = %d", openAlexRequests)
	}
	if crossrefRequests != 0 {
		t.Fatalf("crossrefRequests = %d", crossrefRequests)
	}
	if enriched.DOI != nil {
		t.Fatalf("expected empty doi, got %#v", enriched.DOI)
	}
	if enriched.Abstract == nil || *enriched.Abstract != "rss abstract" || enriched.AbstractSource != "rss" {
		t.Fatalf("expected rss abstract to be kept: %#v", enriched)
	}
}

func TestEnrichPaperRejectsMismatchedDOI(t *testing.T) {
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/works/https://doi.org/10.1000/nature07509") {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/nature07509","title":"Alternative isoform regulation in human tissue transcriptomes","publication_date":"2008-03-27"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()
	crossrefServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1000/nature07509" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1000/nature07509","title":["In silico prediction of protein-protein interactions in human macrophages"],"issued":{"date-parts":[[2014,5]]},"container-title":["BMC Research Notes"]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer crossrefServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = crossrefServer.URL

	paper := store.Paper{
		Title:         "Single-cell mapping of regulatory DNA-protein interactions",
		DOI:           stringPtr("10.1000/nature07509"),
		PublishedDate: stringPtr("2026-06-11"),
	}
	enriched, doiRejected := EnrichPaper(paper)
	if !doiRejected {
		t.Fatalf("expected doiRejected")
	}
	if enriched.DOI != nil {
		t.Fatalf("expected doi to be cleared, got %#v", enriched.DOI)
	}
	if enriched.Journal != nil {
		t.Fatalf("expected mismatched journal not to be applied, got %#v", enriched.Journal)
	}
	if enriched.Abstract != nil || enriched.AbstractSource != "none" {
		t.Fatalf("expected no external abstract to be applied: %#v", enriched.AbstractSource)
	}
}

// 标题一致但 Crossref 记录的发布日期对不上：同样否决该 DOI。
func TestEnrichPaperRejectsDOIWhenCrossrefDateMismatch(t *testing.T) {
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/works/https://doi.org/10.1000/test") {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/test","title":"RNA paper","publication_date":"2026-06-20"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()
	crossrefServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1000/test" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1000/test","title":["RNA paper"],"issued":{"date-parts":[[2013,9]]},"container-title":["Neuron"]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer crossrefServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = crossrefServer.URL

	paper := store.Paper{Title: "RNA paper", DOI: stringPtr("10.1000/test"), PublishedDate: stringPtr("2026-06-11")}
	enriched, doiRejected := EnrichPaper(paper)
	if !doiRejected {
		t.Fatalf("expected doiRejected: crossref date does not match")
	}
	if enriched.DOI != nil {
		t.Fatalf("expected doi to be cleared, got %#v", enriched.DOI)
	}
}

func TestEnrichPaperKeepsDOIWhenCrossrefAccepts(t *testing.T) {
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/works/https://doi.org/10.1000/test") {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/test","title":"Spins in few-electron quantum dots","publication_date":"2007-12-01"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()
	crossrefServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/works/10.1000/test" {
			_, _ = w.Write([]byte(`{"message":{"DOI":"10.1000/test","title":["RNA paper: a longer title and subtitle"],"issued":{"date-parts":[[2026,1]]},"container-title":["Nucleic Acids Research"]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer crossrefServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = crossrefServer.URL

	paper := store.Paper{Title: "RNA paper: a longer title", DOI: stringPtr("10.1000/test"), PublishedDate: stringPtr("2026-01-05")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected")
	}
	if enriched.DOI == nil || *enriched.DOI != "10.1000/test" {
		t.Fatalf("expected doi to be kept, got %#v", enriched.DOI)
	}
	if enriched.Journal == nil || *enriched.Journal != "Nucleic Acids Research" {
		t.Fatalf("unexpected journal: %#v", enriched.Journal)
	}
}

func TestEnrichPaperKeepsDOIWhenCrossrefIsUnavailable(t *testing.T) {
	openAlexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/works/https://doi.org/10.1000/test") {
			_, _ = w.Write([]byte(`{"doi":"https://doi.org/10.1000/test","title":"Spins in few-electron quantum dots","publication_date":"2007-12-01"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer openAlexServer.Close()

	previousOpenAlex := openAlexBaseURL
	previousCrossref := crossrefBaseURL
	defer func() {
		openAlexBaseURL = previousOpenAlex
		crossrefBaseURL = previousCrossref
	}()
	openAlexBaseURL = openAlexServer.URL
	crossrefBaseURL = "http://127.0.0.1:1"

	paper := store.Paper{Title: "RNA paper", DOI: stringPtr("10.1000/test"), PublishedDate: stringPtr("2026-06-11")}
	enriched, doiRejected := EnrichPaper(paper)
	if doiRejected {
		t.Fatalf("unexpected doiRejected: crossref had no verdict, doi must be kept")
	}
	if enriched.DOI == nil || *enriched.DOI != "10.1000/test" {
		t.Fatalf("expected doi to be kept, got %#v", enriched.DOI)
	}
}
