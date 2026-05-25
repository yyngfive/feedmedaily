package jobs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yyngfive/scirssagent/internal/feeds"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestRunSyncBuildsSummaryWithoutDiskReportArtifacts(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(settings.WebDistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WebDistDir, "index.html"), []byte(`<html><head><link rel="stylesheet" href="./assets/app.css"></head><body><script type="module" src="./assets/app.js"></script></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(settings.WebDistDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WebDistDir, "assets", "app.css"), []byte(`body{color:black;}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.WebDistDir, "assets", "app.js"), []byte(`console.log("ok")`), 0o644); err != nil {
		t.Fatal(err)
	}

	previousFetch := fetchAllFeedsFunc
	defer func() { fetchAllFeedsFunc = previousFetch }()
	fetchAllFeedsFunc = func(_ string, _ feeds.FetchOptions) (feeds.FetchResult, error) {
		return feeds.FetchResult{
			Papers: []store.Paper{
				{
					SourceURL:      "https://example.com/rss",
					Title:          "Go migrated paper",
					URL:            "https://example.com/paper-1",
					DOI:            testStringPtr("10.1000/sync"),
					Journal:        testStringPtr("Nature"),
					Abstract:       testStringPtr("Ready for classification."),
					AbstractSource: "openalex",
					Raw:            map[string]any{"guid": "abc"},
				},
			},
			Fetched: 1,
			Errors:  []string{},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.95,\"reason\":\"Matches scope.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"迁移论文\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ClassifierBaseURL = server.URL

	summary, err := RunSync(settings, RunOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Fetched != 1 || summary.Inserted != 1 || summary.Classified != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if _, err := os.Stat(filepath.Join(settings.ReportsDir, "data", "latest.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no latest.json artifact, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(settings.ReportsDir, "latest", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("expected no static report artifact, got err=%v", err)
	}
}

func TestRunSyncDoesNotExpandProfileTaxonomyAndClearsTopicTags(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],"few_shots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	previousFetch := fetchAllFeedsFunc
	defer func() { fetchAllFeedsFunc = previousFetch }()
	fetchAllFeedsFunc = func(_ string, _ feeds.FetchOptions) (feeds.FetchResult, error) {
		return feeds.FetchResult{
			Papers: []store.Paper{
				{
					SourceURL:      "https://example.com/rss",
					Title:          "RNA CRISPR paper",
					URL:            "https://example.com/paper-1",
					Journal:        testStringPtr("Nature"),
					Abstract:       testStringPtr("RNA and CRISPR regulation."),
					AbstractSource: "rss",
					Raw:            map[string]any{"guid": "abc"},
				},
				{
					SourceURL:      "https://example.com/rss",
					Title:          "Gene regulation paper",
					URL:            "https://example.com/paper-2",
					Journal:        testStringPtr("Science"),
					Abstract:       testStringPtr("Gene regulation pathway."),
					AbstractSource: "rss",
					Raw:            map[string]any{"guid": "def"},
				},
			},
			Fetched: 2,
			Errors:  []string{},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.95,\"reason\":\"Matches scope.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"RNA CRISPR 论文\"},{\"id\":\"2\",\"relevance\":\"indirect\",\"confidence\":0.7,\"reason\":\"Partially relevant.\",\"recommended_action\":\"scan\",\"translated_title_zh\":\"基因调控论文\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ClassifierBaseURL = server.URL

	summary, err := RunSync(settings, RunOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Fetched != 2 || summary.Inserted != 2 || summary.Classified != 2 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	profileBytes, err := os.ReadFile(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	expectedProfile := `{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],"few_shots":[]}`
	if string(profileBytes) != expectedProfile {
		t.Fatalf("profile should remain unchanged, got %s", string(profileBytes))
	}

	sqliteStore, err := store.Open(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	first, err := sqliteStore.LatestClassification(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sqliteStore.LatestClassification(2)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil {
		t.Fatalf("missing classifications: %#v %#v", first, second)
	}
	if len(first.TopicTags) != 0 {
		t.Fatalf("unexpected first topic tags: %#v", first.TopicTags)
	}
	if len(second.TopicTags) != 0 {
		t.Fatalf("unexpected second topic tags: %#v", second.TopicTags)
	}
}

func TestRunSyncLeavesProfileUntouchedWhenClassifierReturnsNoTags(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	initialProfile := `{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[{"id":"rna_bio","label":"RNA Bio"}],"few_shots":[]}`
	if err := os.WriteFile(settings.ProfilePath, []byte(initialProfile), 0o644); err != nil {
		t.Fatal(err)
	}

	previousFetch := fetchAllFeedsFunc
	defer func() { fetchAllFeedsFunc = previousFetch }()
	fetchAllFeedsFunc = func(_ string, _ feeds.FetchOptions) (feeds.FetchResult, error) {
		return feeds.FetchResult{
			Papers: []store.Paper{{
				SourceURL:      "https://example.com/rss",
				Title:          "RNA paper",
				URL:            "https://example.com/paper-1",
				Journal:        testStringPtr("Nature"),
				Abstract:       testStringPtr("RNA paper."),
				AbstractSource: "rss",
				Raw:            map[string]any{"guid": "abc"},
			}},
			Fetched: 1,
			Errors:  []string{},
		}, nil
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.95,\"reason\":\"Matches scope.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"RNA 论文\"}]}"}}]}`))
	}))
	defer server.Close()
	settings.ClassifierBaseURL = server.URL

	if _, err := RunSync(settings, RunOptions{}, nil); err != nil {
		t.Fatal(err)
	}

	updatedProfile, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		t.Fatal(err)
	}
	meta := updatedProfile["meta"].(map[string]any)
	if meta["version"] != float64(1) {
		t.Fatalf("expected profile version to stay 1, got %#v", meta)
	}
}

func TestRunSyncReturnsVerificationRequiredWhenFeedNeedsManualCheck(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	previousFetch := fetchAllFeedsFunc
	defer func() { fetchAllFeedsFunc = previousFetch }()
	fetchAllFeedsFunc = func(_ string, _ feeds.FetchOptions) (feeds.FetchResult, error) {
		return feeds.FetchResult{
			VerificationRequests: []feeds.VerificationRequest{{
				URL:    "https://chemrxiv.org/action/showFeed?type=latest&format=rss",
				Target: "cloudflare",
				Reason: "challenge",
			}},
		}, nil
	}

	_, err := RunSync(settings, RunOptions{}, nil)
	var verificationErr *VerificationRequiredError
	if !errors.As(err, &verificationErr) {
		t.Fatalf("expected verification error, got %v", err)
	}
	if len(verificationErr.Requests) != 1 || verificationErr.Requests[0].Target != "cloudflare" {
		t.Fatalf("unexpected verification requests: %#v", verificationErr.Requests)
	}
}

func TestRunSyncTreatsSkippedVerificationWarningsAsNonFatal(t *testing.T) {
	root := t.TempDir()
	settings := testJobSettings(root)
	if err := os.MkdirAll(filepath.Dir(settings.ProfilePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings.ProfilePath, []byte(`{"meta":{"name":"Test","version":1,"created_at":"2026-05-16T00:00:00Z","updated_at":"2026-05-16T00:00:00Z","source_description":"test"},"scope":"RNA biology","relevance_rules":{"direct":["RNA"],"indirect":[],"unrelated":[]},"topic_taxonomy":[],"few_shots":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	previousFetch := fetchAllFeedsFunc
	defer func() { fetchAllFeedsFunc = previousFetch }()
	fetchAllFeedsFunc = func(_ string, _ feeds.FetchOptions) (feeds.FetchResult, error) {
		return feeds.FetchResult{
			Errors: []string{
				"https://chemrxiv.org/action/showFeed?type=latest&format=rss: manual Cloudflare feed verification is only supported on Windows",
			},
		}, nil
	}

	summary, err := RunSync(settings, RunOptions{
		SkippedFeeds: map[string]string{
			"https://chemrxiv.org/action/showFeed?type=latest&format=rss": "manual Cloudflare feed verification is only supported on Windows",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Errors) != 1 {
		t.Fatalf("expected one warning in summary, got %#v", summary)
	}
}

func testStringPtr(value string) *string {
	return &value
}
