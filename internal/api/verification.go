package api

import (
	"fmt"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/logging"
)

type pendingVerification struct {
	ID               string
	JobID            string
	FeedURL          string
	FeedURLs         []string
	Journal          string
	Host             string
	Target           string
	Reason           string
	Method           string
	SessionState     string
	VerifierKind     string
	CallbackURL      string
	Result           chan verificationResult
	Delivered        bool
	CallbackReceived bool
	CallbackResult   verificationResult
}

type verificationResult struct {
	Status      string
	ContentType string
	FeedXML     []byte
	FeedBodies  map[string][]byte
	Err         error
	Warning     string
}

type verificationRegistry struct {
	mu    sync.Mutex
	items map[string]*pendingVerification
}

var (
	apiVerifications             = verificationRegistry{items: map[string]*pendingVerification{}}
	startVerificationFlowFunc    = startVerificationFlow
	completeVerificationFlowFunc = completeVerificationFlow
)

const (
	verificationMethodNativeWebview = "native_webview2"
	verificationMethodBrowserManual = "browser_manual"
)

func launchVerificationAwareSyncJob(settings config.Settings, run func(progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string, verifyHost feeds.VerifyHostFunc) (map[string]any, error)) (jobInfo, bool) {
	job := jobInfo{
		ID:         nextJobID(),
		JobType:    "sync",
		Status:     "queued",
		MessageKey: "job.started",
		Message:    "Job queued.",
		CreatedAt:  nowFunc().UTC(),
	}
	var reused bool
	job, reused = reserveJobUnlessActive(job)
	if reused {
		return job, true
	}
	if path, err := logging.Write(settings.LogsDir, logging.Event{
		Level:      "info",
		Component:  "api.jobs",
		Action:     "queued",
		JobID:      job.ID,
		MessageKey: job.MessageKey,
		Message:    job.Message,
	}); err == nil {
		job.LogPath = path
		storeJob(job)
	}

	go func() {
		started := nowFunc().UTC()
		logJobEvent(settings.LogsDir, &job, "info", "started", "pipeline.feeds.fetching", "Fetching RSS feeds.", "", nil)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "running"
			current.MessageKey = "pipeline.feeds.fetching"
			current.Message = "Fetching RSS feeds."
			current.StartedAt = &started
		})

		progress := func(update jobruntime.ProgressUpdate) {
			logJobEvent(settings.LogsDir, &job, "info", "progress", update.MessageKey, update.Message, "", progressLogData(update))
			updateJob(job.ID, func(current *jobInfo) {
				current.MessageKey = update.MessageKey
				current.Message = update.Message
				current.ProgressStage = update.Stage
				current.ProgressCurrent = update.Current
				current.ProgressTotal = update.Total
				current.ProgressPercent = update.Percent
				current.ProgressLabel = update.Label
				current.ProgressMode = string(update.Mode)
			})
		}

		result, err := runVerificationAwareSync(settings, job.ID, "", progress, run, verificationAwareSyncCallbacks{
			OnWaiting: func(pending *pendingVerification) {
				logJobEvent(settings.LogsDir, &job, "warning", "waiting_for_user", "pipeline.feeds.verification_required", "A protected feed requires manual verification.", "", map[string]any{
					"verification_target":   pending.Target,
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
					"verification_host":     pending.Host,
				})
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "waiting_for_user"
					current.MessageKey = "pipeline.feeds.verification_required"
					current.Message = "A protected feed needs Cloudflare verification. A verification window should open automatically."
					clearJobProgress(current)
					current.Result = map[string]any{
						"verification_required":      true,
						"verification_target":        pending.Target,
						"verification_feed_url":      pending.FeedURL,
						"verification_journal":       pending.Journal,
						"verification_host":          pending.Host,
						"verification_method":        pending.Method,
						"verification_session_state": pending.SessionState,
					}
					current.VerificationRequired = true
					current.VerificationTarget = pending.Target
					current.VerificationFeedURL = pending.FeedURL
					current.VerificationJournal = pending.Journal
					current.VerificationHost = pending.Host
					current.VerificationMethod = pending.Method
					current.VerificationSessionState = pending.SessionState
				})
			},
			OnVerificationStartFailed: func(pending *pendingVerification, err error) {
				logJobEvent(settings.LogsDir, &job, "warning", "verification_start_failed", "pipeline.feeds.verification_required", "", err.Error(), map[string]any{
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
					"verification_target":   pending.Target,
				})
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "running"
					current.MessageKey = "pipeline.feeds.fetching"
					current.Message = "Verification window could not be opened. Skipping that protected feed for this sync."
					clearJobProgress(current)
					current.VerificationRequired = false
					current.VerificationTarget = ""
					current.VerificationFeedURL = ""
					current.VerificationJournal = ""
					current.VerificationHost = ""
					current.VerificationMethod = ""
					current.VerificationSessionState = ""
				})
			},
			OnVerificationStarted: func(pending *pendingVerification) {
				logJobEvent(settings.LogsDir, &job, "info", "verification_started", "pipeline.feeds.verification_required", "Opened the feed verification window.", "", map[string]any{
					"verification_id":       pending.ID,
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
					"verification_host":     pending.Host,
				})
				if pending.SessionState == verificationSessionStateVerified {
					updateJob(job.ID, func(current *jobInfo) {
						current.Status = "running"
						current.MessageKey = "pipeline.feeds.verification_required"
						current.Message = "Reusing the previous protected-feed verification session and retrying this host."
						current.VerificationRequired = false
						current.VerificationTarget = pending.Target
						current.VerificationFeedURL = pending.FeedURL
						current.VerificationJournal = pending.Journal
						current.VerificationHost = pending.Host
						current.VerificationMethod = pending.Method
						current.VerificationSessionState = pending.SessionState
					})
				}
			},
			OnVerificationSkipped: func(pending *pendingVerification, resumeResult verificationResult) {
				logJobEvent(settings.LogsDir, &job, "warning", "verification_skipped", "pipeline.feeds.fetching", "Verification did not return feed XML. Continuing this sync without that feed.", resumeResult.Warning, map[string]any{
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
					"verification_target":   pending.Target,
				})
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "running"
					current.MessageKey = "pipeline.feeds.fetching"
					current.Message = "Verification was not completed. Continuing this sync and recording a fetch warning."
					clearJobProgress(current)
					current.VerificationRequired = false
					current.VerificationTarget = ""
					current.VerificationFeedURL = ""
					current.VerificationJournal = ""
					current.VerificationHost = ""
					current.VerificationMethod = ""
					current.VerificationSessionState = ""
				})
			},
			OnVerificationResumed: func(_ *pendingVerification) {
				logJobEvent(settings.LogsDir, &job, "info", "verification_resumed", "pipeline.feeds.fetching", "Verification received. Continuing RSS fetch with verified XML.", "", nil)
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "running"
					current.MessageKey = "pipeline.feeds.fetching"
					current.Message = "Verification received. Continuing RSS fetch with verified XML."
					clearJobProgress(current)
					current.VerificationRequired = false
					current.VerificationTarget = ""
					current.VerificationFeedURL = ""
					current.VerificationJournal = ""
					current.VerificationHost = ""
					current.VerificationMethod = ""
					current.VerificationSessionState = ""
				})
			},
		})
		if err == nil {
			warningCount := countWarnings(result)
			finished := nowFunc().UTC()
			logJobEvent(settings.LogsDir, &job, "info", "completed", "sync.completed", "Completed.", "", result)
			updateJob(job.ID, func(current *jobInfo) {
				current.Status = "completed"
				current.MessageKey = "sync.completed"
				current.Message = summarizeResult("sync", result)
				current.Result = result
				current.WarningCount = warningCount
				clearJobProgress(current)
				current.FinishedAt = &finished
				current.VerificationRequired = false
				current.VerificationTarget = ""
				current.VerificationFeedURL = ""
				current.VerificationJournal = ""
				current.VerificationHost = ""
				current.VerificationMethod = ""
				current.VerificationSessionState = ""
			})
			return
		}
		finished := nowFunc().UTC()
		logJobEvent(settings.LogsDir, &job, "error", "failed", "sync.failed", "", err.Error(), nil)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "failed"
			current.MessageKey = "sync.failed"
			current.Error = err.Error()
			clearJobProgress(current)
			current.FinishedAt = &finished
			current.VerificationRequired = false
			current.VerificationTarget = ""
			current.VerificationFeedURL = ""
			current.VerificationJournal = ""
			current.VerificationHost = ""
			current.VerificationMethod = ""
			current.VerificationSessionState = ""
		})
		return
	}()

	return job, false
}

