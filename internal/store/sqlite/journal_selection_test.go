package sqlite

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCustomPaperSelectionDateIntersectionAndFingerprint(t *testing.T) {
	s, err := OpenOrCreate(filepath.Join(t.TempDir(), "papers.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loc := time.FixedZone("UTC+8", 8*60*60)
	first := time.Date(2026, 9, 1, 12, 0, 0, 0, loc)
	add := func(url, journal, title string, at time.Time, classified bool) int64 {
		t.Helper()
		id, _, err := s.UpsertPaper(Paper{SourceURL: url, Journal: &journal, Title: title, URL: "https://example.org/" + title}, at)
		if err != nil {
			t.Fatal(err)
		}
		if classified {
			err = s.SaveClassification(id, Classification{Relevance: "direct", TopicTags: []string{}, RecommendedAction: "read", Model: "test"}, at)
			if err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	acs := add("https://pubs.acs.org/rss/jacsat/asap.xml", "wrong feed title", "acs", first, false)
	cell := add("https://cell.com/current.rss", "Cell, Volume 189, Issue 10", "cell", first.Add(24*time.Hour), true)
	outside := add("https://cell.com/current.rss", "Cell, Volume 189, Issue 11", "outside", first.Add(48*time.Hour), false)
	keys := []string{}
	options, err := s.JournalOptions()
	if err != nil {
		t.Fatal(err)
	}
	for _, option := range options {
		if option.Label == "Journal of the American Chemical Society" || option.Label == "Cell" {
			keys = append(keys, option.Key)
		}
	}
	from, to := "2026-09-01", "2026-09-02"
	got, err := s.SelectCustomPapers(PaperFilter{JournalKeys: keys, DateFrom: from, DateTo: to}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Classified != 1 || got.Unclassified != 1 || len(got.Fingerprint) != 64 || got.Timezone != "UTC+08:00" {
		t.Fatalf("selection %#v", got)
	}
	found := map[int64]bool{}
	for _, id := range got.PaperIDs {
		found[id] = true
	}
	if !found[acs] || !found[cell] || found[outside] {
		t.Fatalf("wrong selected IDs %v", got.PaperIDs)
	}
	repeat, err := s.SelectCustomPapers(PaperFilter{JournalKeys: keys, DateFrom: from, DateTo: to}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != repeat.Fingerprint {
		t.Fatal("unstable fingerprint")
	}
}

func TestPaperFilterValidatesBoundaries(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	if _, _, err := (PaperFilter{}).Bounds(loc); err == nil {
		t.Fatal("empty filter accepted")
	}
	if _, _, err := (PaperFilter{DateFrom: "2026-02-29"}).Bounds(loc); err == nil {
		t.Fatal("invalid calendar date accepted")
	}
	if _, _, err := (PaperFilter{DateFrom: "2026-09-03", DateTo: "2026-09-02"}).Bounds(loc); err == nil {
		t.Fatal("reversed range accepted")
	}
	start, end, err := (PaperFilter{DateFrom: "2026-09-02", DateTo: "2026-09-02"}).Bounds(loc)
	if err != nil {
		t.Fatal(err)
	}
	if start.Hour() != 0 || end.Sub(*start) != 24*time.Hour || start.Location() != loc {
		t.Fatalf("bad inclusive day window: %v to %v", start, end)
	}
}
