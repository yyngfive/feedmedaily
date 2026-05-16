package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	writeMutex      sync.Mutex
	defaultDirMutex sync.RWMutex
	defaultLogsDir  string
)

type Event struct {
	TS         string         `json:"ts"`
	Level      string         `json:"level"`
	Component  string         `json:"component"`
	Action     string         `json:"action"`
	JobID      string         `json:"job_id,omitempty"`
	Command    []string       `json:"command,omitempty"`
	MessageKey string         `json:"message_key,omitempty"`
	Message    string         `json:"message,omitempty"`
	Error      string         `json:"error,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

func DailyLogPath(logsDir string, now time.Time) string {
	return filepath.Join(logsDir, now.Format("2006-01-02")+".log")
}

func SetDefaultDir(logsDir string) {
	defaultDirMutex.Lock()
	defer defaultDirMutex.Unlock()
	defaultLogsDir = strings.TrimSpace(logsDir)
}

func DefaultDir() string {
	defaultDirMutex.RLock()
	defer defaultDirMutex.RUnlock()
	return defaultLogsDir
}

func WriteDefault(event Event) (string, error) {
	return Write(DefaultDir(), event)
}

func Write(logsDir string, event Event) (string, error) {
	logsDir = strings.TrimSpace(logsDir)
	if logsDir == "" {
		return "", nil
	}
	if event.TS == "" {
		event.TS = time.Now().Format(time.RFC3339Nano)
	}
	if event.Level == "" {
		event.Level = "info"
	}
	path := DailyLogPath(logsDir, time.Now())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create log directory: %w", err)
	}
	line, err := formatEvent(event)
	if err != nil {
		return "", fmt.Errorf("format log event: %w", err)
	}
	writeMutex.Lock()
	defer writeMutex.Unlock()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(line + "\n"); err != nil {
		return "", fmt.Errorf("write log event: %w", err)
	}
	return path, nil
}

func formatEvent(event Event) (string, error) {
	ts, err := parseTimestamp(event.TS)
	if err != nil {
		return "", err
	}
	level := strings.ToUpper(strings.TrimSpace(event.Level))
	if level == "" {
		level = "INFO"
	}

	message := strings.TrimSpace(event.Message)
	if message == "" {
		switch {
		case event.Error != "":
			message = fmt.Sprintf("%s.%s failed: %s", event.Component, event.Action, event.Error)
		case event.Component != "" || event.Action != "":
			message = strings.TrimSpace(event.Component + " " + event.Action)
		default:
			message = "log event"
		}
	}

	details := formatDetails(event)
	if details != "" {
		message = message + " " + details
	}

	return fmt.Sprintf("%s %s %s", ts.Format("2006-01-02 15:04:05,000"), level, message), nil
}

func parseTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Now(), nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts.Local(), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts.Local(), nil
	}
	return time.Time{}, fmt.Errorf("parse timestamp %q", raw)
}

func formatDetails(event Event) string {
	items := make([]string, 0)
	if event.JobID != "" {
		items = append(items, "job_id="+event.JobID)
	}
	if event.MessageKey != "" {
		items = append(items, "message_key="+event.MessageKey)
	}
	if len(event.Command) > 0 {
		items = append(items, "command="+strings.Join(event.Command, " "))
	}
	if event.Error != "" && !strings.Contains(event.Message, event.Error) {
		items = append(items, "error="+sanitizeValue(event.Error))
	}
	if len(event.Data) > 0 {
		keys := make([]string, 0, len(event.Data))
		for key := range event.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			items = append(items, key+"="+sanitizeValue(event.Data[key]))
		}
	}
	return strings.Join(items, " ")
}

func sanitizeValue(value any) string {
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	if strings.ContainsAny(text, " \t\"") {
		return fmt.Sprintf("%q", text)
	}
	return text
}
