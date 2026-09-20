package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/yyngfive/scirssagent/internal/journals"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	"net/http"
	"os"
	"time"
)

func (s *Server) handleJournalOptions(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	db, err := s.getReadStore()
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string]any{"journals": []journals.Identity{}, "timezone": store.TimezoneLabel(time.Now(), time.Local)})
		return
	}
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	options, err := db.JournalOptions()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"journals": options, "timezone": store.TimezoneLabel(time.Now(), time.Local)})
}

func (s *Server) previewCustomReclassify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := store.PaperFilter{JournalKeys: q["journal_key"], DateFrom: q.Get("date_from"), DateTo: q.Get("date_to")}
	db, err := s.getReadStore()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	preview, err := db.SelectCustomPapers(filter, time.Local)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, preview)
}

func (s *Server) previewCustomReclassifyPost(w http.ResponseWriter, r *http.Request) {
	var filter store.PaperFilter
	if err := json.NewDecoder(r.Body).Decode(&filter); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	db, err := s.getReadStore()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	preview, err := db.SelectCustomPapers(filter, time.Local)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) launchCustomReclassify(w http.ResponseWriter, filter store.PaperFilter, fingerprint string) {
	if fingerprint == "" {
		writeError(w, 400, "Preview the selection before starting.")
		return
	}
	release, locked := tryLockPipeline()
	if !locked {
		writeError(w, 409, "A sync, reclassification, cleanup, or backup job is already running.")
		return
	}
	handedOff := false
	defer func() {
		if !handedOff {
			release()
		}
	}()
	db, err := s.getReadStore()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	selection, err := db.SelectCustomPapers(filter, time.Local)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if selection.Fingerprint != fingerprint {
		writeJSON(w, 409, map[string]any{"detail": "Matching papers changed. Review the refreshed count and start again.", "preview": selection})
		return
	}
	if selection.Total == 0 {
		writeError(w, 400, "No papers match this range.")
		return
	}
	settings := s.snapshotSettings()
	job := launchLocalJob(settings, "reclassify", "job.started", "Job queued.", "pipeline.metadata.enriching", "Getting metadata for papers to reclassify.",
		reclassifyJobRunFunc(settings, "custom", func() ([]int64, error) { return selection.PaperIDs, nil }, nil),
		func(context.Context) (func(), error) { return release, nil })
	handedOff = true
	writeJSON(w, 200, map[string]any{"job": job})
}
