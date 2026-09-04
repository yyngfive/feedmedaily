package classifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
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
	} {
		if !strings.Contains(userPrompts[0], fragment) {
			t.Fatalf("classification prompt missing %q: %s", fragment, userPrompts[0])
		}
	}
	for _, fragment := range []string{"decision_trace", "recommended_action"} {
		if strings.Contains(userPrompts[0], fragment) {
			t.Fatalf("classification prompt should omit %q: %s", fragment, userPrompts[0])
		}
	}
}

func TestManagedClassifierAdaptersSetProviderThinkingAndDoNotFallbackAcrossProviders(t *testing.T) {
	tests := []struct {
		name              string
		provider          string
		model             string
		wantType          string
		wantEffort        string
		wantDoSample      bool
		wantMaxCompletion bool
		failPrimary       bool
	}{
		{name: "deepseek", provider: "deepseek", model: "deepseek-v4-flash", wantType: "disabled"},
		{name: "glm", provider: "zhipu", model: "glm-5.3-flash", wantType: "enabled", wantEffort: "low", wantDoSample: true, failPrimary: true},
		{name: "qwen", provider: "qwen", model: "qwen3.8-flash", wantEffort: "none"},
		{name: "mimo", provider: "mimo", model: "mimo-v2.5", wantType: "disabled", wantMaxCompletion: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
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
				if test.failPrimary && len(requests) == 1 {
					http.Error(w, "thinking request timed out", http.StatusGatewayTimeout)
					return
				}
				if strings.Contains(userContent, "Translate each paper title") {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"translated_title_zh\":\"中文标题\"}]}"}}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.8,\"reason\":\"Relevant.\"}]}"}}]}`))
			}))
			defer server.Close()

			results, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA paper"}}, testProfile(), LLMConfig{
				APIKey: "key", Model: test.model, Provider: test.provider, BaseURL: server.URL, Thinking: "enabled",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 || results[0].TranslatedTitleZH == nil {
				t.Fatalf("unexpected classification result: %#v", results)
			}
			if len(requests) < 2 {
				t.Fatalf("expected classification and title translation requests, got %d", len(requests))
			}
			for index, request := range requests {
				thinking, hasThinking := request["thinking"].(map[string]any)
				if test.wantType == "" {
					if hasThinking {
						t.Fatalf("request %d must omit thinking: %#v", index, request)
					}
				} else if !hasThinking || thinking["type"] != test.wantType {
					t.Fatalf("request %d thinking = %#v, want %s", index, request["thinking"], test.wantType)
				}
				if test.wantEffort == "" {
					if _, exists := request["reasoning_effort"]; exists {
						t.Fatalf("request %d must not send reasoning_effort: %#v", index, request)
					}
				} else if request["reasoning_effort"] != test.wantEffort {
					t.Fatalf("request %d reasoning_effort = %#v", index, request["reasoning_effort"])
				}
				if test.wantDoSample && request["do_sample"] != false {
					t.Fatalf("GLM request %d do_sample = %#v", index, request["do_sample"])
				}
				if test.wantMaxCompletion {
					if _, exists := request["max_tokens"]; exists || request["max_completion_tokens"] == nil {
						t.Fatalf("MiMo request %d must use max_completion_tokens: %#v", index, request)
					}
				}
			}
			if test.failPrimary && len(requests) != 3 {
				t.Fatalf("GLM should retry once without disabling thinking, requests=%d", len(requests))
			}
		})
	}
}

func TestTestConnectionUsesManagedProviderPayload(t *testing.T) {
	tests := []struct {
		name              string
		provider          string
		model             string
		wantType          string
		wantEffort        string
		wantSample        *bool
		wantMaxCompletion bool
	}{
		{name: "deepseek", provider: "deepseek", model: "deepseek-v4-flash", wantType: "disabled"},
		{name: "glm", provider: "zhipu", model: "glm-5.3-flash", wantType: "enabled", wantEffort: "low", wantSample: func() *bool { value := false; return &value }()},
		{name: "qwen", provider: "qwen", model: "qwen3.8-flash", wantEffort: "none"},
		{name: "mimo", provider: "mimo", model: "mimo-v2.5", wantType: "disabled", wantMaxCompletion: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
			}))
			defer server.Close()

			if err := TestConnection(LLMConfig{APIKey: "key", Model: test.model, Provider: test.provider, BaseURL: server.URL, Thinking: "enabled"}); err != nil {
				t.Fatal(err)
			}
			thinking, hasThinking := request["thinking"].(map[string]any)
			if test.wantType == "" {
				if hasThinking {
					t.Fatalf("request must omit thinking: %#v", request)
				}
			} else if !hasThinking || thinking["type"] != test.wantType {
				t.Fatalf("thinking = %#v, want %s", request["thinking"], test.wantType)
			}
			if test.wantEffort == "" {
				if _, exists := request["reasoning_effort"]; exists {
					t.Fatalf("connection test must omit reasoning_effort: %#v", request)
				}
			} else if request["reasoning_effort"] != test.wantEffort {
				t.Fatalf("reasoning_effort = %#v", request["reasoning_effort"])
			}
			if test.wantSample != nil && request["do_sample"] != *test.wantSample {
				t.Fatalf("GLM do_sample = %#v", request["do_sample"])
			}
			if test.wantMaxCompletion {
				if _, exists := request["max_tokens"]; exists || request["max_completion_tokens"] == nil {
					t.Fatalf("MiMo connection test must use max_completion_tokens: %#v", request)
				}
			}
		})
	}
}

func TestTestConnectionRejectsFalseOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":false}"}}]}`))
	}))
	defer server.Close()
	if err := TestConnection(LLMConfig{APIKey: "key", Model: "model", BaseURL: server.URL}); err == nil {
		t.Fatal("expected false ok response to fail")
	}
}

