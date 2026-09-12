package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/profile"
)

type profileModelTestRequest struct {
	ModelID string `json:"model_id"`
}

// testProfileConnectionFunc is a narrow seam for API tests; production uses the real adapter.
var testProfileConnectionFunc = profile.TestConnection

func (s *Server) handleProfileModelTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload profileModelTestRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	settings := s.snapshotSettings()
	model, err := config.ProfileModelForID(settings, strings.TrimSpace(payload.ModelID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Run the test against the selected model even when it is not the current default.
	jobSettings := settings
	jobSettings.ProfileModel = model.ID
	jobSettings.ProfileBaseURL = model.BaseURL
	jobSettings.ProfileAPIKey = model.APIKey
	jobSettings.ProfileProvider = model.Provider

	job := launchLocalJob(
		jobSettings,
		"profile-model-test",
		"model.profile_test.queued",
		"Profile model connection test queued.",
		"model.profile_test.running",
		fmt.Sprintf("Testing %s.", model.Label),
		func(_ context.Context, progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
			_ = progress
			if err := testProfileConnectionFunc(jobSettings, usage); err != nil {
				return nil, err
			}
			return map[string]any{"model_id": model.ID, "provider": model.Provider}, nil
		},
		nil,
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}
