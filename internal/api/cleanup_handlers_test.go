package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func TestAdminCleanupRequiresConfirmationAndLaunchesJob(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	cleanupUnclassifiedContextFunc = func(_ config.Settings, _ context.Context, _ jobruntime.ProgressFunc, _ *llmusage.Collector) (map[string]any, error) {
		return map[string]any{
			"scanned": 4, "duplicate_groups": 1, "deleted_duplicates": 1, "repaired_doi": 1, "reclassified": 2, "needs_review": 1,
		}, nil
	}

	handler := newTestHandler(t, testSettings(root))
	missingConfirmation := httptest.NewRecorder()
	handler.ServeHTTP(missingConfirmation, httptest.NewRequest(http.MethodPost, "/api/admin/cleanup", strings.NewReader(`{"confirm":false}`)))
	if missingConfirmation.Code != http.StatusBadRequest {
		t.Fatalf("cleanup without confirmation = %d %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}

	launch := httptest.NewRecorder()
	handler.ServeHTTP(launch, httptest.NewRequest(http.MethodPost, "/api/admin/cleanup", strings.NewReader(`{"confirm":true}`)))
	if launch.Code != http.StatusOK || !strings.Contains(launch.Body.String(), `"job_type":"cleanup"`) {
		t.Fatalf("cleanup launch = %d %s", launch.Code, launch.Body.String())
	}
	var payload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(launch.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	job := waitForJobTerminalStatus(t, payload.Job.ID)
	if job.Status != "completed" || job.Result["deleted_duplicates"] != 1 {
		t.Fatalf("cleanup job = %#v", job)
	}
}

func TestAdminCleanupReviewRejectsDefer(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()
	settings := testSettings(root)

	sqliteStore, err := store.OpenOrCreate(filepath.Join(root, "data", "literature.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	paperID, _, err := sqliteStore.UpsertPaper(store.Paper{SourceURL: "feed", Title: "Review paper", URL: "https://example.com/review"}, now)
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	reviewID, err := sqliteStore.UpsertCleanupReview(store.CleanupReviewDraft{
		GroupKey: "doi:review", CandidatePaperID: paperID, MatchType: store.CleanupReviewMatchDOIUnclear,
		Reason: "not enough provider data", SuggestedAction: store.CleanupDecisionKeep,
	}, now)
	if err != nil {
		sqliteStore.Close()
		t.Fatal(err)
	}
	if err := sqliteStore.Close(); err != nil {
		t.Fatal(err)
	}

	handler := newTestHandler(t, settings)
	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/admin/cleanup", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"unclassified_paper_count":1`) || !strings.Contains(status.Body.String(), `"pending_review_count":1`) {
		t.Fatalf("cleanup status = %d %s", status.Code, status.Body.String())
	}

	deferRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deferRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/cleanup/reviews/"+formatInt(reviewID), strings.NewReader(`{"decision":"defer"}`)))
	if deferRecorder.Code != http.StatusBadRequest || !strings.Contains(deferRecorder.Body.String(), "decision must be keep, delete, delete_match, clear_doi, or clear_match_doi") {
		t.Fatalf("defer rejection = %d %s", deferRecorder.Code, deferRecorder.Body.String())
	}
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
