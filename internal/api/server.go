package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	"github.com/yyngfive/scirssagent/internal/trayapp"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

type Server struct {
	settings    config.Settings
	settingsMu  sync.RWMutex
	version     string
	shutdown    func()
	storeMu     sync.RWMutex
	readStore   *store.Store
	writeStore  *store.Store
	updateMu    sync.Mutex
	updateCache *cachedUpdateStatus
}

func (s *Server) snapshotSettings() config.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settings
}

func (s *Server) replaceSettings(settings config.Settings) {
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsMu.Unlock()
}

type cachedUpdateStatus struct {
	payload   map[string]any
	expiresAt time.Time
}

const (
	updateStatusSuccessTTL = 5 * time.Minute
	updateStatusFailureTTL = 30 * time.Second
	updateManifestDNSName  = "feedmedaily-update.stassenger.top"
)

var (
	openExternalTargetFunc                                                                                           = appruntime.OpenExternalTarget
	lookupUpdateTXTFunc                                                                                              = net.LookupTXT
	selectReclassifyPaperIDsFunc                                                                                     = jobruntime.SelectPaperIDsForScope
	reclassifyPaperIDsFunc                                                                                           = jobruntime.ReclassifyPaperIDs
	rebuildLatestReportFunc                                                                                          = jobruntime.RebuildLatestReport
	runSyncFunc                                                                                                      = jobruntime.RunSync
	bootstrapProfileFunc                                                                                             = jobruntime.GenerateInitialProfileProposal
	generateProfileProposalFunc                                                                                      = jobruntime.GenerateProfileProposal
	listZoteroCollectionsFunc     func(config.Settings) (zoterosvc.CollectionsResponse, error)                       = zoterosvc.ListCollections
	savePaperToZoteroFunc         func(config.Settings, store.Paper, store.Classification, *string) (*string, error) = zoterosvc.SavePaper
	notifyTraySettingsChangedFunc                                                                                    = trayapp.NotifySettingsChanged
	abstractImageHTTPClient                                                                                          = &http.Client{Timeout: 20 * time.Second}
)

func NewServer(settings config.Settings, shutdown func()) *Server {
	logging.SetDefaultDir(settings.LogsDir)
	migrateExistingDatabase(settings)
	return &Server{
		settings: settings,
		version:  appruntime.PackageVersion(settings.RootDir),
		shutdown: shutdown,
	}
}

// migrateExistingDatabase applies schema and data repairs before read-only API stores are opened.
func migrateExistingDatabase(settings config.Settings) {
	if _, err := os.Stat(settings.DatabasePath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_, _ = logging.WriteDefault(logging.Event{
				Level: "warning", Component: "api.server", Action: "database_migration_stat_failed", Error: err.Error(),
			})
		}
		return
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level: "warning", Component: "api.server", Action: "database_migration_failed", Error: err.Error(),
		})
		return
	}
	if err := sqliteStore.Close(); err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level: "warning", Component: "api.server", Action: "database_migration_close_failed", Error: err.Error(),
		})
	}
}

