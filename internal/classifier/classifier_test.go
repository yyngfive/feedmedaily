package classifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestClassifyPapersWithTranslationFallback(t *testing.T) {
	requests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		messages := payload["messages"].([]any)
		userContent := messages[1].(map[string]any)["content"].(string)
		if strings.Contains(userContent, "Translate each paper title") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"translated_title_zh\":\"中文标题\"}]}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.8,\"topic_tags\":[\"rna_bio\"],\"reason\":\"Relevant.\",\"recommended_action\":\"read\"}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{{ID: 11, Title: "RNA paper"}}, map[string]any{
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
		"topic_taxonomy": []any{map[string]any{"id": "rna_bio", "label": "RNA Bio"}},
		"few_shots":      []any{},
	}, LLMConfig{
		APIKey:   "test-key",
		Model:    "test-model",
		BaseURL:  server.URL,
		Thinking: "enabled",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].TranslatedTitleZH == nil || *results[0].TranslatedTitleZH != "中文标题" {
		t.Fatalf("unexpected classification results: %#v", results)
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	if requests[0]["model"] != "test-model" || requests[0]["temperature"].(float64) != 0 {
		t.Fatalf("unexpected classification request: %#v", requests[0])
	}
	if requests[0]["max_tokens"].(float64) != 600 {
		t.Fatalf("unexpected classification max_tokens: %#v", requests[0]["max_tokens"])
	}
	if requests[0]["thinking"].(map[string]any)["type"] != "enabled" {
		t.Fatalf("unexpected classifier thinking: %#v", requests[0]["thinking"])
	}
	if requests[1]["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatalf("unexpected translation thinking: %#v", requests[1]["thinking"])
	}
}

func TestClassifyPapersMissingIDFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[]}"}}]}`))
	}))
	defer server.Close()

	_, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "A"}}, map[string]any{
		"scope":           "scope",
		"relevance_rules": map[string]any{"direct": []any{}, "indirect": []any{}, "unrelated": []any{}},
		"topic_taxonomy":  []any{},
	}, LLMConfig{APIKey: "key", Model: "model", BaseURL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "missing papers") {
		t.Fatalf("expected missing papers error, got %v", err)
	}
}
