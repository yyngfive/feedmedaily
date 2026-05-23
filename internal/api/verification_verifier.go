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
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	appruntime "github.com/yyngfive/scirssagent/internal/runtime"
)

const verificationBinaryName = "FeedMeDailyVerifier.exe"

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
	cmd := exec.Command(
		binaryPath,
		"--verification-id", pending.ID,
		"--feed-url", pending.FeedURL,
		"--callback-url", callbackURL,
	)
	hideVerificationLauncherWindow(cmd)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open verification browser: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		detail := strings.TrimSpace(stderr.String())
		if detail == "" && err != nil {
			detail = err.Error()
		}
		if detail == "" {
			detail = "verification window exited immediately"
		}
		return fmt.Errorf("open verification browser: %s", detail)
	case <-time.After(900 * time.Millisecond):
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