func storePendingVerification(jobID string, request feeds.VerificationRequest) *pendingVerification {
	return storePendingVerificationWithCallback(jobID, request, []feeds.VerificationRequest{request}, "")
}

func storePendingVerificationWithCallback(jobID string, request feeds.VerificationRequest, groupedRequests []feeds.VerificationRequest, callbackURL string) *pendingVerification {
	feedURLs := make([]string, 0, len(groupedRequests))
	for _, item := range groupedRequests {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		feedURLs = append(feedURLs, item.URL)
	}
	if len(feedURLs) == 0 {
		feedURLs = append(feedURLs, request.URL)
	}
	pending := &pendingVerification{
		ID:          nextJobID(),
		JobID:       jobID,
		FeedURL:     request.URL,
		FeedURLs:    feedURLs,
		Journal:     request.Journal,
		Host:        verificationProfileHost(request.URL),
		Target:      request.Target,
		Reason:      request.Reason,
		Method:      verificationMethodNativeWebview,
		CallbackURL: strings.TrimSpace(callbackURL),
		Result:      make(chan verificationResult, 1),
	}
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()
	apiVerifications.items[pending.ID] = pending
	return pending
}

func pendingVerificationForJob(jobID string, feedURL string) (*pendingVerification, bool) {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()
	for _, pending := range apiVerifications.items {
		if pending.JobID == jobID && pending.FeedURL == feedURL {
			return pending, true
		}
	}
	return nil, false
}

