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

const protectedFeedVerificationBinaryName = "FeedMeDailyProtectedVerifier.exe"

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

var ensureProtectedFeedVerificationBinaryFunc = ensureProtectedFeedVerificationBinary

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
	pending.VerifierKind = verificationVerifierKindNativeWebView
	binaryPath, err := protectedFeedVerificationBinaryPath(settings)
	if err != nil {
		return err
	}
	callbackURL := verificationCallbackURL(settings)
	if strings.TrimSpace(pending.CallbackURL) != "" {
		callbackURL = pending.CallbackURL
	}
	version := appruntime.PackageVersion(settings.RootDir)
	commandArgs := []string{
		"--verification-id", pending.ID,
		"--job-id", pending.JobID,
		"--verification-host", pending.Host,
		"--callback-url", callbackURL,
		"--logs-dir", settings.LogsDir,
		"--app-version", version,
	}
	userDataDir, err := verificationUserDataDir(settings, pending.FeedURL)
	if err != nil {
		return err
	}
	commandArgs = append(commandArgs, "--user-data-dir", userDataDir)
	for _, feedURL := range pending.FeedURLs {
		commandArgs = append(commandArgs, "--feed-url", feedURL)
	}
	return startVerifierProcess(settings, pending, binaryPath, commandArgs, userDataDir, "Started the protected-feed verifier window process.")
}

func startVerifierProcess(settings config.Settings, pending *pendingVerification, binaryPath string, commandArgs []string, userDataDir string, message string) error {
	cmd := newVerifierCommand(binaryPath, commandArgs)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if started, existing := beginVerifierProcessStart(pending.ID); !started {
		logData := map[string]any{
			"verification_id":       pending.ID,
			"verification_feed_url": pending.FeedURL,
			"verification_journal":  pending.Journal,
			"verification_host":     pending.Host,
		}
		if existing != nil {
			logData["verification_pid"] = existing.PID
			logData["verification_binary"] = existing.BinaryPath
		}
		logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "warning", "verification_process_duplicate_blocked", "pipeline.feeds.verification_required", "Skipped launching a duplicate verifier window for the same verification request.", "", logData)
		return nil
	}
	if err := cmd.Start(); err != nil {
		cancelVerifierProcessStart(pending.ID)
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
	finishVerifierProcessStart(process)
	logJobEvent(settings.LogsDir, &jobInfo{ID: pending.JobID}, "info", "verification_process_started", "pipeline.feeds.verification_required", message, "", map[string]any{
		"verification_id":        pending.ID,
		"verification_feed_url":  pending.FeedURL,
		"verification_journal":   pending.Journal,
		"verification_binary":    binaryPath,
		"verification_pid":       process.PID,
		"verification_profile":   userDataDir,
		"verification_host":      pending.Host,
		"verification_feed_urls": pending.FeedURLs,
		"verification_command":   strings.Join(process.Command, " "),
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

func newVerifierCommand(binaryPath string, commandArgs []string) *exec.Cmd {
	return exec.Command(binaryPath, commandArgs...)
}

func protectedFeedVerificationBinaryPath(settings config.Settings) (string, error) {
	if settings.Mode == appruntime.ModeRelease {
		binaryPath := filepath.Join(settings.AppDir, "FeedMeDailyProtectedVerifier", protectedFeedVerificationBinaryName)
		if _, err := os.Stat(binaryPath); err == nil {
			return binaryPath, nil
		}
		return "", fmt.Errorf("protected feed verification helper not found: %s", binaryPath)
	}
	return ensureProtectedFeedVerificationBinaryFunc(settings.RootDir)
}

func verificationCallbackURL(settings config.Settings) string {
	host := verificationCallbackHost(settings.ServerHost)
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, fmt.Sprintf("%d", settings.ServerPort)),
		Path:   "/api/feeds/verification/callback",
	}).String()
}

