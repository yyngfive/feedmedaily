//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/yyngfive/scirssagent/internal/logging"
)

type verifierLogger struct {
	cfg         verifierConfig
	logsDir     string
	executable  string
	binaryMTime string
}

func newVerifierLogger(cfg verifierConfig) *verifierLogger {
	logger := &verifierLogger{
		cfg:     cfg,
		logsDir: filepath.Join(strings.TrimSpace(cfg.LogsDir), "verifier"),
	}
	executable, err := os.Executable()
	if err == nil {
		logger.executable = executable
		if info, statErr := os.Stat(executable); statErr == nil {
			logger.binaryMTime = info.ModTime().Format(timeLayout)
		}
	}
	return logger
}

func (l *verifierLogger) info(action string, message string, data map[string]any) {
	l.write("info", action, message, "", data)
}

func (l *verifierLogger) warning(action string, message string, errText string, data map[string]any) {
	l.write("warning", action, message, errText, data)
}

func (l *verifierLogger) error(action string, message string, errText string, data map[string]any) {
	l.write("error", action, message, errText, data)
}

func (l *verifierLogger) write(level string, action string, message string, errText string, data map[string]any) {
	if l == nil || strings.TrimSpace(l.logsDir) == "" {
		return
	}
	merged := map[string]any{
		"verification_id": l.cfg.VerificationID,
		"feed_url":        l.cfg.FeedURL,
		"pid":             os.Getpid(),
	}
	if strings.TrimSpace(l.cfg.JobID) != "" {
		merged["verifier_job_id"] = l.cfg.JobID
	}
	if strings.TrimSpace(l.cfg.AppVersion) != "" {
		merged["app_version"] = l.cfg.AppVersion
	}
	if strings.TrimSpace(l.executable) != "" {
		merged["exe_path"] = l.executable
	}
	if strings.TrimSpace(l.binaryMTime) != "" {
		merged["binary_mtime"] = l.binaryMTime
	}
	for key, value := range data {
		merged[key] = value
	}
	_, _ = logging.Write(l.logsDir, logging.Event{
		Level:     level,
		Component: "verifier",
		Action:    action,
		JobID:     l.cfg.JobID,
		Message:   message,
		Error:     errText,
		Data:      merged,
	})
}

const timeLayout = "2006-01-02T15:04:05.000Z07:00"
