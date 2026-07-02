package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
)

type syncExecuteFunc func(progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string) (map[string]any, error)

type verificationAwareSyncCallbacks struct {
	OnWaiting                 func(*pendingVerification)
	OnVerificationStartFailed func(*pendingVerification, error)
	OnVerificationStarted     func(*pendingVerification)
	OnVerificationSkipped     func(*pendingVerification, verificationResult)
	OnVerificationResumed     func(*pendingVerification)
}

type verificationCallbackPayload struct {
	VerificationID   string                     `json:"verification_id"`
	VerificationHost string                     `json:"verification_host,omitempty"`
	FeedURL          string                     `json:"feed_url,omitempty"`
	Status           string                     `json:"status"`
	ContentType      string                     `json:"content_type"`
	FeedXML          string                     `json:"feed_xml"`
	CapturedFeeds    []verificationCapturedFeed `json:"captured_feeds,omitempty"`
	SessionVerified  bool                       `json:"session_verified,omitempty"`
	Error            string                     `json:"error"`
}

type verificationCapturedFeed struct {
	FeedURL     string `json:"feed_url"`
	ContentType string `json:"content_type,omitempty"`
	FeedXML     string `json:"feed_xml"`
}

type verificationCallbackAck struct {
	OK             bool   `json:"ok"`
	VerificationID string `json:"verification_id"`
	Acknowledged   bool   `json:"acknowledged,omitempty"`
	CloseWindow    bool   `json:"close_window,omitempty"`
	Duplicate      bool   `json:"duplicate,omitempty"`
}

