package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	jobruntime "github.com/yyngfive/scirssagent/internal/jobs"
	"github.com/yyngfive/scirssagent/internal/logging"
	"github.com/yyngfive/scirssagent/internal/profile"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	zoterosvc "github.com/yyngfive/scirssagent/internal/zotero"
)

type Server struct {
	settings    config.Settings
	version     string
	shutdown    func()
	storeMu     sync.RWMutex
	readStore   *store.Store
	writeStore  *store.Store
	updateMu    sync.Mutex
	updateCache *cachedUpdateStatus
}

type cachedUpdateStatus struct {
	payload   map[string]any
	expiresAt time.Time
}

const (
	updateStatusSuccessTTL = 5 * time.Minute
	updateStatusFailureTTL = 30 * time.Second
)

var (
	openExternalTargetFunc                                                                                          = appruntime.OpenExternalTarget
	fetchUpdateManifestFunc                                                                                         = defaultFetchUpdateManifest
	selectReclassifyPaperIDsFunc                                                                                    = jobruntime.SelectPaperIDsForScope
	reclassifyPaperIDsFunc                                                                                          = jobruntime.ReclassifyPaperIDs
	rebuildLatestReportFunc                                                                                         = jobruntime.RebuildLatestReport
	runSyncFunc                                                                                                     = jobruntime.RunSync
	bootstrapProfileFunc                                                                                            = jobruntime.GenerateInitialProfileProposal
	generateProfileProposalFunc                                                                                     = jobruntime.GenerateProfileProposal
	listZoteroCollectionsFunc    func(config.Settings) (zoterosvc.CollectionsResponse, error)                       = zoterosvc.ListCollections
	savePaperToZoteroFunc        func(config.Settings, store.Paper, store.Classification, *string) (*string, error) = zoterosvc.SavePaper
)

func NewServer(settings config.Settings, shutdown func()) *Server {
	// 用当前解析好的 settings 创建一个可挂到 net/http 上的 API 服务器。
	logging.SetDefaultDir(settings.LogsDir)
	return &Server{
		settings: settings,
		version:  appruntime.PackageVersion(settings.RootDir),
		shutdown: shutdown,
	}
}

func (s *Server) getReadStore() (*store.Store, error) {
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
	sqliteStore, err := store.OpenRead(s.settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	s.readStore = sqliteStore
	return sqliteStore, nil
}

func (s *Server) getWriteStore() (*store.Store, error) {
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
	sqliteStore, err := store.OpenWrite(s.settings.DatabasePath)
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

func (s *Server) cachedUpdateStatus() map[string]any {
	now := nowFunc().UTC()

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	if s.updateCache != nil && now.Before(s.updateCache.expiresAt) {
		return cloneJSONMap(s.updateCache.payload)
	}

	payload := fetchUpdateStatus(s.settings)
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
	// 注册当前 Go 迁移阶段已经接管的全部路由。
	mux := http.NewServeMux()
	mux.HandleFunc("/api/report/latest", s.handleReportLatest)
	mux.HandleFunc("/api/app/health", s.handleAppHealth)
	mux.HandleFunc("/api/app/meta", s.handleAppMeta)
	mux.HandleFunc("/api/app/update", s.handleAppUpdate)
	mux.HandleFunc("/api/app/open", s.handleAppOpen)
	mux.HandleFunc("/api/app/exit", s.handleAppExit)
	mux.HandleFunc("/api/settings/config", s.handleSettingsConfig)
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
	mux.HandleFunc("/api/feeds/verification/start", s.handleFeedVerificationStart)
	mux.HandleFunc("/api/feeds/verification/callback", s.handleFeedVerificationCallback)
	mux.HandleFunc("/api/feeds/verification/complete", s.handleFeedVerificationComplete)
	mux.HandleFunc("/", s.handleStatic)
	return withCORS(mux)
}

func (s *Server) handleReportLatest(w http.ResponseWriter, r *http.Request) {
	// 从 SQLite 实时组装最新报告，避免继续依赖磁盘 report 快照。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sqliteStore, err := s.getReadStore()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, emptyReportPayload())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	report, err := sqliteStore.BuildLatestReport(time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleAppHealth(w http.ResponseWriter, r *http.Request) {
	// 供托盘和外部调用方做最轻量的服务探活。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"name":       appruntime.AppPublicName,
		"version":    s.version,
		"mode":       s.settings.Mode,
		"server_url": requestBaseURL(r),
	})
}

