package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
)

type syncExecuteFunc func(progress func(string, string), overrides map[string][]byte, skippedFeeds map[string]string) (map[string]any, error)

type verificationAwareSyncCallbacks struct {
	OnWaiting                 func(*pendingVerification)
	OnVerificationStartFailed func(*pendingVerification, error)
	OnVerificationStarted     func(*pendingVerification)
	OnVerificationSkipped     func(*pendingVerification, verificationResult)
	OnVerificationResumed     func(*pendingVerification)
}

type verificationCallbackPayload struct {
	VerificationID string `json:"verification_id"`
	Status         string `json:"status"`
	ContentType    string `json:"content_type"`
	FeedXML        string `json:"feed_xml"`
	Error          string `json:"error"`
}

type verificationCallbackAck struct {
	OK             bool   `json:"ok"`
	VerificationID string `json:"verification_id"`
	Acknowledged   bool   `json:"acknowledged,omitempty"`
	CloseWindow    bool   `json:"close_window,omitempty"`
	Duplicate      bool   `json:"duplicate,omitempty"`
}

func runVerificationAwareSync(settings config.Settings, jobID string, callbackURL string, progress func(string, string), run syncExecuteFunc, callbacks verificationAwareSyncCallbacks) (map[string]any, error) {
	overrideBodies := map[string][]byte{}
	skippedFeeds := map[string]string{}
	if strings.TrimSpace(jobID) == "" {
		jobID = "sync-" + nextJobID()
	}

	for {
		result, err := run(progress, overrideBodies, skippedFeeds)
		if err == nil {
			return result, nil
		}

		var verificationErr *jobruntime.VerificationRequiredError
		if !errors.As(err, &verificationErr) || len(verificationErr.Requests) == 0 {
			return nil, err
		}

		pending := storePendingVerificationWithCallback(jobID, verificationErr.Requests[0], callbackURL)
		if callbacks.OnWaiting != nil {
			callbacks.OnWaiting(pending)
		}

		if err := startVerificationFlowFunc(settings, pending); err != nil {
			deletePendingVerification(pending.ID)
			skippedFeeds[pending.FeedURL] = err.Error()
			if callbacks.OnVerificationStartFailed != nil {
				callbacks.OnVerificationStartFailed(pending, err)
			}
			continue
		}

		if callbacks.OnVerificationStarted != nil {
			callbacks.OnVerificationStarted(pending)
		}

		resumeResult := waitForVerification(pending, 20*time.Minute)
		deletePendingVerification(pending.ID)
		if strings.TrimSpace(resumeResult.Warning) != "" {
			skippedFeeds[pending.FeedURL] = resumeResult.Warning
			if callbacks.OnVerificationSkipped != nil {
				callbacks.OnVerificationSkipped(pending, resumeResult)
			}
			continue
		}

		overrideBodies[pending.FeedURL] = resumeResult.FeedXML
		if callbacks.OnVerificationResumed != nil {
			callbacks.OnVerificationResumed(pending)
		}
	}
}

func processVerificationCallback(settings config.Settings, payload verificationCallbackPayload) (verificationCallbackAck, error) {
	pending, ok := pendingVerificationByID(payload.VerificationID)
	if !ok {
		return verificationCallbackAck{}, fmt.Errorf("Verification request not found.")
	}

	result := verificationResult{
		Status:      strings.TrimSpace(payload.Status),
		ContentType: strings.TrimSpace(payload.ContentType),
		FeedXML:     []byte(payload.FeedXML),
	}
	switch result.Status {
	case "success":
		if len(result.FeedXML) == 0 {
			result.Err = fmt.Errorf("verification callback did not include RSS XML")
			result.Warning = result.Err.Error()
		}
	case "aborted":
		if strings.TrimSpace(payload.Error) == "" {
			result.Warning = "the verification window was closed before RSS XML was captured"
		} else {
			result.Warning = strings.TrimSpace(payload.Error)
		}
	case "failed":
		if strings.TrimSpace(payload.Error) == "" {
			result.Warning = "the verification window failed before it could capture RSS XML"
		} else {
			result.Warning = strings.TrimSpace(payload.Error)
		}
	default:
		return verificationCallbackAck{}, fmt.Errorf("verification status must be success, failed, or aborted.")
	}

	var stored bool
	pending, ok, stored = storeVerificationCallbackResult(payload.VerificationID, result)
	if !ok {
		return verificationCallbackAck{}, fmt.Errorf("Verification request not found.")
	}
	if !stored {
		return verificationCallbackAck{
			OK:             true,
			VerificationID: pending.ID,
			Acknowledged:   true,
			CloseWindow:    result.Status == "success",
			Duplicate:      true,
		}, nil
	}

	logData := map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
		"verification_status":   result.Status,
	}
	if result.ContentType != "" {
		logData["content_type"] = result.ContentType
	}
	if result.Status == "success" {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_callback_received", "pipeline.feeds.verification_required", "Verification window captured protected feed XML.", "", logData)
	} else {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_callback_failed", "pipeline.feeds.verification_required", "", result.Warning, logData)
	}

	if markVerificationDelivered(pending.ID) {
		if result.Status == "success" {
			logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_completed", "pipeline.feeds.fetching", "Verification data captured. Resuming RSS fetch.", "", logData)
			go func(logsDir string, jobID string, verificationID string, feedURL string, journal string) {
				time.Sleep(2 * time.Second)
				process, ok := snapshotVerifierProcess(verificationID)
				if !ok || process.Exited {
					return
				}
				logJobEvent(logsDir, &jobInfo{ID: jobID}, "warning", "verification_process_still_running", "pipeline.feeds.verification_required", "Verifier window is still running after the backend acknowledged the XML callback.", "", map[string]any{
					"verification_id":         verificationID,
					"verification_feed_url":   feedURL,
					"verification_journal":    journal,
					"verification_pid":        process.PID,
					"verification_started_at": process.StartedAt.Format(time.RFC3339Nano),
				})
			}(settings.LogsDir, pending.JobID, pending.ID, pending.FeedURL, pending.Journal)
		}
		select {
		case pending.Result <- result:
		default:
		}
	}

	return verificationCallbackAck{
		OK:             true,
		VerificationID: pending.ID,
		Acknowledged:   true,
		CloseWindow:    result.Status == "success",
	}, nil
}
