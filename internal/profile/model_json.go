package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/logging"
	"io"
	"net/http"
	"strings"
	"time"
)

func requestProfileModelJSONBody(settings config.Settings, endpoint string, body []byte) (string, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build profile generation request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+settings.ProfileAPIKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "profile",
			Action:    "request_failed",
			Message:   fmt.Sprintf("LLM Request: POST %s failed", endpoint),
			Error:     err.Error(),
			Data: map[string]any{
				"model":       settings.ProfileModel,
				"duration_ms": time.Since(started).Milliseconds(),
			},
		})
		return "", fmt.Errorf("request profile generation: %w", err)
	}
	defer response.Body.Close()
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "request",
		Message:   fmt.Sprintf("LLM Request: POST %s %q", endpoint, response.Proto+" "+response.Status),
		Data: map[string]any{
			"model":       settings.ProfileModel,
			"status_code": response.StatusCode,
			"duration_ms": time.Since(started).Milliseconds(),
		},
	})
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read profile generation response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		return "", fmt.Errorf("profile model request failed: %s", message)
	}
	var payloadResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(responseBody, &payloadResponse); err != nil {
		return "", fmt.Errorf("parse profile generation response: %w", err)
	}
	if len(payloadResponse.Choices) == 0 {
		return "", fmt.Errorf("profile model response did not contain any choices")
	}
	return payloadResponse.Choices[0].Message.Content, nil
}

func requestProfileModelJSON(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int) (string, error) {
	// 通过 OpenAI-compatible chat completions 调用 profile model 并返回 message content。
	if strings.TrimSpace(settings.ProfileAPIKey) == "" {
		return "", fmt.Errorf("SCIRSS_PROFILE_API_KEY is required for profile generation and prompt revision")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(settings.ProfileBaseURL), "/") + "/chat/completions"
	payload := map[string]any{
		"model": settings.ProfileModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	if thinking := strings.TrimSpace(settings.ProfileThinking); thinking != "" {
		payload["thinking"] = map[string]string{"type": thinking}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode profile generation request: %w", err)
	}
	content, err := requestProfileModelJSONBody(settings, endpoint, body)
	if err == nil || strings.TrimSpace(settings.ProfileThinking) == "" || strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "disabled") || !shouldRetryProfileWithoutThinking(err) {
		return content, err
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "profile",
		Action:    "thinking_fallback_started",
		Message:   "Retrying profile request with thinking disabled.",
		Error:     err.Error(),
		Data:      map[string]any{"model": settings.ProfileModel},
	})
	payload["thinking"] = map[string]string{"type": "disabled"}
	fallbackBody, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return "", fmt.Errorf("encode fallback profile generation request: %w", marshalErr)
	}
	content, fallbackErr := requestProfileModelJSONBody(settings, endpoint, fallbackBody)
	if fallbackErr != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "profile",
			Action:    "thinking_fallback_failed",
			Message:   "Retry with thinking disabled failed.",
			Error:     fallbackErr.Error(),
			Data:      map[string]any{"model": settings.ProfileModel},
		})
		return "", fallbackErr
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "thinking_fallback_completed",
		Message:   "Retry with thinking disabled succeeded.",
		Data:      map[string]any{"model": settings.ProfileModel},
	})
	return content, nil
}

func shouldRetryProfileWithoutThinking(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "deadline exceeded") ||
		strings.Contains(message, "thinking") ||
		strings.Contains(message, "reasoning") ||
		strings.Contains(message, "reasoner") ||
		strings.Contains(message, "504") ||
		strings.Contains(message, "502")
}

func shouldRetryProfileParseWithThinkingDisabled(settings config.Settings, err error) bool {
	if strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "disabled") {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "invalid classification profile json") ||
		strings.Contains(message, "could not find a complete json object") ||
		strings.Contains(message, "parse compact profile proposal") ||
		strings.Contains(message, "parse profile proposal validation") ||
		strings.Contains(message, "unexpected end of json input")
}

func coerceProfileDocument(settings config.Settings, content string) (profileDocument, error) {
	payload, err := ValidateModelProfileText(content)
	if err == nil {
		return parseDocumentMap(payload)
	}
	repaired, repairErr := repairProfileJSON(settings, content)
	if repairErr != nil {
		return profileDocument{}, fmt.Errorf("model returned invalid classification profile JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseDocumentMap(repaired)
}

func coerceProposalDelta(settings config.Settings, content string) (proposalDelta, error) {
	payload, err := ValidateModelProposalDeltaText(content, "Generated profile proposal.")
	if err == nil {
		return parseProposalDeltaMap(payload)
	}
	repaired, repairErr := repairProposalDeltaJSON(settings, content)
	if repairErr != nil {
		return proposalDelta{}, fmt.Errorf("model returned invalid profile delta JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseProposalDeltaMap(repaired)
}

func repairProfileJSON(settings config.Settings, malformedContent string) (map[string]any, error) {
	prompt := strings.TrimSpace(fmt.Sprintf(`
Repair the malformed scientific-literature classification profile below.

Requirements:
- Return valid JSON only.
- Return a complete profile object.
- Follow the required schema exactly.
- Keep the repaired content faithful to the original intent.
- If the draft was truncated, infer the smallest sensible completion.

Required JSON shape:
%s

Malformed draft:
%s
`, compactProfileContract(), malformedContent))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You repair malformed JSON classification profiles.",
		prompt,
		4200,
	)
	if err != nil {
		return nil, err
	}
	return ValidateModelProfileText(content)
}

func repairProposalDeltaJSON(settings config.Settings, malformedContent string) (map[string]any, error) {
	prompt := strings.TrimSpace(fmt.Sprintf(`
Repair the malformed profile-update delta below.

Requirements:
- Return valid JSON only.
- Return a complete delta object.
- Follow the required schema exactly.
- Keep the repaired content faithful to the original intent.
- If the draft was truncated, infer the smallest sensible completion.

Required JSON shape:
%s

Malformed draft:
%s
`, profileDeltaContract(), malformedContent))
	content, err := requestProfileModelJSONFunc(
		settings,
		"You repair malformed JSON profile update deltas.",
		prompt,
		2200,
	)
	if err != nil {
		return nil, err
	}
	return ValidateModelProposalDeltaText(content, "Generated profile proposal.")
}

func coerceCompactProposal(content string) (string, []ProposalChange, error) {
	data, err := extractJSONObjectBytes(content)
	if err != nil {
		return "", nil, err
	}
	var payload struct {
		Summary         string           `json:"summary"`
		ProposedProfile json.RawMessage  `json:"proposed_profile"`
		Changes         []ProposalChange `json:"changes"`
	}
	if err := decodeStrict(data, &payload); err != nil {
		return "", nil, fmt.Errorf("parse compact profile proposal: %w", err)
	}
	summary := normalizeText(payload.Summary)
	if summary == "" {
		return "", nil, fmt.Errorf("compact profile proposal summary cannot be blank")
	}
	changes, err := ValidateProposalChanges(payload.Changes)
	if err != nil {
		return "", nil, err
	}
	return summary, changes, nil
}
