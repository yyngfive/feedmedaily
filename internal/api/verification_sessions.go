package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
)

const (
	verificationSessionStateVerified      = "verified"
	verificationSessionStateNeedsReverify = "needs_reverify"
	verificationSessionStateUnknown       = ""
	verificationVerifierKindWails         = "wails_webview2"
	verificationVerifierKindACSNative     = "acs_native_webview2"
)

type verificationHostSession struct {
	Host              string `json:"host"`
	State             string `json:"state,omitempty"`
	LastVerifiedAt    string `json:"last_verified_at,omitempty"`
	LastSuccessAt     string `json:"last_success_at,omitempty"`
	VerifierKind      string `json:"verifier_kind,omitempty"`
	LastFailureReason string `json:"last_failure_reason,omitempty"`
}

func verificationSessionStorePath(settings config.Settings) string {
	return filepath.Join(settings.DataDir, "verification-sessions.json")
}

func loadVerificationHostSessions(settings config.Settings) (map[string]verificationHostSession, error) {
	path := verificationSessionStorePath(settings)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]verificationHostSession{}, nil
		}
		return nil, fmt.Errorf("read verification sessions: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]verificationHostSession{}, nil
	}
	var sessions map[string]verificationHostSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, fmt.Errorf("parse verification sessions: %w", err)
	}
	if sessions == nil {
		return map[string]verificationHostSession{}, nil
	}
	return sessions, nil
}

func saveVerificationHostSessions(settings config.Settings, sessions map[string]verificationHostSession) error {
	if err := os.MkdirAll(settings.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir for verification sessions: %w", err)
	}
	payload, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("encode verification sessions: %w", err)
	}
	path := verificationSessionStorePath(settings)
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o644); err != nil {
		return fmt.Errorf("write verification sessions temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace verification sessions: %w", err)
	}
	return nil
}

func verificationHostSessionForHost(settings config.Settings, host string) (verificationHostSession, error) {
	cleanHost := strings.TrimSpace(strings.ToLower(host))
	if cleanHost == "" {
		return verificationHostSession{}, nil
	}
	sessions, err := loadVerificationHostSessions(settings)
	if err != nil {
		return verificationHostSession{}, err
	}
	return sessions[cleanHost], nil
}

func updateVerificationHostSession(settings config.Settings, host string, apply func(*verificationHostSession)) (verificationHostSession, error) {
	cleanHost := strings.TrimSpace(strings.ToLower(host))
	if cleanHost == "" {
		return verificationHostSession{}, nil
	}
	sessions, err := loadVerificationHostSessions(settings)
	if err != nil {
		return verificationHostSession{}, err
	}
	session := sessions[cleanHost]
	session.Host = cleanHost
	apply(&session)
	sessions[cleanHost] = session
	if err := saveVerificationHostSessions(settings, sessions); err != nil {
		return verificationHostSession{}, err
	}
	return session, nil
}

func markVerificationHostSessionVerified(settings config.Settings, host string, verifierKind string) (verificationHostSession, error) {
	now := nowFunc().UTC().Format(time.RFC3339Nano)
	return updateVerificationHostSession(settings, host, func(session *verificationHostSession) {
		session.State = verificationSessionStateVerified
		session.LastVerifiedAt = now
		session.LastSuccessAt = now
		session.VerifierKind = strings.TrimSpace(verifierKind)
		session.LastFailureReason = ""
	})
}

func markVerificationHostSessionNeedsReverify(settings config.Settings, host string, verifierKind string, failureReason string) (verificationHostSession, error) {
	return updateVerificationHostSession(settings, host, func(session *verificationHostSession) {
		session.State = verificationSessionStateNeedsReverify
		if strings.TrimSpace(verifierKind) != "" {
			session.VerifierKind = strings.TrimSpace(verifierKind)
		}
		session.LastFailureReason = strings.TrimSpace(failureReason)
	})
}
