package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/profile"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestGenerateInitialProfileProposalCreatesDatabaseAndProposal(t *testing.T) {
	root := t.TempDir()
	settings := profileJobSettings(root)
	server := profileModelTestServer(t, `{"meta":{"name":"Alice","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"RNA biology"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],"few_shots":[]}`)
	defer server.Close()
	settings.ProfileBaseURL = server.URL

	result, err := GenerateInitialProfileProposal(settings, "RNA biology", stringPtr("Alice"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["proposal_id"] != int64(1) && result["proposal_id"] != 1 {
		t.Fatalf("unexpected bootstrap result: %#v", result)
	}
	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	items, err := sqliteStore.ListProfileProposals()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Summary == "" {
		t.Fatalf("unexpected bootstrap proposals: %#v", items)
	}
}

func TestGenerateProfileProposalUsesOpenFeedback(t *testing.T) {
	root := t.TempDir()
	settings := profileJobSettings(root)
	server := profileModelTestServer(t, `{"summary":"Tighten RNA rules","direct_rule_additions":["RNA chemistry"],"indirect_rule_additions":[],"unrelated_rule_additions":[],"scope_rewrite":null,"tag_additions":[],"tag_removals":[]}`)
	defer server.Close()
	settings.ProfileBaseURL = server.URL
	writeProfileFixture(t, settings.ProfilePath)

	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	paperID, _, err := sqliteStore.UpsertPaper(store.Paper{
		SourceURL:      "https://example.com/rss",
		Title:          "RNA chemistry paper",
		URL:            "https://example.com/paper",
		Authors:        []string{},
		AbstractSource: "rss",
		FirstSeenAt:    time.Now().UTC(),
		Raw:            map[string]any{},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.SaveClassification(paperID, store.Classification{
		Relevance:         "indirect",
		Confidence:        0.9,
		TopicTags:         []string{"rna_bio"},
		Reason:            "fixture",
		RecommendedAction: "scan",
		Model:             "fixture",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, err := sqliteStore.CreateFeedback(paperID, "direct", stringPtr("Should be direct"), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := GenerateProfileProposal(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["proposal_id"] != int64(1) && result["proposal_id"] != 1 {
		t.Fatalf("unexpected proposal result: %#v", result)
	}
	sqliteStore, err = store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	item, err := sqliteStore.GetProfileProposal(1)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || len(item.SourceFeedbackIDs) != 1 || item.SourceFeedbackIDs[0] != int(record.ID) {
		t.Fatalf("unexpected saved proposal: %#v", item)
	}
}

func profileJobSettings(root string) config.Settings {
	return config.Settings{
		Mode:             appruntime.ModeSource,
		RootDir:          root,
		AppDir:           root,
		UserDataDir:      root,
		ConfigDir:        filepath.Join(root, "config"),
		DataDir:          filepath.Join(root, "data"),
		DatabasePath:     filepath.Join(root, "data", "literature.sqlite"),
		LogsDir:          filepath.Join(root, "logs"),
		ReportsDir:       filepath.Join(root, "reports"),
		RuntimeStatePath: filepath.Join(root, "runtime.json"),
		WebDistDir:       filepath.Join(root, "web", "dist"),
		FeedsPath:        filepath.Join(root, "data", "rss_feeds.json"),
		ProfilePath:      filepath.Join(root, "data", "classification_profile.json"),
		ProfileAPIKey:    "profile-key",
		ProfileBaseURL:   "https://example.com",
		ProfileModel:     "profile-model",
		ProfileThinking:  "enabled",
	}
}

func profileModelTestServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_ = payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quoteJSONString(content) + `}}]}`))
	}))
}

func writeProfileFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"meta": map[string]any{
			"name":               "Current",
			"version":            1,
			"created_at":         "2026-05-10T00:00:00Z",
			"updated_at":         "2026-05-10T00:00:00Z",
			"source_description": "current",
		},
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{map[string]any{"id": "rna_bio", "label": "RNA Bio"}},
		"few_shots":      []any{},
	}
	if err := profile.WriteCurrent(path, payload); err != nil {
		t.Fatal(err)
	}
}

func quoteJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func stringPtr(value string) *string {
	return &value
}
