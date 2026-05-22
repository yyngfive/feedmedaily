package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

type pendingVerification struct {
	ID      string
	JobID   string
	FeedURL string
	Target  string
	Reason  string
	Result  chan verificationResult
}

type verificationResult struct {
	Status      string
	ContentType string
	FeedXML     []byte
	Err         error
}

type verificationRegistry struct {
	mu    sync.Mutex
	items map[string]*pendingVerification
}

var (
	apiVerifications             = verificationRegistry{items: map[string]*pendingVerification{}}
	launchVerificationHelperFunc = launchVerificationHelper
)

func launchVerificationAwareRunJob(settings config.Settings, run func(progress func(string, string), overrides map[string][]byte) (map[string]any, error)) jobInfo {
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
			result, err := run(progress, overrideBodies)
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
				})
				return
			}

			var verificationErr *jobruntime.VerificationRequiredError
			if errors.As(err, &verificationErr) && len(verificationErr.Requests) > 0 {
				request := verificationErr.Requests[0]
				pending := storePendingVerification(job.ID, request)
				logJobEvent(settings.LogsDir, &job, "warning", "waiting_for_user", "pipeline.feeds.verification_required", "ChemRxiv requires manual verification.", "", map[string]any{
					"verification_target":   pending.Target,
					"verification_feed_url": pending.FeedURL,
				})
				updateJob(job.ID, func(current *jobInfo) {
					current.Status = "waiting_for_user"
					current.MessageKey = "pipeline.feeds.verification_required"
					current.Message = "ChemRxiv requires manual verification before this run can continue."
					current.Result = map[string]any{
						"verification_required": true,
						"verification_target":   pending.Target,
						"verification_feed_url": pending.FeedURL,
					}
					current.VerificationRequired = true
					current.VerificationTarget = pending.Target
					current.VerificationFeedURL = pending.FeedURL
				})

				resumeResult, waitErr := waitForVerification(pending, 20*time.Minute)
				deletePendingVerification(pending.ID)
				if waitErr != nil {
					finished := nowFunc().UTC()
					logJobEvent(settings.LogsDir, &job, "error", "failed", "run.failed", "", waitErr.Error(), nil)
					updateJob(job.ID, func(current *jobInfo) {
						current.Status = "failed"
						current.MessageKey = "run.failed"
						current.Error = waitErr.Error()
						current.FinishedAt = &finished
						current.VerificationRequired = false
						current.VerificationTarget = ""
						current.VerificationFeedURL = ""
					})
					return
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

func waitForVerification(pending *pendingVerification, timeout time.Duration) (verificationResult, error) {
	select {
	case result := <-pending.Result:
		if result.Err != nil {
			return verificationResult{}, result.Err
		}
		if len(result.FeedXML) == 0 {
			return verificationResult{}, fmt.Errorf("verification completed without returning RSS XML")
		}
		return result, nil
	case <-time.After(timeout):
		return verificationResult{}, fmt.Errorf("verification timed out")
	}
}

func launchVerificationHelper(settings config.Settings, verificationID string, callbackURL string, feedURL string) error {
	if verificationTargetForFeedURL(feedURL) != "chemrxiv" {
		return fmt.Errorf("unsupported verification target")
	}
	if strings.TrimSpace(callbackURL) == "" {
		return fmt.Errorf("verification callback URL is required")
	}
	if !isWindowsRuntime() {
		return fmt.Errorf("manual ChemRxiv verification is only supported on Windows")
	}
	if settings.Mode != appruntime.ModeSource {
		return fmt.Errorf("manual ChemRxiv verification currently requires source mode")
	}
	if _, err := exec.LookPath("dotnet"); err != nil {
		return fmt.Errorf("dotnet SDK/runtime is required to launch the WebView2 verifier")
	}
	projectPath := filepath.Join(settings.RootDir, "tools", "ChemRxivVerifier", "ChemRxivVerifier.csproj")
	args := []string{
		"run",
		"--project",
		projectPath,
		"--configuration",
		"Release",
		"--no-launch-profile",
		"--",
		"--verification-id", verificationID,
		"--feed-url", feedURL,
		"--callback-url", callbackURL,
	}
	cmd := exec.Command("dotnet", args...)
	cmd.Dir = settings.RootDir
	hideVerificationLauncherWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("%s: %w", detail, err)
		}
		return err
	}
	return nil
}

func verificationTargetForFeedURL(feedURL string) string {
	parsed, err := neturl.Parse(feedURL)
	if err != nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(parsed.Hostname()), "chemrxiv.org") {
		return "chemrxiv"
	}
	return ""
}

func postVerificationResult(callbackURL string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("callback returned %s", response.Status)
	}
	return nil
}