func (s *Server) getReadStore() (*store.Store, error) {
	settings := s.snapshotSettings()
	s.storeMu.RLock()
	sqliteStore := s.readStore
	s.storeMu.RUnlock()
	if sqliteStore != nil {
		return sqliteStore, nil
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.readStore != nil {
		return s.readStore, nil
	}
	sqliteStore, err := store.OpenRead(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	s.readStore = sqliteStore
	return sqliteStore, nil
}

func (s *Server) getWriteStore() (*store.Store, error) {
	settings := s.snapshotSettings()
	s.storeMu.RLock()
	sqliteStore := s.writeStore
	s.storeMu.RUnlock()
	if sqliteStore != nil {
		return sqliteStore, nil
	}

	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	if s.writeStore != nil {
		return s.writeStore, nil
	}
	sqliteStore, err := store.OpenWrite(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	s.writeStore = sqliteStore
	return sqliteStore, nil
}

func (s *Server) Close() error {
	s.storeMu.Lock()
	readStore := s.readStore
	writeStore := s.writeStore
	s.readStore = nil
	s.writeStore = nil
	s.storeMu.Unlock()
	var closeErr error
	if readStore != nil {
		closeErr = errors.Join(closeErr, readStore.Close())
	}
	if writeStore != nil {
		closeErr = errors.Join(closeErr, writeStore.Close())
	}
	return closeErr
}

func (s *Server) cachedUpdateStatus(force bool) map[string]any {
	now := nowFunc().UTC()

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	if !force && s.updateCache != nil && now.Before(s.updateCache.expiresAt) {
		return cloneJSONMap(s.updateCache.payload)
	}

	payload := fetchUpdateStatus(s.snapshotSettings())
	ttl := updateStatusSuccessTTL
	if status, ok := payload["status"].(string); ok && status == "check_failed" {
		ttl = updateStatusFailureTTL
	}
	s.updateCache = &cachedUpdateStatus{
		payload:   cloneJSONMap(payload),
		expiresAt: now.Add(ttl),
	}
	return cloneJSONMap(payload)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/report/latest", s.handleReportLatest)
	mux.HandleFunc("/api/app/health", s.handleAppHealth)
	mux.HandleFunc("/api/app/meta", s.handleAppMeta)
	mux.HandleFunc("/api/app/update", s.handleAppUpdate)
	mux.HandleFunc("/api/app/open", s.handleAppOpen)
	mux.HandleFunc("/api/app/exit", s.handleAppExit)
	mux.HandleFunc("/api/settings/config", s.handleSettingsConfig)
	mux.HandleFunc("/api/settings/classifier-models/test", s.handleClassifierModelTest)
	mux.HandleFunc("/api/settings/feeds", s.handleSettingsFeeds)
	mux.HandleFunc("/api/settings/scheduler", s.handleSettingsScheduler)
	mux.HandleFunc("/api/profile/current", s.handleProfileCurrent)
	mux.HandleFunc("/api/profile/bootstrap", s.handleProfileBootstrap)
	mux.HandleFunc("/api/feedback", s.handleFeedback)
	mux.HandleFunc("/api/feedback/", s.handleFeedbackByID)
	mux.HandleFunc("/api/papers/", s.handlePaperByID)
	mux.HandleFunc("/api/profile/proposals", s.handleProfileProposals)
	mux.HandleFunc("/api/profile/proposals/generate", s.handleProfileProposalGenerate)
	mux.HandleFunc("/api/profile/proposals/", s.handleProfileProposalByID)
	mux.HandleFunc("/api/zotero/collections", s.handleZoteroCollections)
	mux.HandleFunc("/api/zotero/save/", s.handleZoteroSave)
	mux.HandleFunc("/api/admin/run", s.handleAdminRun)
	mux.HandleFunc("/api/admin/reclassify", s.handleAdminReclassify)
	mux.HandleFunc("/api/admin/jobs/", s.handleAdminJobByID)
	mux.HandleFunc("/api/admin/jobs", s.handleAdminJobs)
	mux.HandleFunc("/api/admin/llm-usage", s.handleAdminLLMUsage)
	mux.HandleFunc("/api/feeds/verification/start", s.handleFeedVerificationStart)
	mux.HandleFunc("/api/feeds/verification/browser", s.handleFeedVerificationBrowser)
	mux.HandleFunc("/api/feeds/verification/callback", s.handleFeedVerificationCallback)
	mux.HandleFunc("/api/feeds/verification/complete", s.handleFeedVerificationComplete)
	mux.HandleFunc("/api/feeds/verification/manual-submit", s.handleFeedVerificationManualSubmit)
	mux.HandleFunc("/", s.handleStatic)
	return withCORS(mux)
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	return false
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		return
	}
}

func stringPtr(value string) *string {
	clean := value
	return &clean
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return payload, nil
}

func cloneJSONMap(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	clone := make(map[string]any, len(payload))
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}

func emptyReportPayload() map[string]any {
	now := nowFunc().UTC()
	return map[string]any{
		"generated_at": now.Format(time.RFC3339),
		"report_date":  now.Format("2006-01-02"),
		"totals": map[string]int{
			"total":     0,
			"direct":    0,
			"indirect":  0,
			"unrelated": 0,
		},
		"papers": []any{},
		"errors": []any{},
	}
}