func verificationCallbackHost(host string) string {
	clean := strings.TrimSpace(host)
	switch clean {
	case "", "0.0.0.0", "::":
		clean = "127.0.0.1"
	}
	if parsedIP := net.ParseIP(clean); parsedIP != nil && parsedIP.IsUnspecified() {
		return "127.0.0.1"
	}
	return clean
}

func verificationUserDataDir(settings config.Settings, feedURL string) (string, error) {
	host := verificationProfileHost(feedURL)
	userDataDir := filepath.Join(settings.DataDir, "verification-profiles", host)
	if err := os.MkdirAll(userDataDir, 0o755); err != nil {
		return "", fmt.Errorf("create verification browser profile dir: %w", err)
	}
	return userDataDir, nil
}

func ensureProtectedFeedVerificationBinary(root string) (string, error) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go command not found; install Go to build the protected-feed verifier helper")
	}
	outputDir := filepath.Join(root, ".tmp", "runtime-bin", "FeedMeDailyProtectedVerifier")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("create protected-feed verifier output dir: %w", err)
	}
	binaryPath := filepath.Join(outputDir, protectedFeedVerificationBinaryName)
	version := appruntime.PackageVersion(root)
	cmd := exec.Command(goPath, protectedFeedVerificationBuildArgs(binaryPath, version)...)
	cmd.Dir = root
	hideVerificationLauncherWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("build protected-feed verifier helper: %s", detail)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return "", fmt.Errorf("protected-feed verifier helper build did not produce %s", binaryPath)
	}
	return binaryPath, nil
}

func protectedFeedVerificationBuildArgs(binaryPath string, version string) []string {
	return []string{
		"build",
		"-tags", "production",
		"-ldflags", fmt.Sprintf("-H=windowsgui -X github.com/yyngfive/scirssagent/internal/runtime.buildVersion=%s", version),
		"-o", binaryPath,
		".\\cmd\\feedmedaily-protected-verifier",
	}
}

func verificationProfileHost(feedURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(feedURL))
	if err != nil {
		return "default"
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "default"
	}
	var normalized strings.Builder
	for _, char := range strings.ToLower(host) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '.' || char == '-' {
			normalized.WriteRune(char)
			continue
		}
		normalized.WriteRune('_')
	}
	if normalized.Len() == 0 {
		return "default"
	}
	return normalized.String()
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

func beginVerifierProcessStart(verificationID string) (bool, *verifierProcess) {
	verifierProcesses.mu.Lock()
	defer verifierProcesses.mu.Unlock()
	process, ok := verifierProcesses.items[verificationID]
	if ok && !process.Exited {
		clone := *process
		clone.Command = append([]string(nil), process.Command...)
		return false, &clone
	}
	verifierProcesses.items[verificationID] = &verifierProcess{
		VerificationID: verificationID,
		StartedAt:      time.Now(),
	}
	return true, nil
}

func cancelVerifierProcessStart(verificationID string) {
	verifierProcesses.mu.Lock()
	defer verifierProcesses.mu.Unlock()
	delete(verifierProcesses.items, verificationID)
}

func finishVerifierProcessStart(process *verifierProcess) {
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

func terminateVerifierProcess(settings config.Settings, verificationID string) {
	process, ok := snapshotVerifierProcess(verificationID)
	if !ok || process.Exited || process.PID <= 0 {
		return
	}
	var killErr error
	osProcess, err := os.FindProcess(process.PID)
	if err == nil {
		killErr = osProcess.Kill()
	} else {
		killErr = err
	}
	markVerifierProcessExited(verificationID, -1, fmt.Errorf("verification process termination requested"))
	logData := map[string]any{
		"verification_id":       process.VerificationID,
		"verification_feed_url": process.FeedURL,
		"verification_journal":  process.Journal,
		"verification_pid":      process.PID,
	}
	if killErr != nil {
		logData["termination_error"] = killErr.Error()
	}
	logJobEvent(settings.LogsDir, &jobInfo{ID: process.JobID}, "warning", "verification_process_terminated", "pipeline.feeds.verification_required", "Verifier process was still running, so FeedMeDaily cleared it before continuing.", "", logData)
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
