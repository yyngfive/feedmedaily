package profile

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
)

// TestConnection verifies a profile model with a minimal JSON-mode request.
// It runs through the same provider-aware request builder as production, so a
// passing test means the thinking controls and structured output work for that
// provider.
func TestConnection(settings config.Settings, usage *llmusage.Collector) error {
	maxTokens := 200
	if strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "enabled") {
		// Thinking consumes the completion budget on several providers; keep the
		// same headroom rule as production profile calls.
		maxTokens = profileThinkingMaxTokensFloor
	}
	content, err := callProfileModelJSON(
		settings,
		"You respond with JSON.",
		`Return exactly {"ok": true}.`,
		maxTokens,
		usage,
		"profile_connection_test",
	)
	if err != nil {
		return err
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return fmt.Errorf("parse profile connection test response: %w", err)
	}
	if ok, valid := decoded["ok"].(bool); !valid || !ok {
		return fmt.Errorf("profile connection test response did not contain ok")
	}
	return nil
}
