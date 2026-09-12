package profile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/logging"
	"io"
	"net/http"
	"strings"
	"time"
)

// profileThinkingMaxTokensFloor is the headroom used by the small connection-test
// request; production profile calls use profileThinkingWideMaxTokensFloor.
const profileThinkingMaxTokensFloor = 4096

// profileThinkingWideMaxTokensFloor is the headroom for thinking-enabled
// profile calls on providers whose reasoning effort is pinned low (measured:
// low-effort reasoning plus the proposal JSON fits comfortably).
const profileThinkingWideMaxTokensFloor = 8192

// profileThinkingDeepMaxTokensFloor is the headroom for providers whose
// thinking runs at default depth or on heavy tasks: MiMo has no effort level,
// and even DeepSeek's low effort measured over 8192 reasoning tokens on the
// proposal task, so both need room for reasoning plus the JSON answer.
const profileThinkingDeepMaxTokensFloor = 16384

// applyProfileProviderControls adapts the OpenAI-compatible profile payload to
// each provider's thinking contract while keeping the JSON-mode request shape.
// thinkingOverride is used by the thinking-disabled retries.
func applyProfileProviderControls(settings config.Settings, payload map[string]any, thinkingOverride string) {
	thinking := strings.TrimSpace(thinkingOverride)
	if thinking == "" {
		thinking = strings.TrimSpace(settings.ProfileThinking)
	}
	enabled := strings.EqualFold(thinking, "enabled")
	provider := strings.ToLower(strings.TrimSpace(settings.ProfileProvider))
	if provider == "" {
		provider = profileProviderFromModel(settings.ProfileModel, settings.ProfileBaseURL)
	}
	switch provider {
	case "qwen":
		// Qwen uses reasoning_effort instead of the thinking object; "none"
		// disables thinking, "low" is the lowest reasoning level. Its reasoning
		// shares the completion budget, so thinking-enabled requests get extra
		// headroom before the JSON payload is generated.
		delete(payload, "thinking")
		delete(payload, "enable_thinking")
		if thinking != "" {
			if enabled {
				payload["reasoning_effort"] = "low"
			} else {
				payload["reasoning_effort"] = "none"
			}
		}
		if enabled {
			raiseProfileMaxTokens(payload, "max_tokens", profileThinkingWideMaxTokensFloor)
		}
	case "mimo":
		// MiMo bounds thinking and the answer with max_completion_tokens and has
		// no effort level, so the budget is the only thinking-depth knob.
		if maxTokens, ok := payload["max_tokens"]; ok {
			payload["max_completion_tokens"] = maxTokens
			delete(payload, "max_tokens")
		}
		if thinking != "" && enabled {
			raiseProfileMaxTokens(payload, "max_completion_tokens", profileThinkingDeepMaxTokensFloor)
		}
		if thinking != "" {
			payload["thinking"] = map[string]string{"type": strings.ToLower(thinking)}
		}
	case "deepseek":
		// DeepSeek exposes low/high/max effort levels; the profile role pins the
		// same lowest level the classifier uses. Even at low effort the proposal
		// task measures over 8192 reasoning tokens, so the budget gives the
		// thinking-enabled call room to finish instead of falling back.
		if thinking != "" {
			payload["thinking"] = map[string]string{"type": strings.ToLower(thinking)}
		}
		if enabled {
			payload["reasoning_effort"] = "low"
			raiseProfileMaxTokens(payload, "max_tokens", profileThinkingDeepMaxTokensFloor)
		}
	case "zhipu":
		// GLM-5.3 always thinks: it accepts the enabled toggle as a no-op but
		// rejects an explicit disabled toggle, and its default reasoning effort
		// overruns the completion budget before any JSON is written. Profile
		// requests therefore pin reasoning_effort=low — the same level the
		// classifier uses for GLM — and keep wide output headroom on both the
		// enabled path and the cannot-actually-disable fallback.
		if enabled {
			payload["thinking"] = map[string]string{"type": "enabled"}
		} else {
			delete(payload, "thinking")
		}
		payload["reasoning_effort"] = "low"
		raiseProfileMaxTokens(payload, "max_tokens", profileThinkingWideMaxTokensFloor)
	default:
		// DeepSeek and unknown OpenAI-compatible providers share the top-level
		// thinking object shape.
		if thinking != "" {
			payload["thinking"] = map[string]string{"type": strings.ToLower(thinking)}
		}
		if enabled {
			raiseProfileMaxTokens(payload, "max_tokens", profileThinkingWideMaxTokensFloor)
		}
	}
}