func pendingVerificationByID(id string) (*pendingVerification, bool) {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()
	pending, ok := apiVerifications.items[id]
	return pending, ok
}

func deletePendingVerification(id string) {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()
	delete(apiVerifications.items, id)
}

func waitForVerification(pending *pendingVerification, timeout time.Duration) verificationResult {
	select {
	case result := <-pending.Result:
		return result
	case <-time.After(timeout):
		return verificationResult{
			Status:  "timeout",
			Warning: "the verification window timed out before RSS XML was captured",
		}
	}
}

func startVerificationFlow(settings config.Settings, pending *pendingVerification) error {
	if pending == nil {
		return fmt.Errorf("verification request not found")
	}
	if verificationTargetForFeedURL(pending.FeedURL) != "cloudflare" {
		return fmt.Errorf("unsupported verification target")
	}
	resetPendingVerificationAttempt(pending, verificationMethodNativeWebview)
	return startVerificationWindowFlow(settings, pending)
}

func completeVerificationFlow(settings config.Settings, pending *pendingVerification) (verificationResult, error) {
	if pending == nil {
		return verificationResult{}, fmt.Errorf("verification request not found")
	}
	if verificationTargetForFeedURL(pending.FeedURL) != "cloudflare" {
		return verificationResult{}, fmt.Errorf("unsupported verification target")
	}
	if !pending.CallbackReceived {
		return verificationResult{}, fmt.Errorf("the verification window has not returned RSS XML yet; finish the Cloudflare check there first")
	}
	if strings.TrimSpace(pending.CallbackResult.Warning) != "" {
		return verificationResult{}, fmt.Errorf("%s", pending.CallbackResult.Warning)
	}
	if pending.CallbackResult.Err != nil {
		return verificationResult{}, pending.CallbackResult.Err
	}
	if len(pending.CallbackResult.FeedXML) == 0 && len(pending.CallbackResult.FeedBodies) == 0 {
		return verificationResult{}, fmt.Errorf("verification completed without returning RSS XML")
	}
	return pending.CallbackResult, nil
}

func verificationTargetForFeedURL(feedURL string) string {
	parsed, err := neturl.Parse(feedURL)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Hostname()) == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
		return "cloudflare"
	default:
		return ""
	}
}

func resetPendingVerificationAttempt(pending *pendingVerification, method string) {
	if pending == nil {
		return
	}
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()
	pending.Delivered = false
	pending.CallbackReceived = false
	pending.CallbackResult = verificationResult{}
	if strings.TrimSpace(method) != "" {
		pending.Method = strings.TrimSpace(method)
	}
}

func storeVerificationCallbackResult(id string, result verificationResult) (*pendingVerification, bool, bool) {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()

	pending, ok := apiVerifications.items[id]
	if !ok {
		return nil, false, false
	}
	if pending.CallbackReceived {
		if pending.CallbackResult.Status == "success" {
			return pending, true, false
		}
		return pending, true, false
	}
	pending.CallbackReceived = true
	pending.CallbackResult = verificationResult{
		Status:      result.Status,
		ContentType: result.ContentType,
		FeedXML:     append([]byte(nil), result.FeedXML...),
		FeedBodies:  cloneFeedBodies(result.FeedBodies),
		Err:         result.Err,
		Warning:     result.Warning,
	}
	return pending, true, true
}

func cloneFeedBodies(items map[string][]byte) map[string][]byte {
	if len(items) == 0 {
		return nil
	}
	cloned := make(map[string][]byte, len(items))
	for key, value := range items {
		cloned[key] = append([]byte(nil), value...)
	}
	return cloned
}

func markVerificationDelivered(id string) bool {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()

	pending, ok := apiVerifications.items[id]
	if !ok || pending.Delivered {
		return false
	}
	pending.Delivered = true
	return true
}

func verificationDeliveryState(id string) (callbackReceived bool, delivered bool) {
	apiVerifications.mu.Lock()
	defer apiVerifications.mu.Unlock()

	pending, ok := apiVerifications.items[id]
	if !ok {
		return false, false
	}
	return pending.CallbackReceived, pending.Delivered
}
