package zotero

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestListCollectionsBuildsPathLabelsAndDefaultFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/123/collections" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"key": "PARENT", "data": map[string]any{"name": "Inbox"}},
			{"key": "CHILD", "data": map[string]any{"name": "RNA", "parentCollection": "PARENT"}},
		})
	}))
	defer server.Close()

	previousBaseURL := apiBaseURL
	previousClient := httpClient
	apiBaseURL = server.URL
	httpClient = server.Client()
	defer func() {
		apiBaseURL = previousBaseURL
		httpClient = previousClient
	}()

	payload, err := ListCollections(config.Settings{
		ZoteroAPIKey:        "key",
		ZoteroLibraryType:   "user",
		ZoteroLibraryID:     "123",
		ZoteroCollectionKey: "CHILD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Collections) != 2 {
		t.Fatalf("collections = %#v", payload.Collections)
	}
	if payload.DefaultCollectionKey == nil || *payload.DefaultCollectionKey != "CHILD" {
		t.Fatalf("default collection key = %#v", payload.DefaultCollectionKey)
	}
	if payload.Collections[0].PathLabel != "Inbox" || payload.Collections[1].PathLabel != "Inbox / RNA" || !payload.Collections[1].IsDefault {
		t.Fatalf("unexpected collections payload: %#v", payload.Collections)
	}
}

func TestSavePaperBuildsExpectedPayload(t *testing.T) {
	var captured []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/groups/456/items" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"successful": map[string]any{
				"0": map[string]any{"key": "ITEM-9"},
			},
		})
	}))
	defer server.Close()

	previousBaseURL := apiBaseURL
	previousClient := httpClient
	apiBaseURL = server.URL
	httpClient = server.Client()
	defer func() {
		apiBaseURL = previousBaseURL
		httpClient = previousClient
	}()

	collectionKey := "COLL-1"
	itemKey, err := SavePaper(config.Settings{
		ZoteroAPIKey:      "key",
		ZoteroLibraryType: "group",
		ZoteroLibraryID:   "456",
	}, store.Paper{
		Title:         "Interesting Paper",
		URL:           "https://example.com/paper",
		Journal:       stringPtr("Nature"),
		DOI:           stringPtr("10.1000/test"),
		FeedTitle:     stringPtr("Feed"),
		PublishedDate: stringPtr("2026-05-16"),
		Authors:       []string{"Alice Smith", "Bob Jones"},
		Abstract:      stringPtr("Abstract"),
	}, store.Classification{
		Relevance: "direct",
		TopicTags: []string{"rna", "crispr"},
	}, &collectionKey)
	if err != nil {
		t.Fatal(err)
	}
	if itemKey == nil || *itemKey != "ITEM-9" {
		t.Fatalf("item key = %#v", itemKey)
	}
	if len(captured) != 1 || captured[0]["title"] != "Interesting Paper" {
		t.Fatalf("captured payload = %#v", captured)
	}
	collections, ok := captured[0]["collections"].([]any)
	if !ok || len(collections) != 1 || collections[0] != "COLL-1" {
		t.Fatalf("unexpected collections field: %#v", captured[0]["collections"])
	}
	tags, ok := captured[0]["tags"].([]any)
	if !ok || len(tags) < 3 {
		t.Fatalf("unexpected tags: %#v", captured[0]["tags"])
	}
}

func stringPtr(value string) *string {
	return &value
}
