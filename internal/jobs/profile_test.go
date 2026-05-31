package jobs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	proposedProfile := items[0].ProposedProfile
	if len(proposedProfile["topic_taxonomy"].([]any)) != 0 || len(proposedProfile["few_shots"].([]any)) != 0 {
		t.Fatalf("bootstrap proposal should clear deprecated profile fields: %#v", proposedProfile)
	}
}

func TestGenerateProfileProposalUsesOpenFeedback(t *testing.T) {
	root := t.TempDir()
	settings := profileJobSettings(root)
	server := profileModelTestServer(t, `{
  "summary":"Tighten RNA rules",
  "proposed_profile":{
    "meta":{"name":"Current","version":2,"created_at":"2026-05-10T00:00:00Z","updated_at":"2026-05-10T00:00:00Z","source_description":"current"},
    "scope":"RNA biology",
    "relevance_rules":{"direct":["RNA chemistry"],"indirect":[],"unrelated":[]},
    "topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],
    "few_shots":[]
  },
  "changes":[
    {
      "id":"change-1",
      "section":"direct_rule",
      "operation":"rewrite",
      "summary":"Promote chemistry-first RNA work.",
      "text_before":["RNA"],
      "text_after":["RNA chemistry"],
      "topic_before":[],
      "topic_after":[],
      "rationale":"The feedback shows chemistry-first RNA papers should be direct.",
      "source_feedback_ids":[1],
      "source_paper_ids":[1],
      "status":"proposed"
    }
  ]
}`)
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
	if item.BaseProfileVersion != 1 || len(item.Changes) != 1 || item.Changes[0].ID != "change-1" {
		t.Fatalf("unexpected compact proposal payload: %#v", item)
	}
}

func TestGenerateProfileProposalPromptIncludesConflictAwareCompactionGuidance(t *testing.T) {
	root := t.TempDir()
	settings := profileJobSettings(root)
	prompts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		prompts = append(prompts, messages[1].(map[string]any)["content"].(string))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"Tighten overlap\",\"proposed_profile\":{\"meta\":{\"name\":\"Current\",\"version\":2,\"created_at\":\"2026-05-10T00:00:00Z\",\"updated_at\":\"2026-05-10T00:00:00Z\",\"source_description\":\"current\"},\"scope\":\"RNA biology\",\"relevance_rules\":{\"direct\":[\"RNA chemistry\"],\"indirect\":[],\"unrelated\":[]},\"topic_taxonomy\":[{\"id\":\"rna_bio\",\"label\":\"RNA Bio\"}],\"few_shots\":[]},\"changes\":[{\"id\":\"change-1\",\"section\":\"direct_rule\",\"operation\":\"rewrite\",\"summary\":\"Promote chemistry-first RNA work.\",\"text_before\":[\"RNA\"],\"text_after\":[\"RNA chemistry\"],\"topic_before\":[],\"topic_after\":[],\"rationale\":\"The feedback shows chemistry-first RNA papers should be direct.\",\"source_feedback_ids\":[1],\"source_paper_ids\":[1],\"status\":\"proposed\"}]}"}}]}`))
	}))
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
		TopicTags:         []string{},
		Reason:            "fixture",
		RecommendedAction: "scan",
		Model:             "fixture",
	}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := sqliteStore.CreateFeedback(paperID, "direct", stringPtr("Should be direct"), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := GenerateProfileProposal(settings, nil); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 proposal prompt, got %d", len(prompts))
	}
	prompt := prompts[0]
	for _, fragment := range []string{
		"do not also add a second rule that expresses the same boundary",
		"the new feedback wins",
		"preserve the salient coverage from the old rules",
		"Apply the same reason-first compaction standard to direct rules",
	} {
		if !strings.Contains(prompt, fragment) {
			t.Fatalf("proposal prompt missing %q: %s", fragment, prompt)
		}
	}
}

func TestGenerateProfileProposalAllowsMaintenanceModeWithoutFeedback(t *testing.T) {
	root := t.TempDir()
	settings := profileJobSettings(root)
	prompts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		prompts = append(prompts, messages[1].(map[string]any)["content"].(string))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"Clean up unrelated rules\",\"proposed_profile\":{\"meta\":{\"name\":\"Current\",\"version\":2,\"created_at\":\"2026-05-10T00:00:00Z\",\"updated_at\":\"2026-05-10T00:00:00Z\",\"source_description\":\"current\"},\"scope\":\"RNA biology\",\"relevance_rules\":{\"direct\":[\"RNA\"],\"indirect\":[],\"unrelated\":[\"General biology without nucleic acid chemistry.\"]},\"topic_taxonomy\":[],\"few_shots\":[]},\"changes\":[{\"id\":\"change-1\",\"section\":\"unrelated_rule\",\"operation\":\"merge\",\"summary\":\"Merge old unrelated residue.\",\"text_before\":[\"Cell biology without nucleic acid chemistry.\",\"Immunology without nucleic acid chemistry.\"],\"text_after\":[\"General biology without nucleic acid chemistry.\"],\"topic_before\":[],\"topic_after\":[],\"rationale\":\"This maintenance pass merges overlapping unrelated rules.\",\"source_feedback_ids\":[],\"source_paper_ids\":[],\"status\":\"proposed\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ProfileBaseURL = server.URL
	writeProfileFixture(t, settings.ProfilePath)

	result, err := GenerateProfileProposal(settings, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["proposal_id"] != int64(1) && result["proposal_id"] != 1 {
		t.Fatalf("unexpected maintenance proposal result: %#v", result)
	}
	if len(prompts) != 1 {
		t.Fatalf("expected 1 maintenance prompt, got %d", len(prompts))
	}
	if !strings.Contains(prompts[0], "maintenance mode because no new human feedback is available") {
		t.Fatalf("expected maintenance mode prompt, got %s", prompts[0])
	}

	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	item, err := sqliteStore.GetProfileProposal(1)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || len(item.SourceFeedbackIDs) != 0 {
		t.Fatalf("maintenance proposal should not consume feedback ids: %#v", item)
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