func TestApplyProviderControlsAllowsExplicitExperimentOverrides(t *testing.T) {
	tests := []struct {
		name              string
		cfg               LLMConfig
		wantThinking      string
		wantEffort        string
		wantMaxCompletion bool
	}{
		{"deepseek low", LLMConfig{Provider: "deepseek", Model: "deepseek-v4-flash", Thinking: "enabled", ReasoningEffort: "low", UseConfiguredProviderControls: true}, "enabled", "low", false},
		{"qwen low", LLMConfig{Provider: "qwen", Model: "qwen3.8-flash", Thinking: "enabled", ReasoningEffort: "low", UseConfiguredProviderControls: true}, "", "low", false},
		{"qwen none", LLMConfig{Provider: "qwen", Model: "qwen3.8-flash", Thinking: "disabled", ReasoningEffort: "none", UseConfiguredProviderControls: true}, "", "none", false},
		{"mimo enabled", LLMConfig{Provider: "mimo", Model: "mimo-v2.5", Thinking: "enabled", ReasoningEffort: "low", UseConfiguredProviderControls: true}, "enabled", "", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := map[string]any{"max_tokens": 600}
			applyProviderControls(test.cfg, payload, false)
			thinking, hasThinking := payload["thinking"].(map[string]string)
			if test.wantThinking == "" {
				if hasThinking {
					t.Fatalf("unexpected thinking: %#v", payload)
				}
			} else if !hasThinking || thinking["type"] != test.wantThinking {
				t.Fatalf("thinking = %#v", payload["thinking"])
			}
			if test.wantEffort == "" {
				if _, ok := payload["reasoning_effort"]; ok {
					t.Fatalf("unexpected effort: %#v", payload)
				}
			} else if payload["reasoning_effort"] != test.wantEffort {
				t.Fatalf("effort = %#v", payload["reasoning_effort"])
			}
			_, hasMax := payload["max_completion_tokens"]
			if hasMax != test.wantMaxCompletion {
				t.Fatalf("max completion = %v, payload=%#v", hasMax, payload)
			}
		})
	}
}

