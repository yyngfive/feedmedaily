package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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
	serverSettings := s.snapshotSettings()
	selectedFeedURLs, err := validateSelectedFeedURLs(serverSettings.FeedsPath, payload.FeedURLs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, reused, reserved := launchVerificationAwareSyncJob(
		serverSettings,
		func(ctx context.Context, progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string, verifyHost feeds.VerifyHostFunc, usage *llmusage.Collector) (map[string]any, error) {
			summary, err := runSyncFunc(serverSettings, jobruntime.RunOptions{
				SelectedFeedURLs:  selectedFeedURLs,
				FeedBodyOverrides: overrides,
				SkippedFeeds:      skippedFeeds,
				VerifyFeedHost:    verifyHost,
				Usage:             usage,
				Context:           ctx,
			}, progress)
			return map[string]any{
				"fetched":    summary.Fetched,
				"inserted":   summary.Inserted,
				"updated":    summary.Updated,
				"classified": summary.Classified,
				"errors":     summary.Errors,
			}, err
		},
	)
	if !reserved {
		writeError(w, http.StatusConflict, "A reclassification job is running. Wait for it to finish before syncing.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "reused": reused})
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
	serverSettings := s.snapshotSettings()
	if r.Method == http.MethodGet {
		paperCount, err := jobruntime.CountPapers(serverSettings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		classifiedCount, err := jobruntime.CountClassifiedPapers(serverSettings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		todayCount, todayClassifiedCount, err := jobruntime.CountTodayPapers(serverSettings, nowFunc())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		limit := 0
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			limit, err = strconv.Atoi(rawLimit)
			if err != nil || limit < 0 || limit > paperCount {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 0 and %d.", paperCount))
				return
			}
		}
		countTotal, countClassified, err := jobruntime.CountRecentPaperClassifications(serverSettings, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"paper_count":              paperCount,
			"classified_paper_count":   classifiedCount,
			"unclassified_paper_count": paperCount - classifiedCount,
			"today_paper_count":        todayCount,
			"today_classified_count":   todayClassifiedCount,
			"today_unclassified_count": todayCount - todayClassifiedCount,
			"count_paper_count":        countTotal,
			"count_classified_count":   countClassified,
			"count_unclassified_count": countTotal - countClassified,
		})
		return
	}
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
		payload.Scope = "today"
	}
	if payload.Scope != "today" && payload.Scope != "feedback" && payload.Scope != "all" && payload.Scope != "count" && payload.Scope != "unclassified" {
		writeError(w, http.StatusBadRequest, "scope must be today, feedback, all, count, or unclassified.")
		return
	}
	if payload.Scope == "count" {
		paperCount, err := jobruntime.CountPapers(serverSettings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if payload.Limit < 0 || payload.Limit > paperCount {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 0 and %d.", paperCount))
			return
		}
	} else {
		payload.Limit = 0
	}
	releasePipeline, locked := tryLockPipeline()
	if !locked {
		writeError(w, http.StatusConflict, "A sync or reclassification job is already running. Wait for it to finish.")
		return
	}
	job := launchLocalJob(
		serverSettings,
		"reclassify",
		"job.started",
		"Job queued.",
		"pipeline.metadata.enriching",
		"Getting metadata for papers to reclassify.",
		reclassifyJobRunFunc(serverSettings, payload.Scope, func() ([]int64, error) {
			return selectReclassifyPaperIDsFunc(serverSettings, payload.Scope, payload.Limit)
		}),
		func(context.Context) (func(), error) { return releasePipeline, nil },
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

// reclassifyJobRunFunc 把一段固定的 paper ids 或 scope 选择逻辑包装成可取消的 reclassify job。
// pipeline 锁由启动方获取（手动路径同步 TryLock，排队路径由 wait 门获取），
// 释放统一交给 launchLocalJob 在作业结束时执行。
func reclassifyJobRunFunc(serverSettings config.Settings, scope string, selectIDs func() ([]int64, error)) localJobFunc {
	return func(ctx context.Context, progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
		paperIDs, err := selectIDs()
		if err != nil {
			return nil, err
		}
		reclassified, err := reclassifyPaperIDsContextFunc(serverSettings, paperIDs, ctx, progress, usage)
		result := map[string]any{
			"scope":        scope,
			"paper_ids":    paperIDs,
			"reclassified": reclassified,
		}
		if err != nil {
			return result, err
		}
		reportCount, err := rebuildLatestReportFunc(serverSettings, progress)
		if err != nil {
			return result, err
		}
		result["report_papers"] = reportCount
		return result, nil
	}
}

func (s *Server) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, listJobs())
}

func (s *Server) handleAdminJobByID(w http.ResponseWriter, r *http.Request) {
	rawID := strings.TrimPrefix(r.URL.Path, "/api/admin/jobs/")
	if strings.HasSuffix(rawID, "/cancel") {
		jobID := strings.TrimSuffix(rawID, "/cancel")
		if jobID == "" || strings.Contains(jobID, "/") {
			writeError(w, http.StatusNotFound, "Job not found.")
			return
		}
		s.handleAdminJobCancel(w, r, jobID)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jobID := rawID
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

func (s *Server) handleAdminJobCancel(w http.ResponseWriter, r *http.Request, jobID string) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	job, requested, exists := requestJobCancellation(jobID)
	if !exists {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	if !isCancellableJobType(job.JobType) {
		writeError(w, http.StatusBadRequest, "Only sync and reclassify jobs can be stopped.")
		return
	}
	if !isActiveJobStatus(job.Status) {
		writeJSON(w, http.StatusOK, map[string]any{"job": job, "cancel_requested": false, "already_terminal": true})
		return
	}
	if !requested {
		writeError(w, http.StatusConflict, "Job is no longer cancellable.")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job, "cancel_requested": true})
}

func (s *Server) handleAdminLLMUsage(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sinceValue := strings.TrimSpace(r.URL.Query().Get("since"))
	if sinceValue == "" {
		writeError(w, http.StatusBadRequest, "since is required and must be RFC3339.")
		return
	}
	since, err := time.Parse(time.RFC3339, sinceValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "since must be RFC3339.")
		return
	}
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := sqliteStore.ListLLMUsage(since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}
