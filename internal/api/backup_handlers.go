package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/yyngfive/scirssagent/internal/backups"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/llmusage"
)

// Guards profile/feed mutations (including proposal DB updates) across the
// snapshot boundary. Pipeline is acquired first by backup; guarded handlers
// may enqueue pipeline work, but never wait for that work synchronously.
var workDataConfigMu sync.Mutex

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	if !requireLocalBackupRequest(w, r) {
		return
	}
	settings := s.snapshotSettings()
	switch r.Method {
	case http.MethodGet:
		entries, err := backups.List(settings)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		_, statErr := os.Stat(settings.DatabasePath)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			writeError(w, 500, statErr.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"backups": entries, "database_exists": statErr == nil})
	case http.MethodPost:
		if _, err := os.Stat(settings.DatabasePath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, 409, "No database exists yet.")
			} else {
				writeError(w, 500, err.Error())
			}
			return
		}
		release, ok := tryLockPipeline()
		if !ok {
			writeError(w, 409, "A sync, reclassification, cleanup, or backup job is already running.")
			return
		}
		job := launchLocalJob(settings, "backup", "backup.queued", "Backup queued.", "backup.preparing", "Preparing backup.",
			func(ctx context.Context, progress jobruntime.ProgressFunc, _ *llmusage.Collector) (map[string]any, error) {
				entry, err := backups.Create(ctx, settings, s.version, &workDataConfigMu, func(stage string) {
					jobruntime.EmitProgress(progress, jobruntime.ProgressUpdate{Stage: "backup", Message: stage, Label: stage})
				})
				if err != nil {
					return nil, err
				}
				return map[string]any{"backup": entry}, nil
			}, func(context.Context) (func(), error) { return release, nil })
		writeJSON(w, 200, map[string]any{"job": job})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, 405, "Method not allowed.")
	}
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if !requireLocalBackupRequest(w, r) {
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/admin/backups/")
	if !strings.HasSuffix(tail, "/download") {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(tail, "/download")
	path, err := backups.Path(s.snapshotSettings(), id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
		} else {
			writeError(w, 500, err.Error())
		}
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(id))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

func requireLocalBackupRequest(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		writeError(w, http.StatusForbidden, "Work-data backups are available only from this computer.")
		return false
	}
	return true
}