func runVerificationAwareSync(settings config.Settings, jobID string, callbackURL string, progress jobruntime.ProgressFunc, run syncExecuteFunc, callbacks verificationAwareSyncCallbacks) (map[string]any, error) {
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

		groupedRequests := groupVerificationRequests(verificationErr.Requests)
		pending := storePendingVerificationWithCallback(jobID, groupedRequests[0], groupedRequests, callbackURL)
		session, sessionErr := verificationHostSessionForHost(settings, pending.Host)
		if sessionErr == nil && strings.TrimSpace(session.State) != "" {
			pending.SessionState = session.State
		}
		if callbacks.OnWaiting != nil && pending.SessionState != verificationSessionStateVerified {
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

		resumeResult := waitForVerification(pending, 10*time.Minute)
		deletePendingVerification(pending.ID)
		if strings.TrimSpace(resumeResult.Warning) != "" {
			terminateVerifierProcess(settings, pending.ID)
			skippedFeeds[pending.FeedURL] = resumeResult.Warning
			if callbacks.OnVerificationSkipped != nil {
				callbacks.OnVerificationSkipped(pending, resumeResult)
			}
			continue
		}

		if len(resumeResult.FeedBodies) > 0 {
			for feedURL, body := range resumeResult.FeedBodies {
				overrideBodies[feedURL] = body
			}
		} else {
			overrideBodies[pending.FeedURL] = resumeResult.FeedXML
		}
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
		FeedBodies:  map[string][]byte{},
	}
	for _, item := range payload.CapturedFeeds {
		feedURL := strings.TrimSpace(item.FeedURL)
		if feedURL == "" || strings.TrimSpace(item.FeedXML) == "" {
			continue
		}
		result.FeedBodies[feedURL] = []byte(item.FeedXML)
		if len(result.FeedXML) == 0 {
			result.FeedXML = []byte(item.FeedXML)
			if result.ContentType == "" {
				result.ContentType = strings.TrimSpace(item.ContentType)
			}
		}
	}
	if len(result.FeedBodies) == 0 {
		result.FeedBodies = nil
	}
	switch result.Status {
	case "success":
		if len(result.FeedXML) == 0 && len(result.FeedBodies) == 0 {
			result.Err = fmt.Errorf("verification callback did not include RSS XML")
			result.Warning = result.Err.Error()
		}
	case "needs_user":
		if payload.SessionVerified || pending.Host != "" {
			if _, err := markVerificationHostSessionNeedsReverify(settings, pending.Host, pending.VerifierKind, "challenge"); err != nil {
				return verificationCallbackAck{}, err
			}
		}
		updateJob(pending.JobID, func(current *jobInfo) {
			current.Status = "waiting_for_user"
			current.MessageKey = "pipeline.feeds.verification_required"
			current.Message = "This protected feed host still needs a manual Cloudflare approval in the verification window."
			current.VerificationRequired = true
			current.VerificationTarget = pending.Target
			current.VerificationFeedURL = pending.FeedURL
			current.VerificationJournal = pending.Journal
			current.VerificationHost = pending.Host
			current.VerificationMethod = pending.Method
			current.VerificationSessionState = verificationSessionStateNeedsReverify
		})
		return verificationCallbackAck{
			OK:             true,
			VerificationID: pending.ID,
			Acknowledged:   true,
			CloseWindow:    false,
		}, nil
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
		return verificationCallbackAck{}, fmt.Errorf("verification status must be success, failed, aborted, or needs_user.")
	}

	var stored bool
	pending, ok, stored = storeVerificationCallbackResult(payload.VerificationID, result)
	if !ok {
		return verificationCallbackAck{}, fmt.Errorf("Verification request not found.")
	}
	if !stored {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_callback_duplicate_ignored", "pipeline.feeds.verification_required", "Ignored a duplicate verification callback for an already handled request.", "", map[string]any{
			"verification_id":       pending.ID,
			"verification_feed_url": pending.FeedURL,
			"verification_journal":  pending.Journal,
			"verification_host":     pending.Host,
			"verification_status":   result.Status,
		})
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
		"verification_host":     pending.Host,
		"verification_status":   result.Status,
	}
	if result.ContentType != "" {
		logData["content_type"] = result.ContentType
	}
	if result.Status == "success" {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_callback_received", "pipeline.feeds.verification_required", "Verification window captured protected feed XML.", "", logData)
	} else {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_callback_failed", "pipeline.feeds.verification_required", "", result.Warning, logData)
		updateJob(pending.JobID, func(current *jobInfo) {
			current.Status = "waiting_for_user"
			current.MessageKey = "pipeline.feeds.verification_required"
			current.Message = "The verification window did not reach feed XML. Reopen it or use browser fallback and paste the final RSS XML."
			current.VerificationRequired = true
			current.VerificationTarget = pending.Target
			current.VerificationFeedURL = pending.FeedURL
			current.VerificationJournal = pending.Journal
			current.VerificationHost = pending.Host
			current.VerificationMethod = pending.Method
			current.VerificationSessionState = verificationSessionStateNeedsReverify
		})
	}

	shouldDeliver := result.Status == "success"
	if shouldDeliver && markVerificationDelivered(pending.ID) {
		if payload.SessionVerified || pending.Host != "" {
			session, err := markVerificationHostSessionVerified(settings, pending.Host, pending.VerifierKind)
			if err != nil {
				return verificationCallbackAck{}, err
			}
			logData["verification_session_state"] = session.State
		}
		if result.Status == "success" {
			logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_completed", "pipeline.feeds.fetching", "Verification received. Re-running RSS fetch with verified XML.", "", logData)
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

func groupVerificationRequests(requests []feeds.VerificationRequest) []feeds.VerificationRequest {
	if len(requests) == 0 {
		return nil
	}
	primary := requests[0]
	host := verificationProfileHost(primary.URL)
	if host == "" || host == "default" {
		return []feeds.VerificationRequest{primary}
	}
	grouped := make([]feeds.VerificationRequest, 0, len(requests))
	seen := map[string]struct{}{}
	for _, item := range requests {
		if verificationProfileHost(item.URL) != host {
			continue
		}
		if _, ok := seen[item.URL]; ok {
			continue
		}
		seen[item.URL] = struct{}{}
		grouped = append(grouped, item)
	}
	if len(grouped) == 0 {
		return []feeds.VerificationRequest{primary}
	}
	return grouped
}