func raiseProfileMaxTokens(payload map[string]any, key string, floor int) {
	if current, ok := payload[key].(int); ok && current < floor {
		payload[key] = floor
	}
}

// profileProviderFromModel infers the provider from the legacy flat settings
// when no managed catalog entry supplied one.
func profileProviderFromModel(model string, baseURL string) string {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	normalizedBaseURL := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(normalizedModel, "qwen") || strings.Contains(normalizedBaseURL, "dashscope.aliyuncs.com"):
		return "qwen"
	case strings.Contains(normalizedModel, "mimo") || strings.Contains(normalizedBaseURL, "xiaomimimo.com"):
		return "mimo"
	case strings.Contains(normalizedModel, "glm") || strings.Contains(normalizedBaseURL, "bigmodel.cn"):
		return "zhipu"
	case strings.Contains(normalizedModel, "deepseek") || strings.Contains(normalizedBaseURL, "api.deepseek.com"):
		return "deepseek"
	default:
		return ""
	}
}

func requestProfileModelJSONBody(settings config.Settings, endpoint string, body []byte, usage *llmusage.Collector, operation string) (string, error) {
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build profile generation request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+settings.ProfileAPIKey)
	request.Header.Set("Content-Type", "application/json")
	// Thinking-enabled providers routinely stream for minutes before the final
	// body completes; the historic 60s budget forced those calls into the
	// thinking-disabled fallback and silently turned the setting into a no-op.
	timeout := 60 * time.Second
	if strings.EqualFold(strings.TrimSpace(settings.ProfileThinking), "enabled") {
		timeout = 300 * time.Second
	}
	started := time.Now()
	response, err := (&http.Client{Timeout: timeout}).Do(request)
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
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens          int64  `json:"prompt_tokens"`
			PromptCacheHitTokens  *int64 `json:"prompt_cache_hit_tokens"`
			PromptCacheMissTokens *int64 `json:"prompt_cache_miss_tokens"`
			CompletionTokens      int64  `json:"completion_tokens"`
			PromptTokensDetails   *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &payloadResponse); err != nil {
		return "", fmt.Errorf("parse profile generation response: %w", err)
	}
	finishReason := ""
	if len(payloadResponse.Choices) > 0 {
		finishReason = strings.TrimSpace(payloadResponse.Choices[0].FinishReason)
	}
	reasoningTokens := int64(0)
	if payloadResponse.Usage != nil && payloadResponse.Usage.CompletionDetails != nil {
		reasoningTokens = payloadResponse.Usage.CompletionDetails.ReasoningTokens
	}
	// finish_reason=length means the provider cut the response at the token
	// budget; logging it keeps thinking-budget truncation diagnosable after the
	// fact instead of surfacing only as an opaque JSON parse failure.
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "profile",
		Action:    "response",
		Message:   fmt.Sprintf("LLM Response: POST %s finish_reason=%q", endpoint, finishReason),
		Data: map[string]any{
			"model":            settings.ProfileModel,
			"finish_reason":    finishReason,
			"operation":        operation,
			"reasoning_tokens": reasoningTokens,
		},
	})
		if payloadResponse.Usage != nil && usage != nil {
			responseUsage := llmusage.ResponseUsage{
				PromptTokens:     payloadResponse.Usage.PromptTokens,
				CompletionTokens: payloadResponse.Usage.CompletionTokens,
			}
			if payloadResponse.Usage.PromptCacheHitTokens != nil && payloadResponse.Usage.PromptCacheMissTokens != nil {
				// DeepSeek-style flat cache breakdown.
				responseUsage.PromptCacheHitTokens = *payloadResponse.Usage.PromptCacheHitTokens
				responseUsage.PromptCacheMissTokens = *payloadResponse.Usage.PromptCacheMissTokens
				responseUsage.CacheBreakdownPresent = true
			} else if details := payloadResponse.Usage.PromptTokensDetails; details != nil && details.CachedTokens > 0 {
				// OpenAI-style nested cached-token details (Zhipu, DashScope).
				cached := details.CachedTokens
				if cached > responseUsage.PromptTokens {
					cached = responseUsage.PromptTokens
				}
				responseUsage.PromptCacheHitTokens = cached
				responseUsage.PromptCacheMissTokens = responseUsage.PromptTokens - cached
				responseUsage.CacheBreakdownPresent = true
			}
			usage.Record(llmusage.Event{Role: "profile", Operation: operation, BaseURL: settings.ProfileBaseURL, Model: settings.ProfileModel, OccurredAt: time.Now().UTC(), Usage: responseUsage})
		}
	if len(payloadResponse.Choices) == 0 {
		return "", fmt.Errorf("profile model response did not contain any choices")
	}
	return payloadResponse.Choices[0].Message.Content, nil
}

