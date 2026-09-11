package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func (s *Server) handleAdminCleanup(w http.ResponseWriter, r *http.Request) {
	serverSettings := s.snapshotSettings()
	if r.Method == http.MethodGet {
		sqliteStore, err := s.getReadStore()
		if err != nil {
			if os.IsNotExist(err) {
				writeJSON(w, http.StatusOK, map[string]any{
					"unclassified_paper_count": 0,
					"pending_review_count":     0,
				})
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		unclassified, err := countUnclassifiedPapers(sqliteStore)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		pending, err := sqliteStore.CountCleanupReviews(store.CleanupReviewStatePending)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"unclassified_paper_count": unclassified,
			"pending_review_count":     pending,
		})
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if !payload.Confirm {
		writeError(w, http.StatusBadRequest, "confirm must be true before database cleanup can start.")
		return
	}
	releasePipeline, locked := tryLockPipeline()
	if !locked {
		writeError(w, http.StatusConflict, "A sync, reclassification, or database cleanup job is already running. Wait for it to finish.")
		return
	}
	jobRun := func(ctx context.Context, progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
		return cleanupUnclassifiedContextFunc(serverSettings, ctx, progress, usage)
	}
	job := launchLocalJob(
		serverSettings,
		"cleanup",
		"job.started",
		"Database cleanup queued.",
		"pipeline.cleanup.scanning",
		"Scanning unclassified papers.",
		jobRun,
		func(context.Context) (func(), error) { return releasePipeline, nil },
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleAdminCleanupReviews(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state != "" && state != store.CleanupReviewStatePending && state != "all" {
		writeError(w, http.StatusBadRequest, "state must be pending or all.")
		return
	}
	if state == "all" {
		state = ""
	}
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []store.CleanupReview{})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := sqliteStore.ListCleanupReviews(state)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleAdminCleanupReviewByID(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimPrefix(r.URL.Path, "/api/admin/cleanup/reviews/")
	if rawID == "" || strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "Cleanup review not found.")
		return
	}
	reviewID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || reviewID <= 0 {
		writeError(w, http.StatusNotFound, "Cleanup review not found.")
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Decision string `json:"decision"`
		Confirm  bool   `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	payload.Decision = strings.TrimSpace(strings.ToLower(payload.Decision))
	if payload.Decision != store.CleanupDecisionKeep && payload.Decision != store.CleanupDecisionDelete && payload.Decision != store.CleanupDecisionDeleteMatch && payload.Decision != store.CleanupDecisionClearDOI && payload.Decision != store.CleanupDecisionClearMatchDOI {
		writeError(w, http.StatusBadRequest, "decision must be keep, delete, delete_match, clear_doi, or clear_match_doi.")
		return
	}
	if (payload.Decision == store.CleanupDecisionDelete || payload.Decision == store.CleanupDecisionDeleteMatch || payload.Decision == store.CleanupDecisionClearDOI || payload.Decision == store.CleanupDecisionClearMatchDOI) && !payload.Confirm {
		writeError(w, http.StatusBadRequest, "confirm must be true for delete or DOI repair decisions.")
		return
	}
	serverSettings := s.snapshotSettings()
	releasePipeline, locked := tryLockPipeline()
	if !locked {
		writeError(w, http.StatusConflict, "A sync, reclassification, or database cleanup job is already running. Wait for it to finish.")
		return
	}
	jobRun := func(ctx context.Context, progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
		return cleanupReviewContextFunc(serverSettings, reviewID, payload.Decision, ctx, progress, usage)
	}
	job := launchLocalJob(
		serverSettings,
		"cleanup-review",
		"job.started",
		"Cleanup review queued.",
		"pipeline.cleanup.applying",
		"Applying cleanup review decision.",
		jobRun,
		func(context.Context) (func(), error) { return releasePipeline, nil },
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func countUnclassifiedPapers(sqliteStore *store.Store) (int, error) {
	return sqliteStore.UnclassifiedPaperCount()
}
