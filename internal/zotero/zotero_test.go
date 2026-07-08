package zotero

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestListCollectionsBuildsPathLabelsAndDefaultFlags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/123/collections/top":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"key": "PARENT", "data": map[string]any{"name": "Inbox"}},
			})
		case "/users/123/collections/PARENT/collections":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"key": "CHILD", "data": map[string]any{"name": "RNA"}},
			})
		case "/users/123/collections/CHILD/collections":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
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
	if payload.Collections[0].PathLabel != "Inbox" || payload.Collections[0].Depth != 0 {
		t.Fatalf("unexpected parent payload: %#v", payload.Collections)
	}
	if payload.Collections[1].PathLabel != "Inbox / RNA" || payload.Collections[1].Depth != 1 || !payload.Collections[1].IsDefault {
		t.Fatalf("unexpected collections payload: %#v", payload.Collections)
	}
}

func TestListCollectionsReadsAllPages(t *testing.T) {
	requestStarts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/123/collections/top" {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		requestStarts = append(requestStarts, r.URL.Query().Get("start"))
		items := []map[string]any{}
		if r.URL.Query().Get("start") == "0" {
			for index := 0; index < 100; index++ {
				key := fmt.Sprintf("COLL-%03d", index)
				items = append(items, map[string]any{"key": key, "data": map[string]any{"name": key}})
			}
		} else {
			items = append(items, map[string]any{"key": "LAST", "data": map[string]any{"name": "Last"}})
		}
		_ = json.NewEncoder(w).Encode(items)
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

	payload, err := ListCollections(config.Settings{ZoteroAPIKey: "key", ZoteroLibraryType: "user", ZoteroLibraryID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Collections) != 101 {
		t.Fatalf("collections = %d", len(payload.Collections))
	}
	if len(requestStarts) != 2 || requestStarts[0] != "0" || requestStarts[1] != "100" {
		t.Fatalf("request starts = %#v", requestStarts)
	}
}

func TestListCollectionsUsesRecursiveParentHierarchy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/123/collections/top":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"key": "XNA", "data": map[string]any{"name": "XNA"}},
			})
		case "/users/123/collections/XNA/collections":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"key": "DNA", "data": map[string]any{"name": "dna walker"}},
				{"key": "RNA", "data": map[string]any{"name": "RNA MB"}},
			})
		case "/users/123/collections/DNA/collections", "/users/123/collections/RNA/collections":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
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

	payload, err := ListCollections(config.Settings{ZoteroAPIKey: "key", ZoteroLibraryType: "user", ZoteroLibraryID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Collections) != 3 {
		t.Fatalf("collections = %#v", payload.Collections)
	}
	if payload.Collections[1].PathLabel != "XNA / dna walker" || payload.Collections[1].Depth != 1 {
		t.Fatalf("unexpected child path: %#v", payload.Collections)
	}
	if payload.Collections[2].PathLabel != "XNA / RNA MB" || payload.Collections[2].ParentKey == nil || *payload.Collections[2].ParentKey != "XNA" {
		t.Fatalf("unexpected child parent: %#v", payload.Collections)
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
		Authors:       []string{"Alice Smith", "Jones, Bob", "The RNA Consortium"},
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
	if !ok || len(tags) != 2 {
		t.Fatalf("unexpected tags: %#v", captured[0]["tags"])
	}
	if captured[0]["date"] != "2026-05-16" {
		t.Fatalf("unexpected date: %#v", captured[0]["date"])
	}
	creators, ok := captured[0]["creators"].([]any)
	if !ok || len(creators) != 3 {
		t.Fatalf("unexpected creators: %#v", captured[0]["creators"])
	}
	if creators[0].(map[string]any)["firstName"] != "Alice" || creators[0].(map[string]any)["lastName"] != "Smith" {
		t.Fatalf("unexpected first creator: %#v", creators[0])
	}
	if creators[1].(map[string]any)["firstName"] != "Bob" || creators[1].(map[string]any)["lastName"] != "Jones" {
		t.Fatalf("unexpected second creator: %#v", creators[1])
	}
	if creators[2].(map[string]any)["name"] != "The RNA Consortium" {
		t.Fatalf("unexpected organization creator: %#v", creators[2])
	}
}

func stringPtr(value string) *string {
	return &value
}
