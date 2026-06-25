package classifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestClassifyPapersWithTranslationFallback(t *testing.T) {
	requests := []map[string]any{}
	userPrompts := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		messages := payload["messages"].([]any)
		userContent := messages[1].(map[string]any)["content"].(string)
		userPrompts = append(userPrompts, userContent)
		if strings.Contains(userContent, "Translate each paper title") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"translated_title_zh\":\"中文标题\"}]}"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.8,\"reason\":\"Relevant.\",\"recommended_action\":\"read\",\"decision_trace\":{\"exclusion_check\":\"No unrelated exclusion fits.\",\"direct_check\":\"Direct rule fits.\",\"indirect_check\":\"Not needed after direct.\",\"priority_resolution\":\"Direct after exclusion check.\"}}]}"}}]}`))
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
		"few_shots": []any{
			map[string]any{"title": "Broad example", "relevance": "direct", "rationale": "Example only"},
		},
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
	if len(userPrompts) == 0 {
		t.Fatalf("expected at least one captured user prompt")
	}
	if strings.Contains(userPrompts[0], "few_shots") {
		t.Fatalf("classification prompt should not include few_shots: %s", userPrompts[0])
	}
	if strings.Contains(userPrompts[0], "topic_taxonomy") {
		t.Fatalf("classification prompt should not include topic_taxonomy: %s", userPrompts[0])
	}
	if !strings.Contains(userPrompts[0], "Judge relevance from the current scope and relevance rules only.") {
		t.Fatalf("classification prompt missing rule-only guidance: %s", userPrompts[0])
	}
	for _, fragment := range []string{
		"Apply unrelated exclusion rules first",
		"If not excluded, choose direct only",
		"If not direct, choose indirect only",
		"If uncertain between indirect and unrelated, choose unrelated",
		"decision_trace",
		"exclusion_check",
		"direct_check",
		"indirect_check",
		"priority_resolution",
	} {
		if !strings.Contains(userPrompts[0], fragment) {
			t.Fatalf("classification prompt missing %q: %s", fragment, userPrompts[0])
		}
	}
}

func TestClassifyPapersFallbackDisablesThinkingAndClearsTopicTags(t *testing.T) {
	requests := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, payload)
		if len(requests) == 1 {
			http.Error(w, "provider timeout while using thinking mode", http.StatusGatewayTimeout)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"RNA 论文\",\"decision_trace\":{\"exclusion_check\":\"No exclusion.\",\"direct_check\":\"Direct rule applies.\",\"indirect_check\":\"Not needed.\",\"priority_resolution\":\"Direct.\"}},{\"id\":\"2\",\"relevance\":\"unrelated\",\"confidence\":0.2,\"reason\":\"Not relevant.\",\"recommended_action\":\"skip\",\"translated_title_zh\":\"无关论文\",\"decision_trace\":{\"exclusion_check\":\"Unrelated exclusion applies.\",\"direct_check\":\"Skipped after exclusion.\",\"indirect_check\":\"Skipped after exclusion.\",\"priority_resolution\":\"Unrelated veto.\"}}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{
		{ID: 1, Title: "RNA paper"},
		{ID: 2, Title: "Irrelevant paper"},
	}, map[string]any{
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
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	if requests[0]["thinking"].(map[string]any)["type"] != "enabled" {
		t.Fatalf("unexpected first request thinking: %#v", requests[0]["thinking"])
	}
	if requests[1]["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatalf("unexpected fallback thinking: %#v", requests[1]["thinking"])
	}
	if len(results[0].TopicTags) != 0 {
		t.Fatalf("expected empty topic tags for direct paper, got %#v", results[0].TopicTags)
	}
	if len(results[1].TopicTags) != 0 {
		t.Fatalf("expected empty topic tags for unrelated paper, got %#v", results[1].TopicTags)
	}
	if _, ok := reflect.TypeOf(results[0]).FieldByName("DecisionTrace"); ok {
		t.Fatalf("decision_trace should not be exposed on stored classification")
	}
}

func TestClassifyPapersRetriesTimeoutThenSucceeds(t *testing.T) {
	previousClient := classifierHTTPClient
	classifierHTTPClient = &http.Client{Timeout: 20 * time.Millisecond}
	defer func() { classifierHTTPClient = previousClient }()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			time.Sleep(100 * time.Millisecond)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"recommended_action\":\"read\",\"translated_title_zh\":\"RNA 论文\"}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA paper"}}, testProfile(), LLMConfig{
		APIKey:  "key",
		Model:   "model",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected timeout retry, got %d requests", requests)
	}
	if len(results) != 1 || results[0].Relevance != "direct" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestClassifyPapersRetriesMalformedContentJSON(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":["}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"indirect\",\"confidence\":0.7,\"reason\":\"Useful context.\",\"recommended_action\":\"scan\",\"translated_title_zh\":\"RNA 方法\"}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA method"}}, testProfile(), LLMConfig{
		APIKey:  "key",
		Model:   "model",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("expected malformed content retry, got %d requests", requests)
	}
	if len(results) != 1 || results[0].Relevance != "indirect" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestClassifyPapersContinuesWhenTitleTranslationFails(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requests++
		messages := payload["messages"].([]any)
		userContent := messages[1].(map[string]any)["content"].(string)
		if strings.Contains(userContent, "Translate each paper title") {
			http.Error(w, "temporary translation failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"recommended_action\":\"read\"}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA paper"}}, testProfile(), LLMConfig{
		APIKey:  "key",
		Model:   "model",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("expected classification plus translation retries, got %d requests", requests)
	}
	if len(results) != 1 || results[0].TranslatedTitleZH != nil {
		t.Fatalf("expected classification without translated title, got %#v", results)
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

func testProfile() map[string]any {
	return map[string]any{
		"scope": "RNA biology",
		"relevance_rules": map[string]any{
			"direct":    []any{"RNA"},
			"indirect":  []any{},
			"unrelated": []any{},
		},
	}
}
