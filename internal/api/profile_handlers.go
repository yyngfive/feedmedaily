package api

import (
	"encoding/json"
	"errors"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleProfileCurrent(w http.ResponseWriter, r *http.Request) {
	serverSettings := s.snapshotSettings()
	switch r.Method {
	case http.MethodGet:
		profilePayload, err := profile.ReadCurrent(serverSettings.ProfilePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profilePayload})
	case http.MethodPut:
		currentProfile, err := profile.ReadCurrent(serverSettings.ProfilePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if currentProfile == nil {
			writeError(w, http.StatusBadRequest, "No classification profile exists yet.")
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		updatedProfile, _, err := profile.PrepareUpdatedProfile(payload, currentProfile, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := profile.WriteCurrent(serverSettings.ProfilePath, updatedProfile); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": updatedProfile})
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (s *Server) handleProfileBootstrap(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	currentProfile, err := profile.ReadCurrent(serverSettings.ProfilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if currentProfile != nil {
		writeError(w, http.StatusBadRequest, "A classification profile already exists.")
		return
	}
	var payload struct {
		InterestDescription string  `json:"interest_description"`
		Name                *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(payload.InterestDescription) == "" {
		writeError(w, http.StatusBadRequest, "interest_description is required.")
		return
	}
	job := launchLocalJob(
		serverSettings,
		"profile-bootstrap",
		"profile.bootstrap.queued",
		"Queued initial profile generation.",
		"profile.bootstrap.generating",
		"Generating the initial classification profile proposal.",
		func(progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
			return bootstrapProfileFunc(serverSettings, payload.InterestDescription, payload.Name, progress, usage)
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sqliteStore, err := s.getReadStore()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusOK, []any{})
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items, err := sqliteStore.ListFeedback()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var payload struct {
			PaperID            int64   `json:"paper_id"`
			CorrectedRelevance string  `json:"corrected_relevance"`
			Note               *string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		sqliteStore, err := s.getWriteStore()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, "Paper not found.")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		record, err := sqliteStore.CreateFeedback(payload.PaperID, payload.CorrectedRelevance, payload.Note, time.Now().UTC())
		if err != nil {
			switch {
			case errors.Is(err, store.ErrPaperNotFound):
				writeError(w, http.StatusNotFound, "Paper not found.")
			case errors.Is(err, store.ErrClassificationNotFound):
				writeError(w, http.StatusBadRequest, "Paper has no classification yet.")
			default:
				writeError(w, http.StatusBadRequest, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, record)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (s *Server) handleProfileProposals(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items, err := sqliteStore.ListProfileProposals()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleProfileProposalGenerate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	serverSettings := s.snapshotSettings()
	currentProfile, err := profile.ReadCurrent(serverSettings.ProfilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if currentProfile == nil {
		writeError(w, http.StatusBadRequest, "No classification profile exists yet.")
		return
	}
	job := launchLocalJob(
		serverSettings,
		"profile-proposal",
		"",
		"",
		"profile.proposal.collecting_feedback",
		"Collecting feedback for profile review.",
		func(progress jobruntime.ProgressFunc, usage *llmusage.Collector) (map[string]any, error) {
			return generateProfileProposalFunc(serverSettings, progress, usage)
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleFeedbackByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	rawID := strings.TrimPrefix(r.URL.Path, "/api/feedback/")
	if rawID == "" || strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "Feedback not found.")
		return
	}
	feedbackID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Feedback not found.")
		return
	}
	sqliteStore, err := s.getWriteStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Feedback not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sqliteStore.DeleteFeedback(feedbackID); err != nil {
		if errors.Is(err, store.ErrFeedbackNotFound) {
			writeError(w, http.StatusNotFound, "Feedback not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "feedback_id": feedbackID})
}

func (s *Server) handlePaperByID(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimPrefix(r.URL.Path, "/api/papers/")
	parts := strings.Split(rawPath, "/")
	if len(parts) != 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	paperID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	if parts[1] == "abstract-image" {
		s.handlePaperAbstractImage(w, r, paperID)
		return
	}
	if parts[1] != "read" {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	read := true
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if strings.TrimSpace(string(data)) != "" {
			var payload struct {
				Read *bool `json:"read"`
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "Invalid JSON body.")
				return
			}
			if payload.Read != nil {
				read = *payload.Read
			}
		}
	}
	sqliteStore, err := s.getWriteStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Paper not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	readAt, err := sqliteStore.SetPaperRead(paperID, read, time.Now().UTC())
	if err != nil {
		if errors.Is(err, store.ErrPaperNotFound) {
			writeError(w, http.StatusNotFound, "Paper not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"paper_id": paperID, "read_at": readAt})
}

func (s *Server) handlePaperAbstractImage(w http.ResponseWriter, r *http.Request, paperID int64) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	src := strings.TrimSpace(r.URL.Query().Get("src"))
	if src == "" || !safeRemoteImageURL(src) {
		writeError(w, http.StatusBadRequest, "Unsupported image URL.")
		return
	}
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Paper not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	paper, err := sqliteStore.PaperByID(paperID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if paper == nil || !paperHasAbstractImage(*paper, src) {
		writeError(w, http.StatusNotFound, "Image not found.")
		return
	}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, src, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Unsupported image URL.")
		return
	}
	request.Header.Set("User-Agent", browserUserAgent())
	request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	if strings.TrimSpace(paper.URL) != "" {
		request.Header.Set("Referer", paper.URL)
	}
	response, err := abstractImageHTTPClient.Do(request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer response.Body.Close()
	contentType := response.Header.Get("Content-Type")
	if response.StatusCode < 200 || response.StatusCode >= 300 || !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeError(w, http.StatusBadGateway, "Image request failed.")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if length := response.Header.Get("Content-Length"); length != "" {
		w.Header().Set("Content-Length", length)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, response.Body)
}

func paperHasAbstractImage(paper store.Paper, src string) bool {
	for _, image := range paper.AbstractImages {
		if image.Src == src {
			return true
		}
	}
	return false
}

func safeRemoteImageURL(raw string) bool {
	parsed, err := neturl.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return false
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || (!ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified())
}

func browserUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
}

func (s *Server) handleProfileProposalByID(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimPrefix(r.URL.Path, "/api/profile/proposals/")
	if rawPath == "" {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}
	parts := strings.Split(rawPath, "/")
	if len(parts) > 2 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}
	proposalID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}

	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		s.handleProfileProposalDetail(w, proposalID)
	case len(parts) == 2 && parts[1] == "apply" && r.Method == http.MethodPost:
		s.handleProfileProposalApply(w, r, proposalID)
	case len(parts) == 2 && parts[1] == "reject" && r.Method == http.MethodPost:
		s.handleProfileProposalReject(w, proposalID)
	default:
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
	}
}

func (s *Server) handleProfileProposalDetail(w http.ResponseWriter, proposalID int64) {
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Profile proposal not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item, err := sqliteStore.GetProfileProposal(proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleProfileProposalApply(w http.ResponseWriter, r *http.Request, proposalID int64) {
	// 支持 legacy 整份 apply，以及带 accepted/rejected change ids 的局部 apply。
	serverSettings := s.snapshotSettings()
	sqliteStore, err := s.getWriteStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Profile proposal not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proposal, err := sqliteStore.GetProfileProposal(proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if proposal == nil {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}
	if proposal.State == "applied" {
		writeJSON(w, http.StatusOK, proposal)
		return
	}
	if proposal.State == "rejected" {
		writeError(w, http.StatusConflict, "Profile proposal has already been rejected.")
		return
	}

	currentProfile, err := profile.ReadCurrent(serverSettings.ProfilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC()
	appliedProfile := map[string]any(nil)
	version := 0
	finalizedChanges := proposal.Changes
	if len(proposal.Changes) == 0 {
		appliedProfile, version, err = profile.PrepareAppliedProfile(proposal.ProposedProfile, currentProfile, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		var payload struct {
			AcceptedChangeIDs []string `json:"accepted_change_ids"`
			RejectedChangeIDs []string `json:"rejected_change_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		currentVersion, err := profile.CurrentProfileVersion(currentProfile)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if proposal.BaseProfileVersion != currentVersion {
			writeError(w, http.StatusConflict, "Current profile version has changed since this proposal was generated. Regenerate the proposal and review it again.")
			return
		}
		finalizedChanges, err = profile.FinalizeProposalChanges(proposal.Changes, payload.AcceptedChangeIDs, payload.RejectedChangeIDs)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		appliedProfile, version, err = profile.PrepareAppliedProfileFromChanges(currentProfile, finalizedChanges, now)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := profile.WriteCurrent(serverSettings.ProfilePath, appliedProfile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	feedbackIDs := appliedProposalFeedbackIDs(proposal, finalizedChanges)
	if err := sqliteStore.ApplyProfileProposalState(proposalID, version, appliedProfile, finalizedChanges, now); err != nil {
		if errors.Is(err, store.ErrProfileProposalNotFound) {
			writeError(w, http.StatusNotFound, "Profile proposal not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sqliteStore.MarkFeedbackUsed(feedbackIDs); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	paperIDs, err := sqliteStore.PaperIDsForFeedbackIDs(feedbackIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updatedProposal, err := sqliteStore.GetProfileProposal(proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updatedProposal == nil {
		writeError(w, http.StatusInternalServerError, "Profile proposal disappeared after apply.")
		return
	}
	if len(paperIDs) > 0 {
		if _, err := reclassifyPaperIDsFunc(serverSettings, paperIDs, nil); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := rebuildLatestReportFunc(serverSettings, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updatedProposal)
}

func appliedProposalFeedbackIDs(proposal *store.ProfileProposal, finalizedChanges []profile.ProposalChange) []int64 {
	// Legacy proposals do not have per-change status, so keep their original top-level feedback scope.
	if proposal == nil || len(proposal.Changes) == 0 {
		if proposal == nil {
			return []int64{}
		}
		return store.FeedbackIDsToInt64(proposal.SourceFeedbackIDs)
	}
	seen := map[int64]struct{}{}
	ids := make([]int64, 0)
	for _, change := range finalizedChanges {
		if change.Status != profile.ProposalStatusAccepted {
			continue
		}
		for _, id := range change.SourceFeedbackIDs {
			if id <= 0 {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Server) handleProfileProposalReject(w http.ResponseWriter, proposalID int64) {
	sqliteStore, err := s.getWriteStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Profile proposal not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	item, err := sqliteStore.GetProfileProposal(proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "Profile proposal not found.")
		return
	}
	if err := sqliteStore.RejectProfileProposalState(proposalID, time.Now().UTC()); err != nil {
		if errors.Is(err, store.ErrProfileProposalNotFound) {
			writeError(w, http.StatusNotFound, "Profile proposal not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	updated, err := sqliteStore.GetProfileProposal(proposalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if updated == nil {
		writeError(w, http.StatusInternalServerError, "Profile proposal disappeared after reject.")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleZoteroCollections(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	payload, err := listZoteroCollectionsFunc(s.snapshotSettings())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleZoteroSave(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	rawID := strings.TrimPrefix(r.URL.Path, "/api/zotero/save/")
	if rawID == "" || strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	paperID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	var payload struct {
		CollectionKey *string `json:"collection_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	sqliteStore, err := s.getWriteStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "Paper not found.")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	paper, err := sqliteStore.PaperByID(paperID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if paper == nil {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	classification, err := sqliteStore.LatestClassification(paperID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if classification == nil {
		writeError(w, http.StatusBadRequest, "Paper has no classification yet.")
		return
	}
	current, err := sqliteStore.LatestZoteroStatus(paperID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if current != nil && current.Saved {
		writeJSON(w, http.StatusOK, current)
		return
	}
	itemKey, saveErr := savePaperToZoteroFunc(s.snapshotSettings(), *paper, *classification, payload.CollectionKey)
	if saveErr != nil {
		status, err := sqliteStore.UpsertZoteroStatus(paperID, "error", nil, stringPtr(saveErr.Error()), time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, status)
		return
	}
	status, err := sqliteStore.UpsertZoteroStatus(paperID, "saved", itemKey, nil, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}