func TestClassifyPapersUsesThinkingTokenFloorForDeepSeekAndMiMo(t *testing.T) {
	for _, test := range []struct {
		name, provider, model string
		wantMaxField          string
	}{
		{"deepseek", "deepseek", "deepseek-v4-flash", "max_tokens"},
		{"mimo", "mimo", "mimo-v2.5", "max_completion_tokens"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer r.Body.Close()
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"translated_title_zh\":\"中文标题\"}]}"}}]}`))
			}))
			defer server.Close()

			_, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA paper"}}, testProfile(), LLMConfig{
				APIKey: "key", Model: test.model, Provider: test.provider, BaseURL: server.URL,
				Thinking: "enabled", ReasoningEffort: "low", UseConfiguredProviderControls: true,
				MinMaxTokens: 4096,
			})
			if err != nil {
				t.Fatal(err)
			}
			if request[test.wantMaxField] != float64(4096) {
				t.Fatalf("%s = %#v, want 4096; payload=%#v", test.wantMaxField, request[test.wantMaxField], request)
			}
			if test.wantMaxField == "max_completion_tokens" {
				if _, ok := request["max_tokens"]; ok {
					t.Fatalf("MiMo payload must omit max_tokens: %#v", request)
				}
			}
			thinking := request["thinking"].(map[string]any)
			if thinking["type"] != "enabled" {
				t.Fatalf("thinking = %#v", request["thinking"])
			}
		})
	}
}

func TestClassifyPapersUsesCompactOutputAndDerivesRecommendedActions(t *testing.T) {
	userPrompt := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		messages := payload["messages"].([]any)
		userPrompt = messages[1].(map[string]any)["content"].(string)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Direct match.\",\"translated_title_zh\":\"直接相关\"},{\"id\":\"2\",\"relevance\":\"indirect\",\"confidence\":0.7,\"reason\":\"Useful context.\",\"translated_title_zh\":\"间接相关\"},{\"id\":\"3\",\"relevance\":\"unrelated\",\"confidence\":0.8,\"reason\":\"Outside scope.\",\"translated_title_zh\":\"不相关\"}]}"}}]}`))
	}))
	defer server.Close()

	results, err := ClassifyPapers([]store.Paper{
		{ID: 1, Title: "Direct"},
		{ID: 2, Title: "Indirect"},
		{ID: 3, Title: "Unrelated"},
	}, testProfile(), LLMConfig{APIKey: "key", Model: "model", BaseURL: server.URL, Thinking: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(userPrompt, "decision_trace") {
		t.Fatalf("classification prompt should omit decision_trace: %s", userPrompt)
	}
	if strings.Contains(userPrompt, "recommended_action") {
		t.Fatalf("classification prompt should omit recommended_action: %s", userPrompt)
	}
	wantActions := []string{"read", "scan", "skip"}
	for index, want := range wantActions {
		if results[index].RecommendedAction != want {
			t.Fatalf("result %d recommended action = %q, want %q", index, results[index].RecommendedAction, want)
		}
		if results[index].Reason == "" {
			t.Fatalf("result %d should retain reason", index)
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
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":["}}],"usage":{"prompt_tokens":11,"prompt_cache_hit_tokens":3,"prompt_cache_miss_tokens":8,"completion_tokens":5,"total_tokens":16}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"indirect\",\"confidence\":0.7,\"reason\":\"Useful context.\",\"recommended_action\":\"scan\",\"translated_title_zh\":\"RNA 方法\"}]}"}}],"usage":{"prompt_tokens":13,"prompt_cache_hit_tokens":4,"prompt_cache_miss_tokens":9,"completion_tokens":7,"total_tokens":20}}`))
	}))
	defer server.Close()

	usage := llmusage.NewCollector()
	results, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA method"}}, testProfile(), LLMConfig{
		APIKey:  "key",
		Model:   "model",
		BaseURL: server.URL,
		Usage:   usage,
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
	summary := usage.Summary()
	if summary.RequestCount != 2 || summary.PromptCacheHitTokens != 7 || summary.PromptCacheMissTokens != 17 || summary.CompletionTokens != 12 {
		t.Fatalf("usage summary = %#v", summary)
	}
}

func TestClassifyPapersReadsOpenAIStyleCachedTokenDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"direct\",\"confidence\":0.9,\"reason\":\"Relevant.\",\"translated_title_zh\":\"RNA 方法\"}]}"}}],"usage":{"prompt_tokens":13,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens":7,"total_tokens":20}}`))
	}))
	defer server.Close()

	usage := llmusage.NewCollector()
	_, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "RNA method"}}, testProfile(), LLMConfig{
		APIKey: "key", Model: "glm-5.3-flash", BaseURL: server.URL, Usage: usage,
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := usage.Summary()
	if summary.PromptCacheHitTokens != 4 || summary.PromptCacheMissTokens != 9 || summary.CompletionTokens != 7 {
		t.Fatalf("OpenAI-style usage summary = %#v", summary)
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

func TestOpenCodeZenProviderUsesClientUserAgent(t *testing.T) {
	var capturedUserAgent string
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserAgent = r.Header.Get("User-Agent")
		capturedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"indirect\",\"confidence\":0.7,\"reason\":\"ok\"}]}"}}]}`))
	}))
	defer server.Close()

	_, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "A"}}, testProfile(), LLMConfig{
		APIKey:                        config.OpenCodeZenPublicAPIKey,
		Model:                         "mimo-v2.5-free",
		BaseURL:                       server.URL,
		Provider:                      "opencode",
		Thinking:                      "disabled",
		UseConfiguredProviderControls: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedUserAgent != opencodeZenUserAgent {
		t.Fatalf("OpenCode Zen requests must carry the client User-Agent, got %q", capturedUserAgent)
	}
	if capturedAuth != "Bearer "+config.OpenCodeZenPublicAPIKey {
		t.Fatalf("unexpected auth header %q", capturedAuth)
	}
}

func TestNonOpenCodeProviderKeepsDefaultUserAgent(t *testing.T) {
	var capturedUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"id\":\"1\",\"relevance\":\"unrelated\",\"confidence\":0.7,\"reason\":\"ok\"}]}"}}]}`))
	}))
	defer server.Close()

	_, err := ClassifyPapers([]store.Paper{{ID: 1, Title: "A"}}, testProfile(), LLMConfig{
		APIKey:                        "key",
		Model:                         "deepseek-v4-flash",
		BaseURL:                       server.URL,
		Provider:                      "deepseek",
		Thinking:                      "disabled",
		UseConfiguredProviderControls: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedUserAgent != "Go-http-client/1.1" {
		t.Fatalf("non-OpenCode requests must keep the default User-Agent, got %q", capturedUserAgent)
	}
}
