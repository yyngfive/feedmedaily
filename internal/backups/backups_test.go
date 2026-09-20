package backups

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestCreateRestorableSnapshotIncludingCommittedWAL(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{DataDir: filepath.Join(root, "data"), DatabasePath: filepath.Join(root, "data", "literature.sqlite"), ProfilePath: filepath.Join(root, "data", "classification_profile.json"), FeedsPath: filepath.Join(root, "data", "rss_feeds.json")}
	if err := os.MkdirAll(settings.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	source, err := sqlite.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	now := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	journal := "Nature"
	paperID, _, err := source.UpsertPaper(sqlite.Paper{SourceURL: "https://nature.com/nature.rss", Journal: &journal, Title: "Snapshot paper", URL: "https://nature.com/article"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.SaveClassification(paperID, sqlite.Classification{Relevance: "direct", Model: "test", TopicTags: []string{}, RecommendedAction: "read"}, now); err != nil {
		t.Fatal(err)
	}
	profile := []byte(`{"topic_taxonomy":[]}`)
	feeds := []byte(`[ {"journal":"Nature","url":"https://nature.com/nature.rss?access=preserve"} ]`)
	if err := os.WriteFile(settings.ProfilePath, profile, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.FeedsPath, feeds, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SCIRSS_CLASSIFIER_API_KEY=must-not-ship"), 0600); err != nil {
		t.Fatal(err)
	}
	var configLock sync.Mutex
	entry, err := Create(context.Background(), settings, "0.6.2", &configLock, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Size <= 0 || entry.ID == "" {
		t.Fatalf("invalid entry %#v", entry)
	}
	path, err := Path(settings, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		found[file.Name] = data
	}
	archive.Close()
	for _, name := range []string{"literature.sqlite", "classification_profile.json", "rss_feeds.json", "RESTORE.md", "manifest.json"} {
		if _, ok := found[name]; !ok {
			t.Fatalf("archive missing %s", name)
		}
	}
	for name := range found {
		if name == ".env" || strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
			t.Fatalf("sensitive/unexpected archive member %s", name)
		}
	}
	if string(found["rss_feeds.json"]) != string(feeds) {
		t.Fatal("feed URLs changed")
	}
	var manifest Manifest
	if err := json.Unmarshal(found["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.AppVersion != "0.6.2" || len(manifest.Missing) != 0 || len(manifest.Files) != 4 {
		t.Fatalf("manifest %#v", manifest)
	}
	restoredPath := filepath.Join(t.TempDir(), "data", "literature.sqlite")
	if err := os.MkdirAll(filepath.Dir(restoredPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restoredPath, found["literature.sqlite"], 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := sqlite.OpenRead(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	count, err := restored.PaperCount()
	if err != nil || count != 1 {
		t.Fatalf("restored papers=%d err=%v", count, err)
	}
	classified, err := restored.ClassifiedPaperCount()
	if err != nil || classified != 1 {
		t.Fatalf("restored classified=%d err=%v", classified, err)
	}
	if err := restored.QuickCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if string(found["classification_profile.json"]) != string(profile) {
		t.Fatal("profile did not round trip")
	}
	if _, err := List(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := Path(settings, "../.env"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestCreateMissingConfigAndFailedSnapshot(t *testing.T) {
	settings := config.Settings{DataDir: t.TempDir()}
	settings.DatabasePath = filepath.Join(settings.DataDir, "literature.sqlite")
	settings.ProfilePath = filepath.Join(settings.DataDir, "classification_profile.json")
	settings.FeedsPath = filepath.Join(settings.DataDir, "rss_feeds.json")
	db, err := sqlite.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var lock sync.Mutex
	entry, err := Create(context.Background(), settings, "test", &lock, nil)
	if err != nil {
		t.Fatal(err)
	}
	path, err := Path(settings, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	for _, file := range archive.File {
		if file.Name == "manifest.json" {
			r, _ := file.Open()
			data, _ := io.ReadAll(r)
			r.Close()
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
		}
	}
	archive.Close()
	if len(manifest.Missing) != 2 {
		t.Fatalf("missing manifest entries %#v", manifest.Missing)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), settings, "test", &lock, nil); err == nil {
		t.Fatal("invalid config accepted")
	}
	entries, err := List(settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("partial backup published: %#v", entries)
	}
}
