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
	FeedURL        string
	CallbackURL    string
	UserDataDir    string
}

func parseArgs(args []string) (verifierConfig, error) {
	fs := flag.NewFlagSet("feedmedaily-verifier", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg verifierConfig
	fs.StringVar(&cfg.VerificationID, "verification-id", "", "Pending verification request ID.")
	fs.StringVar(&cfg.FeedURL, "feed-url", "", "Protected feed URL that needs manual verification.")
	fs.StringVar(&cfg.CallbackURL, "callback-url", "", "Local callback endpoint that receives RSS XML.")
	if err := fs.Parse(args); err != nil {
		return verifierConfig{}, err
	}

	cfg.VerificationID = strings.TrimSpace(cfg.VerificationID)
	cfg.FeedURL = strings.TrimSpace(cfg.FeedURL)
	cfg.CallbackURL = strings.TrimSpace(cfg.CallbackURL)
	if cfg.VerificationID == "" {
		return verifierConfig{}, fmt.Errorf("--verification-id is required")
	}
	if cfg.FeedURL == "" {
		return verifierConfig{}, fmt.Errorf("--feed-url is required")
	}
	if cfg.CallbackURL == "" {
		return verifierConfig{}, fmt.Errorf("--callback-url is required")
	}

	userDataDir, err := os.MkdirTemp("", "feedmedaily-verifier-*")
	if err != nil {
		return verifierConfig{}, fmt.Errorf("create verifier browser session dir: %w", err)
	}
	cfg.UserDataDir = filepath.Clean(userDataDir)
	return cfg, nil
}
