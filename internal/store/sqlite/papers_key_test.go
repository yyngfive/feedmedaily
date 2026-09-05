package sqlite

import (
	"path/filepath"
	"testing"
	"time"
)

// 回归背景：enrichment 补 DOI 后按内容重算 paper_key，会把同一篇文章插成
// 新行；分类结果仍挂在旧行上，新行永远处于未分类状态。
func TestUpsertPaperWithKeyKeepsOriginalRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	s, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()

	paper := Paper{Title: "Cell paper", URL: "https://example.com/article", SourceURL: "https://example.com/rss"}
	id, isNew, err := s.UpsertPaper(paper, now)
	if err != nil {
		t.Fatal(err)
	}
	if !isNew {
		t.Fatalf("expected first insert to be new")
	}

	storedKey, err := s.StoredPaperKey(id)
	if err != nil {
		t.Fatal(err)
	}
	if storedKey != "url:https://example.com/article" {
		t.Fatalf("unexpected stored key: %q", storedKey)
	}

	enriched := paper
	enriched.DOI = stringPtr("10.1000/valid")
	updatedID, isNew, err := s.UpsertPaperWithKey(enriched, storedKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if updatedID != id || isNew {
		t.Fatalf("expected enrichment upsert to stay on row %d, got id %d isNew=%v", id, updatedID, isNew)
	}

	merged, err := s.PaperByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if merged.DOI == nil || *merged.DOI != "10.1000/valid" {
		t.Fatalf("expected doi on original row, got %#v", merged.DOI)
	}

	reingestID, isNew, err := s.UpsertPaper(paper, now)
	if err != nil {
		t.Fatal(err)
	}
	if reingestID != id || isNew {
		t.Fatalf("expected re-ingest to reuse row %d, got id %d isNew=%v", id, reingestID, isNew)
	}

	if err := s.ClearPaperDOI(id); err != nil {
		t.Fatal(err)
	}
	cleared, err := s.PaperByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.DOI != nil {
		t.Fatalf("expected cleared doi, got %#v", cleared.DOI)
	}
}
