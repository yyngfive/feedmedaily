//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type verifierConfig struct {
	VerificationID string
	JobID          string
	FeedURL        string
	CallbackURL    string
	LogsDir        string
	AppVersion     string
	UserDataDir    string
	CleanupProfile bool
}

func parseArgs(args []string) (verifierConfig, error) {
	fs := flag.NewFlagSet("feedmedaily-verifier", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg verifierConfig
	fs.StringVar(&cfg.VerificationID, "verification-id", "", "Pending verification request ID.")
	fs.StringVar(&cfg.JobID, "job-id", "", "Owning job ID for log correlation.")
	fs.StringVar(&cfg.FeedURL, "feed-url", "", "Protected feed URL that needs manual verification.")
	fs.StringVar(&cfg.CallbackURL, "callback-url", "", "Local callback endpoint that receives RSS XML.")
	fs.StringVar(&cfg.LogsDir, "logs-dir", "", "Base logs directory for verifier-local diagnostics.")
	fs.StringVar(&cfg.AppVersion, "app-version", "", "FeedMeDaily version string for diagnostics.")
	fs.StringVar(&cfg.UserDataDir, "user-data-dir", "", "WebView2 user-data directory for the verifier browser profile.")
	if err := fs.Parse(args); err != nil {
		return verifierConfig{}, err
	}

	cfg.VerificationID = strings.TrimSpace(cfg.VerificationID)
	cfg.JobID = strings.TrimSpace(cfg.JobID)
	cfg.FeedURL = strings.TrimSpace(cfg.FeedURL)
	cfg.CallbackURL = strings.TrimSpace(cfg.CallbackURL)
	cfg.LogsDir = strings.TrimSpace(cfg.LogsDir)
	cfg.AppVersion = strings.TrimSpace(cfg.AppVersion)
	cfg.UserDataDir = strings.TrimSpace(cfg.UserDataDir)
	if cfg.VerificationID == "" {
		return verifierConfig{}, fmt.Errorf("--verification-id is required")
	}
	if cfg.FeedURL == "" {
		return verifierConfig{}, fmt.Errorf("--feed-url is required")
	}
	if cfg.CallbackURL == "" {
		return verifierConfig{}, fmt.Errorf("--callback-url is required")
	}
	if cfg.UserDataDir == "" {
		userDataDir, err := os.MkdirTemp("", "feedmedaily-verifier-*")
		if err != nil {
			return verifierConfig{}, fmt.Errorf("create verifier browser session dir: %w", err)
		}
		cfg.UserDataDir = filepath.Clean(userDataDir)
		cfg.CleanupProfile = true
		return cfg, nil
	}
	cfg.UserDataDir = filepath.Clean(cfg.UserDataDir)
	if err := os.MkdirAll(cfg.UserDataDir, 0o755); err != nil {
		return verifierConfig{}, fmt.Errorf("create verifier browser profile dir: %w", err)
	}
	return cfg, nil
}