func (s *Server) handleAppMeta(w http.ResponseWriter, r *http.Request) {
	// 返回前端和托盘都需要的运行路径、版本和进程状态信息。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	processRunning := false
	if state, err := appruntime.ReadState(s.settings.RuntimeStatePath); err == nil && state != nil {
		processRunning = appruntime.ProcessRunning(state.PID)
	}
	updateManifestURL := any(nil)
	if s.settings.UpdateManifestURL != "" {
		updateManifestURL = s.settings.UpdateManifestURL
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                appruntime.AppPublicName,
		"version":             s.version,
		"mode":                s.settings.Mode,
		"install_dir":         s.settings.AppDir,
		"config_dir":          s.settings.ConfigDir,
		"data_dir":            s.settings.DataDir,
		"logs_dir":            s.settings.LogsDir,
		"static_dir":          s.settings.WebDistDir,
		"tray_settings_path":  filepath.Join(s.settings.ConfigDir, "tray-settings.json"),
		"server_url":          requestBaseURL(r),
		"scheduler_task_name": appruntime.SchedulerTaskName,
		"update_manifest_url": updateManifestURL,
		"process_running":     processRunning,
	})
}

func (s *Server) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	// 检查远程 update manifest，告诉前端是否有新版本。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, s.cachedUpdateStatus())
}

func (s *Server) handleAppOpen(w http.ResponseWriter, r *http.Request) {
	// 按前端传入的 target 打开目录、服务地址或更新链接。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	updateStatus := map[string]any(nil)
	if payload.Target == "download_url" || payload.Target == "release_notes_url" {
		updateStatus = s.cachedUpdateStatus()
	}
	target, err := resolveAppOpenTarget(s.settings, payload.Target, requestBaseURL(r), updateStatus)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := openExternalTargetFunc(target); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": "open",
		"target": payload.Target,
		"detail": target,
	})
}

func (s *Server) handleAppExit(w http.ResponseWriter, r *http.Request) {
	// 触发本地服务优雅退出，并先返回成功响应给前端/托盘。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": "exit",
		"detail": "Shutting down the local FeedMeDaily service.",
	})
	if s.shutdown != nil {
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = appruntime.ClearState(s.settings.RuntimeStatePath)
			s.shutdown()
		}()
	}
}