func requestProfileModelJSON(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int) (string, error) {
	return requestProfileModelJSONWithUsage(settings, systemPrompt, userPrompt, maxTokens, nil, "profile_request")
}

func requestProfileModelJSONWithUsage(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int, usage *llmusage.Collector, operation string) (string, error) {
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
	applyProfileProviderControls(settings, payload, "")
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode profile generation request: %w", err)
	}
	content, err := requestProfileModelJSONBody(settings, endpoint, body, usage, operation)
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
	fallbackPayload := map[string]any{
		"model": settings.ProfileModel,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature":     0,
		"max_tokens":      maxTokens,
		"response_format": map[string]string{"type": "json_object"},
	}
	applyProfileProviderControls(settings, fallbackPayload, "disabled")
	fallbackBody, marshalErr := json.Marshal(fallbackPayload)
	if marshalErr != nil {
		return "", fmt.Errorf("encode fallback profile generation request: %w", marshalErr)
	}
	content, fallbackErr := requestProfileModelJSONBody(settings, endpoint, fallbackBody, usage, operation+"_thinking_fallback")
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

func callProfileModelJSON(settings config.Settings, systemPrompt string, userPrompt string, maxTokens int, usage *llmusage.Collector, operation string) (string, error) {
	if usage == nil {
		return requestProfileModelJSONFunc(settings, systemPrompt, userPrompt, maxTokens)
	}
	return requestProfileModelJSONWithUsage(settings, systemPrompt, userPrompt, maxTokens, usage, operation)
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

func coerceProfileDocument(settings config.Settings, content string, usage *llmusage.Collector) (profileDocument, error) {
	payload, err := ValidateModelProfileText(content)
	if err == nil {
		return parseDocumentMap(payload)
	}
	repaired, repairErr := repairProfileJSON(settings, content, usage)
	if repairErr != nil {
		return profileDocument{}, fmt.Errorf("model returned invalid classification profile JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseDocumentMap(repaired)
}

func coerceProposalDelta(settings config.Settings, content string, usage *llmusage.Collector) (proposalDelta, error) {
	payload, err := ValidateModelProposalDeltaText(content, "Generated profile proposal.")
	if err == nil {
		return parseProposalDeltaMap(payload)
	}
	repaired, repairErr := repairProposalDeltaJSON(settings, content, usage)
	if repairErr != nil {
		return proposalDelta{}, fmt.Errorf("model returned invalid profile delta JSON. First parse failed: %v Repair attempt failed: %v", err, repairErr)
	}
	return parseProposalDeltaMap(repaired)
}

func repairProfileJSON(settings config.Settings, malformedContent string, usage *llmusage.Collector) (map[string]any, error) {
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
	content, err := callProfileModelJSON(
		settings,
		"You repair malformed JSON classification profiles.",
		prompt,
		4200,
		usage,
		"profile_json_repair",
	)
	if err != nil {
		return nil, err
	}
	return ValidateModelProfileText(content)
}

func repairProposalDeltaJSON(settings config.Settings, malformedContent string, usage *llmusage.Collector) (map[string]any, error) {
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
	content, err := callProfileModelJSON(
		settings,
		"You repair malformed JSON profile update deltas.",
		prompt,
		2200,
		usage,
		"profile_delta_json_repair",
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
