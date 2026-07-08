package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
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
	"github.com/yyngfive/scirssagent/internal/trayapp"
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

func (s *Server) cachedUpdateStatus(force bool) map[string]any {
	now := nowFunc().UTC()

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	if !force && s.updateCache != nil && now.Before(s.updateCache.expiresAt) {
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
	mux.HandleFunc("/api/feeds/verification/browser", s.handleFeedVerificationBrowser)
	mux.HandleFunc("/api/feeds/verification/callback", s.handleFeedVerificationCallback)
	mux.HandleFunc("/api/feeds/verification/complete", s.handleFeedVerificationComplete)
	mux.HandleFunc("/api/feeds/verification/manual-submit", s.handleFeedVerificationManualSubmit)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                appruntime.AppPublicName,
		"version":             s.version,
		"mode":                s.settings.Mode,
		"install_dir":         s.settings.AppDir,
		"config_dir":          s.settings.ConfigDir,
		"tray_instance_id":    appruntime.TrayInstanceID(s.settings.ConfigDir),
		"data_dir":            s.settings.DataDir,
		"logs_dir":            s.settings.LogsDir,
		"static_dir":          s.settings.WebDistDir,
		"tray_settings_path":  filepath.Join(s.settings.ConfigDir, "tray-settings.json"),
		"server_url":          requestBaseURL(r),
		"scheduler_task_name": appruntime.SchedulerTaskName,
		"process_running":     processRunning,
	})
}

