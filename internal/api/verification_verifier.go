package api

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const verificationBinaryName = "FeedMeDailyVerifier.exe"

type verifierProcess struct {
	VerificationID string
	JobID          string
	FeedURL        string
	Journal        string
	BinaryPath     string
	Command        []string
	PID            int
	StartedAt      time.Time
	Exited         bool
	ExitCode       int
	ExitError      string
}

var verifierProcesses = struct {
	mu    sync.Mutex
	items map[string]*verifierProcess
}{
	items: map[string]*verifierProcess{},
}

func startVerificationWindowFlow(settings config.Settings, pending *pendingVerification) error {
	if pending == nil {
		return fmt.Errorf("verification request not found")
	}
	if verificationTargetForFeedURL(pending.FeedURL) != "cloudflare" {
		return fmt.Errorf("unsupported verification target")
	}
	if !isWindowsRuntime() {
		return fmt.Errorf("manual Cloudflare feed verification is only supported on Windows")
	}

	binaryPath, err := verificationBinaryPath(settings)
	if err != nil {
		return err
	}
	callbackURL := verificationCallbackURL(settings)
	version := appruntime.PackageVersion(settings.RootDir)
	commandArgs := []string{
		"--verification-id", pending.ID,
		"--job-id", pending.JobID,
		"--feed-url", pending.FeedURL,
		"--callback-url", callbackURL,
		"--logs-dir", settings.LogsDir,
		"--app-version", version,
	}
	cmd := exec.Command(
		binaryPath,
		commandArgs...,
	)
	hideVerificationLauncherWindow(cmd)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open verification browser: %w", err)
	}
	process := &verifierProcess{
		VerificationID: pending.ID,
		JobID:          pending.JobID,
		FeedURL:        pending.FeedURL,
		Journal:        pending.Journal,
		BinaryPath:     binaryPath,
		Command:        append([]string{binaryPath}, commandArgs...),
		PID:            verifierPID(cmd),
		StartedAt:      time.Now(),
	}
	storeVerifierProcess(process)
	logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_process_started", "pipeline.feeds.verification_required", "Started the verifier window process.", "", map[string]any{
		"verification_id":       pending.ID,
		"verification_feed_url": pending.FeedURL,
		"verification_journal":  pending.Journal,
		"verification_binary":   binaryPath,
		"verification_pid":      process.PID,
		"verification_command":  strings.Join(process.Command, " "),
	})

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		exitCode := verifierExitCode(cmd, err)
		markVerifierProcessExited(pending.ID, exitCode, err)
		logVerificationProcessExit(settings, pending, process, exitCode, err)
		detail := strings.TrimSpace(stderr.String())
		if detail == "" && err != nil {
			detail = err.Error()
		}
		if detail == "" {
			detail = "verification window exited immediately"
		}
		return fmt.Errorf("open verification browser: %s", detail)
	case <-time.After(900 * time.Millisecond):
		go func() {
			err := <-done
			exitCode := verifierExitCode(cmd, err)
			markVerifierProcessExited(pending.ID, exitCode, err)
			logVerificationProcessExit(settings, pending, process, exitCode, err)
		}()
		return nil
	}
}

func verificationBinaryPath(settings config.Settings) (string, error) {
	if settings.Mode == appruntime.ModeRelease {
		binaryPath := filepath.Join(settings.AppDir, verificationBinaryName)
		if _, err := os.Stat(binaryPath); err == nil {
			return binaryPath, nil
		}
		return "", fmt.Errorf("verification helper not found: %s", binaryPath)
	}
	return ensureSourceBinaryFunc(settings.RootDir, "./cmd/feedmedaily-verifier", verificationBinaryName)
}

func verificationCallbackURL(settings config.Settings) string {
	host := strings.TrimSpace(settings.ServerHost)
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	if parsedIP := net.ParseIP(host); parsedIP != nil && parsedIP.IsUnspecified() {
		host = "127.0.0.1"
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, fmt.Sprintf("%d", settings.ServerPort)),
		Path:   "/api/feeds/verification/callback",
	}).String()
}

func verifierPID(cmd *exec.Cmd) int {
	if cmd == nil || cmd.Process == nil {
		return 0
	}
	return cmd.Process.Pid
}

func verifierExitCode(cmd *exec.Cmd, err error) int {
	if cmd != nil && cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return 0
	}
	return -1
}

func storeVerifierProcess(process *verifierProcess) {
	if process == nil {
		return
	}
	verifierProcesses.mu.Lock()
	defer verifierProcesses.mu.Unlock()
	clone := *process
	clone.Command = append([]string(nil), process.Command...)
	verifierProcesses.items[process.VerificationID] = &clone
}

func markVerifierProcessExited(verificationID string, exitCode int, err error) {
	verifierProcesses.mu.Lock()
	defer verifierProcesses.mu.Unlock()
	process, ok := verifierProcesses.items[verificationID]
	if !ok {
		return
	}
	process.Exited = true
	process.ExitCode = exitCode
	if err != nil {
		process.ExitError = err.Error()
	} else {
		process.ExitError = ""
	}
}

func snapshotVerifierProcess(verificationID string) (verifierProcess, bool) {
	verifierProcesses.mu.Lock()
	defer verifierProcesses.mu.Unlock()
	process, ok := verifierProcesses.items[verificationID]
	if !ok {
		return verifierProcess{}, false
	}
	clone := *process
	clone.Command = append([]string(nil), process.Command...)
	return clone, true
}

func logVerificationProcessExit(settings config.Settings, pending *pendingVerification, process *verifierProcess, exitCode int, err error) {
	callbackReceived, delivered := verificationDeliveryState(pending.ID)
	data := map[string]any{
		"verification_id":        pending.ID,
		"verification_feed_url":  pending.FeedURL,
		"verification_journal":   pending.Journal,
		"verification_pid":       process.PID,
		"verification_exit_code": exitCode,
		"callback_received":      callbackReceived,
		"delivered":              delivered,
		"elapsed_ms":             time.Since(process.StartedAt).Milliseconds(),
	}
	if err != nil {
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_process_exited", "pipeline.feeds.verification_required", "", err.Error(), data)
		return
	}
	logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_process_exited", "pipeline.feeds.verification_required", "Verifier window process exited.", "", data)
}
