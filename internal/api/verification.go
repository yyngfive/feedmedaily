package api

import (
	"errors"
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
	Journal          string
	Target           string
	Reason           string
	Result           chan verificationResult
	Delivered        bool
	CallbackReceived bool
	CallbackResult   verificationResult
}

type verificationResult struct {
	Status      string
	ContentType string
	FeedXML     []byte
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

func launchVerificationAwareRunJob(settings config.Settings, run func(progress func(string, string), overrides map[string][]byte, skippedFeeds map[string]string) (map[string]any, error)) jobInfo {
	job := jobInfo{
		ID:         nextJobID(),
		JobType:    "run",
		Status:     "queued",
		MessageKey: "job.started",
		Message:    "Job queued.",
		CreatedAt:  nowFunc().UTC(),
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
	}
	storeJob(job)

	go func() {
		started := nowFunc().UTC()
		overrideBodies := map[string][]byte{}
		skippedFeeds := map[string]string{}
		logJobEvent(settings.LogsDir, &job, "info", "started", "pipeline.feeds.fetching", "Fetching RSS feeds.", "", nil)
		updateJob(job.ID, func(current *jobInfo) {
			current.Status = "running"
			current.MessageKey = "pipeline.feeds.fetching"
			current.Message = "Fetching RSS feeds."
			current.StartedAt = &started
		})

		progress := func(messageKey string, message string) {
			logJobEvent(settings.LogsDir, &job, "info", "progress", messageKey, message, "", nil)
			updateJob(job.ID, func(current *jobInfo) {
				current.MessageKey = messageKey
				current.Message = message
			})
		}

		for {
			result, err := run(progress, overrideBodies, skippedFeeds)
			if err == nil {
				warningCount := countWarnings(result)
				finished := nowFunc().UTC()
				logJobEvent(settings.LogsDir, &job, "info", "completed", "run.completed", "Completed.", "", result)
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "completed"
					current.MessageKey = "run.completed"
					current.Message = summarizeResult("run", result)
					current.Result = result
					current.WarningCount = warningCount
					current.FinishedAt = &finished
					current.VerificationRequired = false
					current.VerificationTarget = ""
					current.VerificationFeedURL = ""
					current.VerificationJournal = ""
				})
				return
			}

			var verificationErr *jobruntime.VerificationRequiredError
			if errors.As(err, &verificationErr) && len(verificationErr.Requests) > 0 {
				request := verificationErr.Requests[0]
				pending := storePendingVerification(job.ID, request)
				logJobEvent(settings.LogsDir, &job, "warning", "waiting_for_user", "pipeline.feeds.verification_required", "A protected feed requires manual verification.", "", map[string]any{
					"verification_target":   pending.Target,
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
				})
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "waiting_for_user"
					current.MessageKey = "pipeline.feeds.verification_required"
					current.Message = "A protected feed needs Cloudflare verification. A verification window should open automatically."
					current.Result = map[string]any{
						"verification_required": true,
						"verification_target":   pending.Target,
						"verification_feed_url": pending.FeedURL,
						"verification_journal":  pending.Journal,
					}
					current.VerificationRequired = true
					current.VerificationTarget = pending.Target
					current.VerificationFeedURL = pending.FeedURL
					current.VerificationJournal = pending.Journal
				})

				if err := startVerificationFlowFunc(settings, pending); err != nil {
					deletePendingVerification(pending.ID)
					skippedFeeds[pending.FeedURL] = err.Error()
					logJobEvent(settings.LogsDir, &job, "warning", "verification_start_failed", "pipeline.feeds.verification_required", "", err.Error(), map[string]any{
						"verification_feed_url": pending.FeedURL,
						"verification_journal":  pending.Journal,
						"verification_target":   pending.Target,
					})
					updateJob(job.ID, func(current *jobInfo) {
						current.Status = "running"
						current.MessageKey = "pipeline.feeds.fetching"
						current.Message = "Verification window could not be opened. Skipping that protected feed for this run."
						current.VerificationRequired = false
						current.VerificationTarget = ""
						current.VerificationFeedURL = ""
						current.VerificationJournal = ""
					})
					continue
				}
				logJobEvent(settings.LogsDir, &job, "info", "verification_started", "pipeline.feeds.verification_required", "Opened the feed verification window.", "", map[string]any{
					"verification_id":       pending.ID,
					"verification_feed_url": pending.FeedURL,
					"verification_journal":  pending.Journal,
				})

				resumeResult := waitForVerification(pending, 20*time.Minute)
				deletePendingVerification(pending.ID)
				if strings.TrimSpace(resumeResult.Warning) != "" {
					skippedFeeds[pending.FeedURL] = resumeResult.Warning
					logJobEvent(settings.LogsDir, &job, "warning", "verification_skipped", "pipeline.feeds.fetching", "Verification did not return feed XML. Continuing this run without that feed.", resumeResult.Warning, map[string]any{
						"verification_feed_url": pending.FeedURL,
						"verification_journal":  pending.Journal,
						"verification_target":   pending.Target,
					})
					updateJob(job.ID, func(current *jobInfo) {
						current.Status = "running"
						current.MessageKey = "pipeline.feeds.fetching"
						current.Message = "Verification was not completed. Continuing this run and recording a fetch warning."
						current.VerificationRequired = false
						current.VerificationTarget = ""
						current.VerificationFeedURL = ""
						current.VerificationJournal = ""
					})
					continue
				}
				overrideBodies[pending.FeedURL] = resumeResult.FeedXML
				logJobEvent(settings.LogsDir, &job, "info", "verification_resumed", "pipeline.feeds.fetching", "Verification received. Resuming RSS fetch.", "", nil)
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "running"
					current.MessageKey = "pipeline.feeds.fetching"
					current.Message = "Verification received. Resuming RSS fetch."
					current.VerificationRequired = false
					current.VerificationTarget = ""
					current.VerificationFeedURL = ""
					current.VerificationJournal = ""
				})
				continue
			}

			finished := nowFunc().UTC()
			logJobEvent(settings.LogsDir, &job, "error", "failed", "run.failed", "", err.Error(), nil)
			updateJob(job.ID, func(current *jobInfo) {
				current.Status = "failed"
				current.MessageKey = "run.failed"
				current.Error = err.Error()
				current.FinishedAt = &finished
				current.VerificationRequired = false
				current.VerificationTarget = ""
				current.VerificationFeedURL = ""
				current.VerificationJournal = ""
			})
			return
		}
	}()

	return job
}

func storePendingVerification(jobID string, request feeds.VerificationRequest) *pendingVerification {
	pending := &pendingVerification{
		ID:      nextJobID(),
		JobID:   jobID,
		FeedURL: request.URL,
		Journal: request.Journal,
		Target:  request.Target,
		Reason:  request.Reason,
		Result:  make(chan verificationResult, 1),
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
	pending.Delivered = false
	pending.CallbackReceived = false
	pending.CallbackResult = verificationResult{}
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
	if len(pending.CallbackResult.FeedXML) == 0 {
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
		Err:         result.Err,
		Warning:     result.Warning,
	}
	return pending, true, true
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
