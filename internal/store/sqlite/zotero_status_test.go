package sqlite

import (
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertZoteroStatusTracksSavedAndErrorStates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	paperID, _, err := store.UpsertPaper(Paper{
		SourceURL: "https://example.com/rss",
		Title:     "Zotero write test",
		URL:       "https://example.com/zotero-write-test",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	itemKey := "ITEM-1"
	saved, err := store.UpsertZoteroStatus(paperID, "saved", &itemKey, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || !saved.Saved || saved.ItemKey == nil || *saved.ItemKey != "ITEM-1" || saved.SavedAt == nil {
		t.Fatalf("unexpected saved status: %#v", saved)
	}
	lastError := "rate limited"
	failed, err := store.UpsertZoteroStatus(paperID, "error", nil, &lastError, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if failed == nil || failed.Saved || failed.LastError == nil || *failed.LastError != "rate limited" || failed.SavedAt != nil {
		t.Fatalf("unexpected error status: %#v", failed)
	}
	reloaded, err := store.LatestZoteroStatus(paperID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == nil || reloaded.State == nil || *reloaded.State != "error" {
		t.Fatalf("unexpected reloaded status: %#v", reloaded)
	}
}
