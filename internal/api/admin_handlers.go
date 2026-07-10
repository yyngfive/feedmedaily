package api

import (
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"io"
	"net/http"
	"strings"
)

func (s *Server) handleAdminRun(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		FeedURLs []string `json:"feed_urls"`
	}
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if strings.TrimSpace(string(data)) != "" {
			if err := json.Unmarshal(data, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "Invalid JSON body.")
				return
			}
		}
	}
	selectedFeedURLs, err := validateSelectedFeedURLs(s.settings.FeedsPath, payload.FeedURLs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job := launchVerificationAwareSyncJob(
		s.settings,
		func(progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string, verifyHost feeds.VerifyHostFunc) (map[string]any, error) {
			summary, err := runSyncFunc(s.settings, jobruntime.RunOptions{
				SelectedFeedURLs:  selectedFeedURLs,
				FeedBodyOverrides: overrides,
				SkippedFeeds:      skippedFeeds,
				VerifyFeedHost:    verifyHost,
			}, progress)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"fetched":    summary.Fetched,
				"inserted":   summary.Inserted,
				"updated":    summary.Updated,
				"classified": summary.Classified,
				"errors":     summary.Errors,
			}, nil
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func validateSelectedFeedURLs(feedsPath string, requested []string) ([]string, error) {
	selected := make([]string, 0, len(requested))
	seen := map[string]struct{}{}
	for _, rawURL := range requested {
		feedURL := strings.TrimSpace(rawURL)
		if feedURL == "" {
			continue
		}
		if _, ok := seen[feedURL]; ok {
			continue
		}
		seen[feedURL] = struct{}{}
		selected = append(selected, feedURL)
	}
	if len(selected) == 0 {
		return nil, nil
	}
	subscriptions, err := feeds.ReadSubscriptions(feedsPath)
	if err != nil {
		return nil, err
	}
	saved := map[string]struct{}{}
	for _, subscription := range subscriptions {
		saved[strings.TrimSpace(subscription.URL)] = struct{}{}
	}
	for _, feedURL := range selected {
		if _, ok := saved[feedURL]; !ok {
			return nil, fmt.Errorf("feed_urls contains an unknown feed URL: %s", feedURL)
		}
	}
	return selected, nil
}

func (s *Server) handleAdminReclassify(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(payload.Scope) == "" {
		payload.Scope = "recent"
	}
	if payload.Scope != "recent" && payload.Scope != "feedback" && payload.Scope != "all" {
		writeError(w, http.StatusBadRequest, "scope must be recent, feedback, or all.")
		return
	}
	if payload.Limit == 0 {
		payload.Limit = 50
	}
	if payload.Limit < 1 || payload.Limit > 500 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 500.")
		return
	}
	job := launchLocalJob(
		s.settings.LogsDir,
		"reclassify",
		"job.started",
		"Job queued.",
		"pipeline.metadata.enriching",
		"Getting metadata for papers to reclassify.",
		func(progress jobruntime.ProgressFunc) (map[string]any, error) {
			paperIDs, err := selectReclassifyPaperIDsFunc(s.settings, payload.Scope, payload.Limit)
			if err != nil {
				return nil, err
			}
			reclassified, err := reclassifyPaperIDsFunc(s.settings, paperIDs, progress)
			if err != nil {
				return nil, err
			}
			reportCount, err := rebuildLatestReportFunc(s.settings, progress)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"scope":         payload.Scope,
				"paper_ids":     paperIDs,
				"reclassified":  reclassified,
				"report_papers": reportCount,
			}, nil
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, listJobs())
}

func (s *Server) handleAdminJobByID(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/api/admin/jobs/")
	if jobID == "" || strings.Contains(jobID, "/") {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	job, ok := jobByID(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
