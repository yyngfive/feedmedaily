package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yyngfive/scirssagent/internal/classifier"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
)

type classifierModelTestRequest struct {
	ModelID string `json:"model_id"`
	APIKey  string `json:"api_key"`
}

// testClassifierConnectionFunc is a narrow seam for API tests; production uses the real adapter.
var testClassifierConnectionFunc = classifier.TestConnection

func (s *Server) handleClassifierModelTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload classifierModelTestRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	settings := s.snapshotSettings()
	model, err := config.ClassifierModelConfigForID(settings, strings.TrimSpace(payload.ModelID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if key := strings.TrimSpace(payload.APIKey); key != "" {
		model.APIKey = key
	}
	if strings.TrimSpace(model.APIKey) == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("API key is required for classifier model %s.", model.ID))
		return
	}
	// Preserve the tested model in usage summaries even if a provider omits its usage block.
	jobSettings := settings
	jobSettings.ClassifierModels.DefaultModelID = model.ID
	jobSettings.ClassifierAPIKey = model.APIKey
	jobSettings.ClassifierModel = model.ID
	jobSettings.ClassifierBaseURL = model.BaseURL
	jobSettings.ClassifierThinking = model.Thinking
	minMaxTokens := 0
	if model.Thinking == "enabled" && (model.Provider == "deepseek" || model.Provider == "mimo") {
		minMaxTokens = classifier.ThinkingMaxTokensFloor
	}

	job := launchLocalJob(
		jobSettings,
		"model-test",
		"model.test.queued",
		"Classifier connection test queued.",
		"model.test.running",
		fmt.Sprintf("Testing %s.", model.Label),
		func(_ context.Context, progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
			_ = progress
			err := testClassifierConnectionFunc(classifier.LLMConfig{
				APIKey:                        model.APIKey,
				Model:                         model.ID,
				BaseURL:                       model.BaseURL,
				Provider:                      model.Provider,
				Thinking:                      model.Thinking,
				ReasoningEffort:               model.ReasoningEffort,
				UseConfiguredProviderControls: true,
				MinMaxTokens:                  minMaxTokens,
				Usage:                         usage,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"model_id": model.ID, "provider": model.Provider}, nil
		},
		nil,
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}
