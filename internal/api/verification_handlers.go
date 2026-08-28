package api

import (
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/feeds"
	"net/http"
	"strings"
)

func (s *Server) handleFeedVerificationStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	var payload struct {
		JobID   string `json:"job_id"`
		FeedURL string `json:"feed_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	job, ok := jobByID(payload.JobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	if job.Status != "waiting_for_user" || !job.VerificationRequired {
		writeError(w, http.StatusBadRequest, "Job is not waiting for manual verification.")
		return
	}
	pending, ok := pendingVerificationForJob(payload.JobID, payload.FeedURL)
	if !ok {
		writeError(w, http.StatusNotFound, "Verification request not found.")
		return
	}
	terminateVerifierProcess(serverSettings, pending.ID)
	if err := startVerificationFlowFunc(serverSettings, pending); err != nil {
		logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_start_failed", "pipeline.feeds.verification_required", "", err.Error(), map[string]any{
			"verification_id":       pending.ID,
			"verification_feed_url": pending.FeedURL,
			"verification_journal":  pending.Journal,
		})
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_started", "pipeline.feeds.verification_required", "Opened the feed verification window.", "", map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
	})
	updateJob(pending.JobID, func(current *jobInfo) {
		current.Status = "waiting_for_user"
		current.MessageKey = "pipeline.feeds.verification_required"
		current.Message = "A protected feed needs Cloudflare verification. Finish it in the verification window or use browser fallback and paste the final RSS XML."
		current.VerificationRequired = true
		current.VerificationTarget = pending.Target
		current.VerificationFeedURL = pending.FeedURL
		current.VerificationJournal = pending.Journal
		current.VerificationHost = pending.Host
		current.VerificationMethod = verificationMethodNativeWebview
		current.VerificationSessionState = pending.SessionState
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"verification_id": pending.ID,
	})
}

func (s *Server) handleFeedVerificationBrowser(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	var payload struct {
		JobID   string `json:"job_id"`
		FeedURL string `json:"feed_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	job, ok := jobByID(payload.JobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	if job.Status != "waiting_for_user" || !job.VerificationRequired {
		writeError(w, http.StatusBadRequest, "Job is not waiting for manual verification.")
		return
	}
	pending, ok := pendingVerificationForJob(payload.JobID, payload.FeedURL)
	if !ok {
		writeError(w, http.StatusNotFound, "Verification request not found.")
		return
	}
	resetPendingVerificationAttempt(pending, verificationMethodBrowserManual)
	if err := openExternalTargetFunc(pending.FeedURL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_browser_opened", "pipeline.feeds.verification_required", "Opened the protected feed in the system browser for manual verification.", "", map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
	})
	updateJob(pending.JobID, func(current *jobInfo) {
		current.Status = "waiting_for_user"
		current.MessageKey = "pipeline.feeds.verification_required"
		current.Message = "Finish the Cloudflare check in your browser, wait until the final RSS XML is visible, then paste it here."
		current.VerificationRequired = true
		current.VerificationTarget = pending.Target
		current.VerificationFeedURL = pending.FeedURL
		current.VerificationJournal = pending.Journal
		current.VerificationHost = pending.Host
		current.VerificationMethod = verificationMethodBrowserManual
		current.VerificationSessionState = pending.SessionState
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"verification_id": pending.ID,
	})
}

func (s *Server) handleFeedVerificationComplete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	var payload struct {
		JobID   string `json:"job_id"`
		FeedURL string `json:"feed_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	job, ok := jobByID(payload.JobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	if job.Status != "waiting_for_user" || !job.VerificationRequired {
		writeError(w, http.StatusBadRequest, "Job is not waiting for manual verification.")
		return
	}
	pending, ok := pendingVerificationForJob(payload.JobID, payload.FeedURL)
	if !ok {
		writeError(w, http.StatusNotFound, "Verification request not found.")
		return
	}
	result, err := completeVerificationFlowFunc(serverSettings, pending)
	logData := map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
	}
	if err != nil {
		logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_retry_failed", "pipeline.feeds.verification_required", "", err.Error(), logData)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(result.ContentType) != "" {
		logData["content_type"] = result.ContentType
	}
	logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_completed", "pipeline.feeds.fetching", "Verification received. Continuing RSS fetch with verified XML.", "", logData)
	select {
	case pending.Result <- result:
	default:
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verification_id": pending.ID})
}

func (s *Server) handleFeedVerificationCallback(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	var payload verificationCallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	ack, err := processVerificationCallback(serverSettings, payload)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ack)
}

func (s *Server) handleFeedVerificationManualSubmit(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	var payload struct {
		JobID   string `json:"job_id"`
		FeedURL string `json:"feed_url"`
		FeedXML string `json:"feed_xml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	job, ok := jobByID(payload.JobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	if job.Status != "waiting_for_user" || !job.VerificationRequired {
		writeError(w, http.StatusBadRequest, "Job is not waiting for manual verification.")
		return
	}
	pending, ok := pendingVerificationForJob(payload.JobID, payload.FeedURL)
	if !ok {
		writeError(w, http.StatusNotFound, "Verification request not found.")
		return
	}
	logData := map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
	}
	logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_manual_submit_started", "pipeline.feeds.verification_required", "Validating manually pasted RSS XML.", "", logData)
	if strings.TrimSpace(payload.FeedXML) == "" {
		logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_manual_submit_rejected", "pipeline.feeds.verification_required", "", "feed XML is required", logData)
		writeError(w, http.StatusBadRequest, "Feed XML is required.")
		return
	}
	normalizedXML, err := feeds.ValidateFeedXML(payload.FeedURL, []byte(payload.FeedXML))
	if err != nil {
		logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_manual_submit_rejected", "pipeline.feeds.verification_required", "", err.Error(), logData)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resetPendingVerificationAttempt(pending, verificationMethodBrowserManual)
	ack, err := processVerificationCallback(serverSettings, verificationCallbackPayload{
		VerificationID: pending.ID,
		Status:         "success",
		ContentType:    "application/xml",
		FeedXML:        string(normalizedXML),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logJobEvent(serverSettings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_manual_submit_accepted", "pipeline.feeds.verification_required", "Accepted manually pasted RSS XML.", "", logData)
	writeJSON(w, http.StatusOK, ack)
}
