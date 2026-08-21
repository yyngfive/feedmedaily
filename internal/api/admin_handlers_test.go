package api

import (
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdminSyncJob(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	runSyncFunc = func(_ config.Settings, _ jobruntime.RunOptions, progress jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		if progress != nil {
			progress(jobruntime.ItemProgress("pipeline.feeds.fetching", "fetch", 1, 1, "Test Journal", "Fetching feed 1/1: Test Journal."))
			progress(jobruntime.PercentProgress("pipeline.metadata.enriching", "metadata", 1, 1, "Getting metadata 1/1 (100%)."))
			progress(jobruntime.PercentProgress("pipeline.classifier.classifying", "classification", 1, 1, "Classifying papers 1/1 (100%)."))
			progress(jobruntime.ProgressUpdate{MessageKey: "pipeline.report.refreshing", Message: "Refreshing the latest report from SQLite.", Stage: "report"})
		}
		return jobruntime.RunSummary{
			Fetched:    2,
			Inserted:   1,
			Updated:    1,
			Classified: 1,
			Errors:     []string{"https://bad.feed/: timeout"},
		}, nil
	}
	handler := newTestHandler(t, testSettings(root))

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, runPayload.Job.ID)

	jobRecorder := httptest.NewRecorder()
	handler.ServeHTTP(jobRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs/"+runPayload.Job.ID, nil))
	if jobRecorder.Code != http.StatusOK || !contains(jobRecorder.Body.String(), `"status":"completed"`) || !contains(jobRecorder.Body.String(), `"fetched":2`) {
		t.Fatalf("job detail = %d %s", jobRecorder.Code, jobRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/api/admin/jobs", nil))
	if listRecorder.Code != http.StatusOK || !contains(listRecorder.Body.String(), `"job_type":"sync"`) {
		t.Fatalf("job list = %d %s", listRecorder.Code, listRecorder.Body.String())
	}
}

func TestAdminRunReusesActiveSyncJob(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSync := func() { releaseOnce.Do(func() { close(release) }) }
	var firstJobID string
	defer func() {
		releaseSync()
		deadline := time.Now().Add(2 * time.Second)
		for firstJobID != "" && time.Now().Before(deadline) {
			job, ok := jobByID(firstJobID)
			if ok && (job.Status == "completed" || job.Status == "failed") {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	var calls atomic.Int32
	runSyncFunc = func(_ config.Settings, _ jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return jobruntime.RunSummary{Fetched: 1}, nil
	}
	handler := newTestHandler(t, testSettings(root))

	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first run launch = %d %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstPayload); err != nil {
		t.Fatal(err)
	}
	firstJobID = firstPayload.Job.ID
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first sync did not start")
	}

	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, httptest.NewRequest(http.MethodPost, "/api/admin/run", nil))
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second run launch = %d %s", secondRecorder.Code, secondRecorder.Body.String())
	}
	var secondPayload struct {
		Job    jobInfo `json:"job"`
		Reused bool    `json:"reused"`
	}
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &secondPayload); err != nil {
		t.Fatal(err)
	}
	if !secondPayload.Reused {
		t.Fatal("second run should report the active sync as reused")
	}
	if secondPayload.Job.ID != firstPayload.Job.ID {
		t.Fatalf("second run job id = %q, want active job %q", secondPayload.Job.ID, firstPayload.Job.ID)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("runSyncFunc calls = %d, want 1", got)
	}
	releaseSync()
	waitForJobCompletion(t, firstPayload.Job.ID)
}

func TestAdminRunPassesSelectedFeedURLs(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	settings := testSettings(root)
	writeFile(t, settings.FeedsPath, `[
  {"journal":"One","url":"https://example.com/one.rss"},
  {"journal":"Two","url":"https://example.com/two.rss"}
]`)
	seenOptions := make(chan jobruntime.RunOptions, 1)
	runSyncFunc = func(_ config.Settings, opts jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		seenOptions <- opts
		return jobruntime.RunSummary{Fetched: 1, Inserted: 1, Classified: 1}, nil
	}
	handler := newTestHandler(t, settings)

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(
		http.MethodPost,
		"/api/admin/run",
		strings.NewReader(`{"feed_urls":["https://example.com/two.rss"]}`),
	))
	if runRecorder.Code != http.StatusOK {
		t.Fatalf("run launch = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
	var runPayload struct {
		Job jobInfo `json:"job"`
	}
	if err := json.Unmarshal(runRecorder.Body.Bytes(), &runPayload); err != nil {
		t.Fatal(err)
	}
	waitForJobCompletion(t, runPayload.Job.ID)

	select {
	case opts := <-seenOptions:
		if strings.Join(opts.SelectedFeedURLs, "|") != "https://example.com/two.rss" {
			t.Fatalf("selected feed urls = %#v", opts.SelectedFeedURLs)
		}
	default:
		t.Fatal("runSyncFunc did not receive options")
	}
}

func TestAdminRunRejectsUnknownSelectedFeedURL(t *testing.T) {
	root := t.TempDir()
	restore := stubAPIGlobals(t)
	defer restore()

	settings := testSettings(root)
	writeFile(t, settings.FeedsPath, `[{"journal":"One","url":"https://example.com/one.rss"}]`)
	runSyncFunc = func(_ config.Settings, _ jobruntime.RunOptions, _ jobruntime.ProgressFunc) (jobruntime.RunSummary, error) {
		t.Fatal("runSyncFunc should not be called for invalid feed_urls")
		return jobruntime.RunSummary{}, nil
	}
	handler := newTestHandler(t, settings)

	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, httptest.NewRequest(
		http.MethodPost,
		"/api/admin/run",
		strings.NewReader(`{"feed_urls":["https://example.com/missing.rss"]}`),
	))
	if runRecorder.Code != http.StatusBadRequest || !contains(runRecorder.Body.String(), "unknown feed URL") {
		t.Fatalf("run response = %d %s", runRecorder.Code, runRecorder.Body.String())
	}
}