func (s *Server) handleSettingsConfig(w http.ResponseWriter, r *http.Request) {
	// 兼容前端的设置页读写接口。
	switch r.Method {
	case http.MethodGet:
		response, err := config.SettingsConfig(s.settings.RootDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
	case http.MethodPut:
		var payload config.SettingsConfigUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		response, err := config.UpdateLocalSettings(s.settings.RootDir, payload.Fields)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (s *Server) handleSettingsFeeds(w http.ResponseWriter, r *http.Request) {
	// 兼容 RSS 订阅列表的读取和保存。
	switch r.Method {
	case http.MethodGet:
		subscriptions, err := feeds.ReadSubscriptions(s.settings.FeedsPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, subscriptions)
	case http.MethodPut:
		var payload feeds.SettingsUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		subscriptions, err := feeds.WriteSubscriptions(s.settings.FeedsPath, payload.Feeds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, subscriptions)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (s *Server) handleSettingsScheduler(w http.ResponseWriter, r *http.Request) {
	// 用 tray-settings.json 模拟当前阶段的 scheduler 设置接口。
	path := filepath.Join(s.settings.ConfigDir, "tray-settings.json")
	switch r.Method {
	case http.MethodGet:
		settings, err := appruntime.LoadTraySchedulerSettings(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		command, err := backendRunCommandFunc(s.settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, schedulerResponse(settings, s.settings.Mode, path, strings.Join(command, " ")))
	case http.MethodPut:
		var payload struct {
			DailyTime string `json:"daily_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON body.")
			return
		}
		normalized := appruntime.NormalizeDailyTime(payload.DailyTime)
		if normalized == "" {
			writeError(w, http.StatusBadRequest, "daily_time must be in HH:MM format.")
			return
		}
		settings, err := appruntime.LoadTraySchedulerSettings(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settings.ScheduleEnabled = true
		settings.DailyTime = normalized
		if err := settings.Save(path); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		command, err := backendRunCommandFunc(s.settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, schedulerResponse(settings, s.settings.Mode, path, strings.Join(command, " ")))
	case http.MethodDelete:
		settings, err := appruntime.LoadTraySchedulerSettings(path)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		settings.ScheduleEnabled = false
		if err := settings.Save(path); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		command, err := backendRunCommandFunc(s.settings)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, schedulerResponse(settings, s.settings.Mode, path, strings.Join(command, " ")))
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	}
}

func (s *Server) handleProfileCurrent(w http.ResponseWriter, r *http.Request) {
	// 读取或保存当前 profile 文件，保持 {"profile": ...} 响应结构。
	switch r.Method {
	case http.MethodGet:
		profilePayload, err := profile.ReadCurrent(s.settings.ProfilePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profile": profilePayload})
	case http.MethodPut:
		currentProfile, err := profile.ReadCurrent(s.settings.ProfilePath)
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
		if err := profile.WriteCurrent(s.settings.ProfilePath, updatedProfile); err != nil {
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
	// 用 Go 原生 job 生成初始 profile proposal，并写入 SQLite proposal 表。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	currentProfile, err := profile.ReadCurrent(s.settings.ProfilePath)
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
		s.settings.LogsDir,
		"profile-bootstrap",
		"profile.bootstrap.queued",
		"Queued initial profile generation.",
		"profile.bootstrap.generating",
		"Generating the initial classification profile proposal.",
		func(progress jobruntime.ProgressFunc) (map[string]any, error) {
			return bootstrapProfileFunc(s.settings, payload.InterestDescription, payload.Name, progress)
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	// 统一承接 feedback 列表读取和创建。
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
	// 从 SQLite 返回真实 proposal 列表，供前端 review 面板加载。
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
	// 用 Go 原生 job 从当前 profile 和 open feedback 生成新的 proposal。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	currentProfile, err := profile.ReadCurrent(s.settings.ProfilePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if currentProfile == nil {
		writeError(w, http.StatusBadRequest, "No classification profile exists yet.")
		return
	}
	job := launchLocalJob(
		s.settings.LogsDir,
		"profile-proposal",
		"",
		"",
		"profile.proposal.collecting_feedback",
		"Collecting feedback for profile review.",
		func(progress jobruntime.ProgressFunc) (map[string]any, error) {
			return generateProfileProposalFunc(s.settings, progress)
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleFeedbackByID(w http.ResponseWriter, r *http.Request) {
	// 兼容 DELETE /api/feedback/{id}。
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
	// 兼容 POST /api/papers/{id}/read。
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}
	rawPath := strings.TrimPrefix(r.URL.Path, "/api/papers/")
	parts := strings.Split(rawPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "read" {
		writeError(w, http.StatusNotFound, "Paper not found.")
		return
	}
	paperID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Paper not found.")
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
	readAt, err := sqliteStore.MarkPaperRead(paperID, time.Now().UTC())
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

func (s *Server) handleProfileProposalByID(w http.ResponseWriter, r *http.Request) {
	// 同时处理 proposal detail、apply 和 reject。
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
	// 补齐单条 proposal 详情读取，和 Python API 保持一致。
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

	currentProfile, err := profile.ReadCurrent(s.settings.ProfilePath)
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
	if err := profile.WriteCurrent(s.settings.ProfilePath, appliedProfile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	feedbackIDs := store.FeedbackIDsToInt64(proposal.SourceFeedbackIDs)
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
		if _, err := reclassifyPaperIDsFunc(s.settings, paperIDs, nil); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := rebuildLatestReportFunc(s.settings, nil); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updatedProposal)
}

func (s *Server) handleProfileProposalReject(w http.ResponseWriter, proposalID int64) {
	// 复刻 Python reject proposal 的本地状态更新。
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
	// 用 Go 原生 Zotero client 返回 collections，保持前端当前同步加载模型。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	payload, err := listZoteroCollectionsFunc(s.settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleZoteroSave(w http.ResponseWriter, r *http.Request) {
	// 直接调用 Go 原生 Zotero 保存逻辑，并同步刷新本地 zotero_saves 状态。
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
	itemKey, saveErr := savePaperToZoteroFunc(s.settings, *paper, *classification, payload.CollectionKey)
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

func (s *Server) handleAdminRun(w http.ResponseWriter, r *http.Request) {
	// 用 Go 原生 fetch -> enrich -> classify -> report pipeline 执行一次完整同步。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	job := launchVerificationAwareSyncJob(
		s.settings,
		func(progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string) (map[string]any, error) {
			summary, err := runSyncFunc(s.settings, jobruntime.RunOptions{
				FeedBodyOverrides: overrides,
				SkippedFeeds:      skippedFeeds,
			}, progress)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"fetched":    summary.Fetched,
				"inserted":   summary.Inserted,
				"updated":    summary.Updated,
				"classified": summary.Classified,
				"errors":     summary.Errors,
			}, nil
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleFeedVerificationStart(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
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
	if err := startVerificationFlowFunc(s.settings, pending); err != nil {
		logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_start_failed", "pipeline.feeds.verification_required", "", err.Error(), map[string]any{
			"verification_id":       pending.ID,
			"verification_feed_url": pending.FeedURL,
			"verification_journal":  pending.Journal,
		})
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_started", "pipeline.feeds.verification_required", "Opened the feed verification window.", "", map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
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
	result, err := completeVerificationFlowFunc(s.settings, pending)
	logData := map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
	}
	if err != nil {
		logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_retry_failed", "pipeline.feeds.verification_required", "", err.Error(), logData)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(result.ContentType) != "" {
		logData["content_type"] = result.ContentType
	}
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_completed", "pipeline.feeds.fetching", "Verification received. Re-running RSS fetch with verified XML.", "", logData)
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
	var payload verificationCallbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	ack, err := processVerificationCallback(s.settings, payload)
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

func (s *Server) handleAdminReclassify(w http.ResponseWriter, r *http.Request) {
	// 用 Go 原生 metadata + classifier + report 读模型执行重分类作业。
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var payload struct {
		Scope string `json:"scope"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(payload.Scope) == "" {
		payload.Scope = "recent"
	}
	if payload.Scope != "recent" && payload.Scope != "feedback" && payload.Scope != "all" {
		writeError(w, http.StatusBadRequest, "scope must be recent, feedback, or all.")
		return
	}
	if payload.Limit == 0 {
		payload.Limit = 50
	}
	if payload.Limit < 1 || payload.Limit > 500 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 500.")
		return
	}
	job := launchLocalJob(
		s.settings.LogsDir,
		"reclassify",
		"job.started",
		"Job queued.",
		"pipeline.metadata.enriching",
		"Getting metadata for papers to reclassify.",
		func(progress jobruntime.ProgressFunc) (map[string]any, error) {
			paperIDs, err := selectReclassifyPaperIDsFunc(s.settings, payload.Scope, payload.Limit)
			if err != nil {
				return nil, err
			}
			reclassified, err := reclassifyPaperIDsFunc(s.settings, paperIDs, progress)
			if err != nil {
				return nil, err
			}
			reportCount, err := rebuildLatestReportFunc(s.settings, progress)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"scope":         payload.Scope,
				"paper_ids":     paperIDs,
				"reclassified":  reclassified,
				"report_papers": reportCount,
			}, nil
		},
	)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) handleAdminJobs(w http.ResponseWriter, r *http.Request) {
	// 返回当前 Go 侧维护的后台作业列表。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, listJobs())
}

func (s *Server) handleAdminJobByID(w http.ResponseWriter, r *http.Request) {
	// 返回单个后台作业详情，供前端轮询状态。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/api/admin/jobs/")
	if jobID == "" || strings.Contains(jobID, "/") {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	job, ok := jobByID(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "Job not found.")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	// 提供静态资源直出和 SPA fallback，保证浏览器可打开前端。
	if strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/"), "api/") {
		writeError(w, http.StatusNotFound, "API route not found.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}

	if filePath, ok := staticFileForPath(s.settings.WebDistDir, r.URL.Path); ok {
		http.ServeFile(w, r, filePath)
		return
	}
	indexPath := filepath.Join(s.settings.WebDistDir, "index.html")
	if fileExists(indexPath) {
		http.ServeFile(w, r, indexPath)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("<h1>FeedMeDaily</h1><p>Frontend build not found. Build the web assets before packaging this release.</p>"))
}

func staticFileForPath(distDir string, rawPath string) (string, bool) {
	// 只允许访问 web/dist 内部的现有文件，避免目录穿越。
	cleanPath := filepath.Clean(strings.TrimPrefix(rawPath, "/"))
	if cleanPath == "." || strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
		return "", false
	}
	requested := filepath.Join(distDir, cleanPath)
	rel, err := filepath.Rel(distDir, requested)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	if !fileExists(requested) {
		return "", false
	}
	return requested, true
}

func fileExists(path string) bool {
	// 判断目标路径是否是存在的普通文件。
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func requestBaseURL(r *http.Request) string {
	// 反推出当前请求看到的服务根 URL。
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	// 做统一的 HTTP 方法校验，不符合时自动返回 405。
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
	return false
}

func writeError(w http.ResponseWriter, status int, detail string) {
	// 保持与现有前端兼容的 {"detail": "..."} 错误结构。
	writeJSON(w, status, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	// 统一输出 JSON 响应。
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		return
	}
}

func stringPtr(value string) *string {
	// 为需要可选字符串指针的写路径提供一个轻量 helper。
	clean := value
	return &clean
}

func withCORS(next http.Handler) http.Handler {
	// 当前本地服务对前端放开简单的跨域访问。
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
	// 读取一个磁盘 JSON 对象文件；文件不存在时返回 nil。
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
	// 在 report 尚未生成时返回一个结构完整的空报告。
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

func defaultFetchUpdateManifest(manifestURL string) (map[string]any, error) {
	// 默认的 update manifest 拉取实现。
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get(manifestURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("update manifest request failed with %s", response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func fetchUpdateStatus(settings config.Settings) map[string]any {
	// 把远程 manifest 解析成前端消费的统一更新状态对象。
	currentVersion := appruntime.PackageVersion(settings.RootDir)
	payload := map[string]any{
		"status":            "not_configured",
		"current_version":   currentVersion,
		"latest_version":    nil,
		"has_update":        false,
		"download_url":      nil,
		"release_notes_url": nil,
		"detail":            "Update checks are not configured for this build.",
		"checked_at":        nowFunc().UTC().Format(time.RFC3339),
	}
	if settings.UpdateManifestURL == "" {
		return payload
	}

	manifest, err := fetchUpdateManifestFunc(settings.UpdateManifestURL)
	if err != nil {
		payload["status"] = "check_failed"
		payload["detail"] = err.Error()
		return payload
	}

	latestVersion := strings.TrimSpace(fmt.Sprintf("%v", manifest["version"]))
	if latestVersion == "<nil>" {
		latestVersion = ""
	}
	downloadURL := strings.TrimSpace(fmt.Sprintf("%v", manifest["download_url"]))
	if downloadURL == "<nil>" {
		downloadURL = ""
	}
	releaseNotesURL := strings.TrimSpace(fmt.Sprintf("%v", manifest["release_notes_url"]))
	if releaseNotesURL == "<nil>" {
		releaseNotesURL = ""
	}

	hasUpdate := latestVersion != "" && appruntime.IsNewerVersion(latestVersion, currentVersion)
	payload["status"] = "up_to_date"
	if hasUpdate {
		payload["status"] = "update_available"
	}
	if latestVersion == "" {
		payload["detail"] = "Manifest did not include a version field."
	} else {
		payload["detail"] = nil
	}
	payload["latest_version"] = nil
	if latestVersion != "" {
		payload["latest_version"] = latestVersion
	}
	payload["has_update"] = hasUpdate
	payload["download_url"] = nil
	if downloadURL != "" {
		payload["download_url"] = downloadURL
	}
	payload["release_notes_url"] = nil
	if releaseNotesURL != "" {
		payload["release_notes_url"] = releaseNotesURL
	}
	return payload
}

func resolveAppOpenTarget(settings config.Settings, target string, serverURL string, update map[string]any) (string, error) {
	// 把抽象 target 名字解析成真正要打开的路径或 URL。
	mapping := map[string]string{
		"data_dir":    settings.DataDir,
		"logs_dir":    settings.LogsDir,
		"install_dir": settings.AppDir,
		"server_url":  serverURL,
	}
	if update != nil {
		if downloadURL, ok := update["download_url"].(string); ok && downloadURL != "" {
			mapping["download_url"] = downloadURL
		}
		if releaseNotesURL, ok := update["release_notes_url"].(string); ok && releaseNotesURL != "" {
			mapping["release_notes_url"] = releaseNotesURL
		}
	}
	resolved := mapping[target]
	if resolved == "" {
		return "", fmt.Errorf("Unsupported app open target: %s", target)
	}
	return resolved, nil
}

func schedulerResponse(settings appruntime.TraySchedulerSettings, mode string, settingsPath string, command string) map[string]any {
	// 把托盘本地调度设置转成前端现有 scheduler API 的响应格式。
	automaticSupported := goruntime.GOOS == "windows"
	advisory := any(nil)
	if !automaticSupported {
		advisoryText := "This platform currently only saves the daily sync setting locally. Automatic runs are not started from the Web UI; use cron or another system scheduler instead."
		if strings.TrimSpace(command) != "" {
			advisoryText = "This platform currently only saves the daily sync setting locally. Automatic runs are not started from the Web UI; use cron or another system scheduler with the helper command below instead."
		}
		advisory = advisoryText
	}
	response := map[string]any{
		"installed":           settings.ScheduleEnabled,
		"task_name":           appruntime.SchedulerTaskName,
		"mode":                mode,
		"scheduler_backend":   "tray_local",
		"scheduled_time":      settings.DailyTime,
		"settings_path":       settingsPath,
		"command":             command,
		"state":               nil,
		"next_run_time":       nil,
		"last_run_time":       nil,
		"last_result":         nil,
		"platform":            goruntime.GOOS,
		"automatic_supported": automaticSupported,
		"advisory":            advisory,
	}
	if settings.ScheduleEnabled {
		if automaticSupported {
			response["state"] = "Enabled"
			if nextRun, ok := nextScheduledRun(settings.DailyTime, nowFunc()); ok {
				response["next_run_time"] = nextRun.Format(time.RFC3339)
			}
			if lastRun, ok := lastScheduledRun(settings.LastRunDate, settings.DailyTime, nowFunc().Location()); ok {
				response["last_run_time"] = lastRun.Format(time.RFC3339)
				response["last_result"] = 0
			}
		} else {
			response["state"] = "Saved only"
		}
	}
	return response
}

func nextScheduledRun(dailyTime string, now time.Time) (time.Time, bool) {
	// 根据 HH:MM 计算下一次应该触发的本地时间。
	normalized := appruntime.NormalizeDailyTime(dailyTime)
	if normalized == "" {
		return time.Time{}, false
	}
	parts := strings.Split(normalized, ":")
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next, true
}

func lastScheduledRun(lastRunDate string, dailyTime string, location *time.Location) (time.Time, bool) {
	// 根据上次运行日期和日常时间推算上次运行的时间戳。
	if strings.TrimSpace(lastRunDate) == "" {
		return time.Time{}, false
	}
	normalized := appruntime.NormalizeDailyTime(dailyTime)
	if normalized == "" {
		return time.Time{}, false
	}
	parts := strings.Split(normalized, ":")
	hour, _ := strconv.Atoi(parts[0])
	minute, _ := strconv.Atoi(parts[1])
	day, err := time.ParseInLocation("2006-01-02", lastRunDate, location)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, location), true
}
