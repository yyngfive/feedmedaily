package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/profile"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) handleReportLatest(w http.ResponseWriter, r *http.Request) {
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
	// 注册表快照 + 主题计数由 API 层组装：论文列表不等待 profile 之外的任何水合，
	// 注册表随报告一并返回，前端解析 id→label 无需二次请求。
	serverSettings := s.snapshotSettings()
	if profilePayload, err := profile.ReadCurrent(serverSettings.ProfilePath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	} else if profilePayload != nil {
		attachReportTopics(&report, profilePayload)
	}
	writeJSON(w, http.StatusOK, report)
}

// attachReportTopics 结合当前 profile 注册表与报告论文填充主题展示块。
func attachReportTopics(report *store.Report, profilePayload map[string]any) {
	topics := store.ReportTopics{Items: []store.ReportTopic{}, Counts: map[string]int{}}
	if rawTopics, ok := profilePayload["topic_taxonomy"].([]any); ok {
		for _, rawTopic := range rawTopics {
			topic, ok := rawTopic.(map[string]any)
			if !ok {
				continue
			}
			id, _ := topic["id"].(string)
			label, _ := topic["label"].(string)
			if strings.TrimSpace(id) == "" || strings.TrimSpace(label) == "" {
				continue
			}
			topics.Items = append(topics.Items, store.ReportTopic{ID: strings.TrimSpace(id), Label: strings.TrimSpace(label)})
		}
	}
	valid := map[string]struct{}{}
	for _, topic := range topics.Items {
		valid[topic.ID] = struct{}{}
	}
	for _, paper := range report.Papers {
		switch paper.Classification.Relevance {
		case "direct", "indirect":
		default:
			continue
		}
		tags := paper.Classification.TopicTags
		assigned := false
		for _, tag := range tags {
			if _, ok := valid[tag]; ok {
				topics.Counts[tag]++
				assigned = true
				break
			}
		}
		if len(tags) == 0 {
			// 相关但从未做过主题判定。
			topics.Unprocessed++
		} else if !assigned {
			// 哨兵 none 或已删除主题的孤儿 id。
			topics.Unassigned++
		}
	}
	report.Topics = &topics
}

func (s *Server) handleAppHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	settings := s.snapshotSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"name":       appruntime.AppPublicName,
		"version":    s.version,
		"mode":       settings.Mode,
		"server_url": requestBaseURL(r),
	})
}

func (s *Server) handleAppMeta(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	settings := s.snapshotSettings()
	processRunning := false
	if state, err := appruntime.ReadState(settings.RuntimeStatePath); err == nil && state != nil {
		processRunning = appruntime.ProcessRunning(state.PID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                appruntime.AppPublicName,
		"version":             s.version,
		"mode":                settings.Mode,
		"install_dir":         settings.AppDir,
		"config_dir":          settings.ConfigDir,
		"tray_instance_id":    appruntime.TrayInstanceID(settings.ConfigDir),
		"data_dir":            settings.DataDir,
		"logs_dir":            settings.LogsDir,
		"static_dir":          settings.WebDistDir,
		"tray_settings_path":  filepath.Join(settings.ConfigDir, "tray-settings.json"),
		"server_url":          requestBaseURL(r),
		"scheduler_task_name": appruntime.SchedulerTaskName,
		"process_running":     processRunning,
	})
}

func (s *Server) handleAppUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	force := r.URL.Query().Get("force") == "1"
	writeJSON(w, http.StatusOK, s.cachedUpdateStatus(force))
}

func (s *Server) handleAppOpen(w http.ResponseWriter, r *http.Request) {
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
	settings := s.snapshotSettings()
	updateStatus := map[string]any(nil)
	if payload.Target == "download_url" || payload.Target == "release_notes_url" {
		updateStatus = s.cachedUpdateStatus(false)
	}
	target, err := resolveAppOpenTarget(settings, payload.Target, requestBaseURL(r), updateStatus)
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
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": "exit",
		"detail": "Shutting down the local FeedMeDaily service.",
	})
	if s.shutdown != nil {
		settings := s.snapshotSettings()
		go func() {
			time.Sleep(200 * time.Millisecond)
			_ = appruntime.ClearState(settings.RuntimeStatePath)
			s.shutdown()
		}()
	}
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/"), "api/") {
		writeError(w, http.StatusNotFound, "API route not found.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
		return
	}

	settings := s.snapshotSettings()
	if filePath, ok := staticFileForPath(settings.WebDistDir, r.URL.Path); ok {
		http.ServeFile(w, r, filePath)
		return
	}
	indexPath := filepath.Join(settings.WebDistDir, "index.html")
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
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func fetchUpdateManifestFromDNS() (map[string]any, error) {
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
