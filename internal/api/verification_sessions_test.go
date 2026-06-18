package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
)

func TestVerificationHostSessionPersistsVerifiedState(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{DataDir: filepath.Join(root, "data")}
	nowFunc = func() time.Time {
		return time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	}
	defer func() { nowFunc = time.Now }()

	session, err := markVerificationHostSessionVerified(settings, "pubs.acs.org", verificationVerifierKindNativeWebView)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != verificationSessionStateVerified {
		t.Fatalf("state = %q", session.State)
	}

	loaded, err := verificationHostSessionForHost(settings, "pubs.acs.org")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != verificationSessionStateVerified || loaded.VerifierKind != verificationVerifierKindNativeWebView {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestVerificationHostSessionMarksNeedsReverify(t *testing.T) {
	root := t.TempDir()
	settings := config.Settings{DataDir: filepath.Join(root, "data")}

	if _, err := markVerificationHostSessionVerified(settings, "pubs.acs.org", verificationVerifierKindNativeWebView); err != nil {
		t.Fatal(err)
	}
	session, err := markVerificationHostSessionNeedsReverify(settings, "pubs.acs.org", verificationVerifierKindNativeWebView, "challenge")
	if err != nil {
		t.Fatal(err)
	}
	if session.State != verificationSessionStateNeedsReverify {
		t.Fatalf("state = %q", session.State)
	}
	if session.LastFailureReason != "challenge" {
		t.Fatalf("failure reason = %q", session.LastFailureReason)
	}
	if session.LastSuccessAt == "" {
		t.Fatal("expected prior success timestamp to remain recorded")
	}
}
