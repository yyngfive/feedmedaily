package profile

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
)

func TestRequestProfileModelJSONRecordsOpenAICacheBreakdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":3}}}`))
	}))
	defer server.Close()

	collector := llmusage.NewCollector()
	settings := config.Settings{
		ProfileAPIKey:   "test-key",
		ProfileBaseURL:  server.URL,
		ProfileModel:    "glm-5.3",
		ProfileProvider: "zhipu",
		ProfileThinking: "disabled",
	}
	content, err := requestProfileModelJSONWithUsage(settings, "system", "user", 100, collector, "test_op")
	if err != nil {
		t.Fatal(err)
	}
	if content != "{\"ok\":true}" {
		t.Fatalf("content = %q", content)
	}

	summary := collector.Summary()
	if summary.RequestCount != 1 || summary.PromptCacheHitTokens != 40 || summary.PromptCacheMissTokens != 60 {
		t.Fatalf("OpenAI-style cache details were not recorded: %#v", summary)
	}
}