func (s *Server) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	// 检查固定 DNS TXT 更新记录，告诉前端是否有新版本。
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	force := r.URL.Query().Get("force") == "1"
	writeJSON(w, http.StatusOK, s.cachedUpdateStatus(force))
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
		updateStatus = s.cachedUpdateStatus(false)
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
		updatedSettings, err := config.Load(s.settings.RootDir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.settings = updatedSettings
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
		s.notifyTraySettingsChanged()
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
		s.notifyTraySettingsChanged()
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

func (s *Server) notifyTraySettingsChanged() {
	// Web 保存共享调度配置后，尽力通知正在运行的托盘立刻刷新内存态。
	if err := notifyTraySettingsChangedFunc(s.settings.ConfigDir); err != nil {
		_, _ = logging.Write(s.settings.LogsDir, logging.Event{
			Level:     "warning",
			Component: "api",
			Action:    "tray_settings_notify_failed",
			Message:   "Saved scheduler settings, but notifying the running tray failed.",
			Error:     err.Error(),
			Data: map[string]any{
				"config_dir":         s.settings.ConfigDir,
				"tray_instance_id":   appruntime.TrayInstanceID(s.settings.ConfigDir),
				"tray_settings_path": filepath.Join(s.settings.ConfigDir, "tray-settings.json"),
			},
		})
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
	// 兼容 /api/papers/{id}/read，并代理当前论文自己的摘要图片。
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
	var payload struct {
		FeedURLs []string `json:"feed_urls"`
	}
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		if strings.TrimSpace(string(data)) != "" {
			if err := json.Unmarshal(data, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "Invalid JSON body.")
				return
			}
		}
	}
	selectedFeedURLs, err := validateSelectedFeedURLs(s.settings.FeedsPath, payload.FeedURLs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	job := launchVerificationAwareSyncJob(
		s.settings,
		func(progress jobruntime.ProgressFunc, overrides map[string][]byte, skippedFeeds map[string]string, verifyHost feeds.VerifyHostFunc) (map[string]any, error) {
			summary, err := runSyncFunc(s.settings, jobruntime.RunOptions{
				SelectedFeedURLs:  selectedFeedURLs,
				FeedBodyOverrides: overrides,
				SkippedFeeds:      skippedFeeds,
				VerifyFeedHost:    verifyHost,
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

func validateSelectedFeedURLs(feedsPath string, requested []string) ([]string, error) {
	selected := make([]string, 0, len(requested))
	seen := map[string]struct{}{}
	for _, rawURL := range requested {
		feedURL := strings.TrimSpace(rawURL)
		if feedURL == "" {
			continue
		}
		if _, ok := seen[feedURL]; ok {
			continue
		}
		seen[feedURL] = struct{}{}
		selected = append(selected, feedURL)
	}
	if len(selected) == 0 {
		return nil, nil
	}
	subscriptions, err := feeds.ReadSubscriptions(feedsPath)
	if err != nil {
		return nil, err
	}
	saved := map[string]struct{}{}
	for _, subscription := range subscriptions {
		saved[strings.TrimSpace(subscription.URL)] = struct{}{}
	}
	for _, feedURL := range selected {
		if _, ok := saved[feedURL]; !ok {
			return nil, fmt.Errorf("feed_urls contains an unknown feed URL: %s", feedURL)
		}
	}
	return selected, nil
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
	terminateVerifierProcess(s.settings, pending.ID)
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
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_browser_opened", "pipeline.feeds.verification_required", "Opened the protected feed in the system browser for manual verification.", "", map[string]any{
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
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_completed", "pipeline.feeds.fetching", "Verification received. Continuing RSS fetch with verified XML.", "", logData)
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

func (s *Server) handleFeedVerificationManualSubmit(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
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
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_manual_submit_started", "pipeline.feeds.verification_required", "Validating manually pasted RSS XML.", "", logData)
	if strings.TrimSpace(payload.FeedXML) == "" {
		logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_manual_submit_rejected", "pipeline.feeds.verification_required", "", "feed XML is required", logData)
		writeError(w, http.StatusBadRequest, "Feed XML is required.")
		return
	}
	normalizedXML, err := feeds.ValidateFeedXML(payload.FeedURL, []byte(payload.FeedXML))
	if err != nil {
		logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_manual_submit_rejected", "pipeline.feeds.verification_required", "", err.Error(), logData)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resetPendingVerificationAttempt(pending, verificationMethodBrowserManual)
	ack, err := processVerificationCallback(s.settings, verificationCallbackPayload{
		VerificationID: pending.ID,
		Status:         "success",
		ContentType:    "application/xml",
		FeedXML:        string(normalizedXML),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logJobEvent(s.settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_manual_submit_accepted", "pipeline.feeds.verification_required", "Accepted manually pasted RSS XML.", "", logData)
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

func fetchUpdateManifestFromDNS() (map[string]any, error) {
	// 读取固定 DNS TXT 记录；发布流程保证记录包含 version 和 url。
	records, err := lookupUpdateTXTFunc(updateManifestDNSName)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		manifest := parseUpdateTXT(record)
		if manifest["version"] != "" && manifest["url"] != "" {
			return map[string]any{
				"version":           manifest["version"],
				"download_url":      manifest["url"],
				"release_notes_url": manifest["url"],
			}, nil
		}
	}
	return nil, fmt.Errorf("DNS TXT %s must include version and url", updateManifestDNSName)
}

func parseUpdateTXT(record string) map[string]string {
	// 解析 version=...;url=... 这种极简 manifest。
	result := map[string]string{}
	for _, part := range strings.Split(record, ";") {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key == "version" || key == "url" {
			result[key] = value
		}
	}
	return result
}

func fetchUpdateStatus(settings config.Settings) map[string]any {
	// 把 DNS TXT manifest 解析成前端消费的统一更新状态对象。
	currentVersion := appruntime.PackageVersion(settings.RootDir)
	payload := map[string]any{
		"status":            "checking",
		"current_version":   currentVersion,
		"latest_version":    nil,
		"has_update":        false,
		"download_url":      nil,
		"release_notes_url": nil,
		"detail":            nil,
		"checked_at":        nowFunc().UTC().Format(time.RFC3339),
	}

	manifest, err := fetchUpdateManifestFromDNS()
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
	if latestVersion == "" || (downloadURL == "" && releaseNotesURL == "") {
		payload["status"] = "check_failed"
		payload["detail"] = fmt.Sprintf("DNS TXT %s must include version and url", updateManifestDNSName)
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
		"tray_instance_id":    appruntime.TrayInstanceID(filepath.Dir(settingsPath)),
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
