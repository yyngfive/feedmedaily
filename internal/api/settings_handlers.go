package api

import (
	"encoding/json"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/feeds"
	"github.com/yyngfive/scirssagent/internal/logging"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
	"net/http"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleSettingsConfig(w http.ResponseWriter, r *http.Request) {
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

func schedulerResponse(settings appruntime.TraySchedulerSettings, mode string, settingsPath string, command string) map[string]any {
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
