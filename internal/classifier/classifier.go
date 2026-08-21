package classifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

const baseClassificationInstructions = `You are a careful scientific literature classifier.

Classify each paper using only the user-supplied classification profile.
Do not invent new interests, relevance rules, or decision criteria outside the profile.
Do not broaden the user's scope beyond what the profile supports.
Judge relevance from the current scope and relevance rules only.
Do not let broad examples, topic labels, or surface object similarity expand direct relevance.
If two historical cues appear to conflict, follow the current profile rules as written.

Classify by priority and core contribution:
1. Apply unrelated exclusion rules first. If any unrelated rule fits the paper's core contribution, choose unrelated even if the paper mentions DNA, RNA, aptamer, probe, CRISPR, sequencing, polymerase, engineering, or other profile-adjacent terms.
2. If not excluded, choose direct only when the paper's core contribution satisfies the direct rules.
3. If not direct, choose indirect only when the work closely informs the direct-rule areas.
4. If uncertain between indirect and unrelated, choose unrelated unless the paper directly informs nucleic-acid chemistry, substrate design, or nucleic-acid-enzyme engineering under the supplied profile.

Labels are fixed:
- direct
- indirect
- unrelated

Return concise, evidence-based reasoning grounded in the title and abstract.`

type LLMConfig struct {
	APIKey   string
	Model    string
	BaseURL  string
	Thinking string
}

type promptPaper struct {
	ID       string
	PaperID  int64
	Title    string
	Journal  string
	Abstract string
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

var classifierHTTPClient = &http.Client{Timeout: 60 * time.Second}

const classifierMaxAttempts = 2

func ClassifyPapers(papers []store.Paper, profile map[string]any, cfg LLMConfig) ([]store.Classification, error) {
	// 复刻 Python classifier 的批量分类与缺失标题翻译补全逻辑。
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("SCIRSS_CLASSIFIER_API_KEY is required for classification.")
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    "batch_started",
		Message:   fmt.Sprintf("Classifying batch of %d paper(s)", len(papers)),
		Data: map[string]any{
			"model": cfg.Model,
			"items": len(papers),
		},
	})
	indexed := make([]promptPaper, 0, len(papers))
	for index, paper := range papers {
		indexed = append(indexed, promptPaper{
			ID:       fmt.Sprintf("%d", index+1),
			PaperID:  paper.ID,
			Title:    paper.Title,
			Journal:  firstNonEmpty(stringValue(paper.Journal), stringValue(paper.FeedTitle), "unknown"),
			Abstract: firstNonEmpty(stringValue(paper.Abstract), "No abstract available."),
		})
	}

	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a careful scientific literature classifier."},
			{"role": "user", "content": batchClassificationPrompt(indexed, profile)},
		},
		"temperature":     0,
		"max_tokens":      max(600, 220*len(papers)),
		"response_format": map[string]string{"type": "json_object"},
		"thinking":        map[string]string{"type": normalizedThinking(cfg.Thinking)},
	}

	var decoded map[string]any
	lastContent := ""
	var lastParseErr error
	for attempt := 1; attempt <= classifierMaxAttempts; attempt++ {
		content, err := requestJSONContentWithFallback(cfg, payload, "classification")
		if err != nil {
			return nil, err
		}
		lastContent = content
		if strings.TrimSpace(content) == "" {
			lastParseErr = fmt.Errorf("empty classifier response content")
			if attempt < classifierMaxAttempts {
				logClassifierRetry("classification_parse_retry", "Retrying after empty classifier response content.", lastParseErr, cfg.Model, "classification", attempt, len(content))
			}
			continue
		}
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			lastParseErr = fmt.Errorf("parse classifier response: %w", err)
			if attempt < classifierMaxAttempts {
				logClassifierRetry("classification_parse_retry", "Retrying after malformed classifier JSON content.", lastParseErr, cfg.Model, "classification", attempt, len(content))
				continue
			}
			return nil, lastParseErr
		}
		break
	}
	if decoded == nil {
		if lastParseErr != nil {
			return nil, lastParseErr
		}
		return nil, fmt.Errorf("batch model returned empty JSON content length=%d", len(lastContent))
	}
	rawItems, ok := decoded["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("batch JSON response missing items list")
	}

	byID := map[string]store.Classification{}
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemID := strings.TrimSpace(fmt.Sprintf("%v", item["id"]))
		if itemID == "" {
			continue
		}
		classification, err := decodeClassification(item, cfg.Model)
		if err != nil {
			return nil, err
		}
		byID[itemID] = classification
	}

	missingTitles := make([]promptPaper, 0)
	for _, item := range indexed {
		classification, ok := byID[item.ID]
		if ok && (classification.TranslatedTitleZH == nil || strings.TrimSpace(*classification.TranslatedTitleZH) == "") {
			missingTitles = append(missingTitles, item)
		}
	}
	if len(missingTitles) > 0 {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "info",
			Component: "classifier",
			Action:    "translation_fallback_started",
			Message:   fmt.Sprintf("Translating %d missing title(s)", len(missingTitles)),
			Data: map[string]any{
				"model": cfg.Model,
				"items": len(missingTitles),
			},
		})
		translations, err := TranslateTitlesBatch(missingTitles, cfg)
		if err != nil {
			_, _ = logging.WriteDefault(logging.Event{
				Level:     "warning",
				Component: "classifier",
				Action:    "translation_fallback_failed",
				Message:   "Title translation fallback failed; continuing without translated titles.",
				Error:     err.Error(),
				Data: map[string]any{
					"model": cfg.Model,
					"items": len(missingTitles),
				},
			})
			translations = map[string]string{}
		}
		for itemID, translated := range translations {
			classification := byID[itemID]
			if strings.TrimSpace(translated) == "" {
				continue
			}
			value := translated
			classification.TranslatedTitleZH = &value
			byID[itemID] = classification
		}
	}

	results := make([]store.Classification, 0, len(indexed))
	missingIDs := make([]string, 0)
	for _, item := range indexed {
		classification, ok := byID[item.ID]
		if !ok {
			missingIDs = append(missingIDs, item.ID)
			continue
		}
		results = append(results, classification)
	}
	if len(missingIDs) > 0 {
		return nil, fmt.Errorf("batch response missing papers: %s", strings.Join(missingIDs, ", "))
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    "batch_completed",
		Message:   fmt.Sprintf("Classified %d paper(s)", len(results)),
		Data: map[string]any{
			"model":          cfg.Model,
			"items":          len(results),
			"translated":     len(missingTitles),
			"missing_titles": len(missingTitles),
		},
	})
	return results, nil
}

func TranslateTitlesBatch(papers []promptPaper, cfg LLMConfig) (map[string]string, error) {
	// 只给缺中文标题的结果补一次批量翻译，不因部分缺项整体失败。
	payload := map[string]any{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You translate scientific paper titles into concise Simplified Chinese."},
			{"role": "user", "content": titleTranslationPrompt(papers)},
		},
		"temperature":     0,
		"max_tokens":      max(300, 120*len(papers)),
		"response_format": map[string]string{"type": "json_object"},
		"thinking":        map[string]string{"type": "disabled"},
	}
	content, err := requestJSONContentWithFallback(cfg, payload, "title_translation")
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return nil, fmt.Errorf("parse title translation response: %w", err)
	}
	rawItems, ok := decoded["items"].([]any)
	if !ok {
		return map[string]string{}, nil
	}
	translations := map[string]string{}
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemID := strings.TrimSpace(fmt.Sprintf("%v", item["id"]))
		translated := strings.TrimSpace(fmt.Sprintf("%v", item["translated_title_zh"]))
		if itemID == "" || translated == "" || translated == "<nil>" {
			continue
		}
		translations[itemID] = translated
	}
	return translations, nil
}

func requestJSONContent(cfg LLMConfig, payload map[string]any, operation string, attempt int) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode classifier request: %w", err)
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build classifier request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := classifierHTTPClient.Do(request)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "classifier",
			Action:    operation + "_request_failed",
			Message:   fmt.Sprintf("LLM Request: POST %s failed", endpoint),
			Error:     err.Error(),
			Data: map[string]any{
				"model":       cfg.Model,
				"operation":   operation,
				"attempt":     attempt,
				"duration_ms": time.Since(started).Milliseconds(),
			},
		})
		return "", fmt.Errorf("request classifier: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "warning",
			Component: "classifier",
			Action:    operation + "_response_read_failed",
			Message:   "Failed to read classifier response body.",
			Error:     err.Error(),
			Data: map[string]any{
				"model":       cfg.Model,
				"operation":   operation,
				"attempt":     attempt,
				"status_code": response.StatusCode,
				"duration_ms": time.Since(started).Milliseconds(),
			},
		})
		return "", fmt.Errorf("read classifier response: %w", err)
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    operation + "_request",
		Message:   fmt.Sprintf("LLM Request: POST %s %q", endpoint, response.Proto+" "+response.Status),
		Data: map[string]any{
			"model":          cfg.Model,
			"operation":      operation,
			"attempt":        attempt,
			"status_code":    response.StatusCode,
			"content_length": len(responseBody),
			"duration_ms":    time.Since(started).Milliseconds(),
		},
	})
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		return "", fmt.Errorf("classifier request failed: %s", message)
	}
	var decoded chatCompletionResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		logClassifierRetry(operation+"_envelope_parse_failed", "Classifier response envelope was not valid JSON.", err, cfg.Model, operation, attempt, len(responseBody))
		return "", fmt.Errorf("parse classifier response envelope: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("classifier response did not contain any choices")
	}
	return decoded.Choices[0].Message.Content, nil
}

func requestJSONContentWithFallback(cfg LLMConfig, payload map[string]any, operation string) (string, error) {
	content, err := requestJSONContent(cfg, payload, operation, 1)
	if err == nil && strings.TrimSpace(content) != "" {
		return content, nil
	}
	if err == nil {
		err = fmt.Errorf("classifier response content was empty")
	}
	if normalizedThinking(cfg.Thinking) == "disabled" || !shouldRetryWithoutThinking(err) {
		if !shouldRetryClassifierRequest(err) {
			return "", err
		}
		logRequestRetry(cfg.Model, operation, "primary", 1, err)
		return requestJSONContentWithRetries(cfg, payload, operation, "primary", 2)
	}

	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "classifier",
		Action:    operation + "_thinking_fallback_started",
		Message:   "Retrying request with thinking disabled.",
		Error:     err.Error(),
		Data: map[string]any{
			"model":     cfg.Model,
			"operation": operation,
		},
	})
	fallbackPayload := clonePayload(payload)
	fallbackPayload["thinking"] = map[string]string{"type": "disabled"}
	content, fallbackErr := requestJSONContentWithRetries(cfg, fallbackPayload, operation, "thinking_disabled", 1)
	if fallbackErr != nil {
		_, _ = logging.WriteDefault(logging.Event{
			Level:     "error",
			Component: "classifier",
			Action:    operation + "_thinking_fallback_failed",
			Message:   "Retry with thinking disabled failed.",
			Error:     fallbackErr.Error(),
			Data: map[string]any{
				"model":     cfg.Model,
				"operation": operation,
			},
		})
		return "", fallbackErr
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    operation + "_thinking_fallback_completed",
		Message:   "Retry with thinking disabled succeeded.",
		Data: map[string]any{
			"model":     cfg.Model,
			"operation": operation,
		},
	})
	return content, nil
}

func requestJSONContentWithRetries(cfg LLMConfig, payload map[string]any, operation string, phase string, startAttempt int) (string, error) {
	var lastErr error
	for attempt := startAttempt; attempt <= classifierMaxAttempts; attempt++ {
		content, err := requestJSONContent(cfg, payload, operation, attempt)
		if err == nil {
			if strings.TrimSpace(content) != "" {
				return content, nil
			}
			err = fmt.Errorf("classifier response content was empty")
		}
		lastErr = err
		if attempt == classifierMaxAttempts || !shouldRetryClassifierRequest(err) {
			break
		}
		logRequestRetry(cfg.Model, operation, phase, attempt, err)
		time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
	}
	return "", lastErr
}

func logRequestRetry(model string, operation string, phase string, attempt int, err error) {
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "classifier",
		Action:    operation + "_request_retry",
		Message:   "Retrying classifier request after transient failure.",
		Error:     err.Error(),
		Data: map[string]any{
			"model":     model,
			"operation": operation,
			"phase":     phase,
			"attempt":   attempt,
		},
	})
}

func batchClassificationPrompt(papers []promptPaper, profile map[string]any) string {
	profileJSON, _ := json.MarshalIndent(profilePromptPayload(profile), "", "  ")
	items := make([]map[string]any, 0, len(papers))
	for _, paper := range papers {
		items = append(items, map[string]any{
			"id":       paper.ID,
			"title":    paper.Title,
			"journal":  paper.Journal,
			"abstract": paper.Abstract,
		})
	}
	itemsJSON, _ := json.Marshal(items)
	return strings.TrimSpace(fmt.Sprintf(`
%s

User classification profile:
%s

Return valid JSON only, with this exact shape:
{
  "items": [
    {
      "id": "string",
      "relevance": "direct | indirect | unrelated",
      "confidence": 0.0,
      "reason": "one concise sentence",
      "translated_title_zh": "concise Chinese title translation"
    }
  ]
}

Items:
%s
`, baseClassificationInstructions, string(profileJSON), string(itemsJSON)))
}

func titleTranslationPrompt(papers []promptPaper) string {
	items := make([]map[string]any, 0, len(papers))
	for _, paper := range papers {
		items = append(items, map[string]any{
			"id":    paper.ID,
			"title": paper.Title,
		})
	}
	itemsJSON, _ := json.Marshal(items)
	return strings.TrimSpace(fmt.Sprintf(`
Translate each paper title into concise Simplified Chinese.

Return valid JSON only, with this exact shape:
{
  "items": [
    {
      "id": "string",
      "translated_title_zh": "简体中文标题"
    }
  ]
}

Items:
%s
`, string(itemsJSON)))
}

func profilePromptPayload(profile map[string]any) map[string]any {
	payload := map[string]any{
		"scope":           normalizedString(profile["scope"]),
		"relevance_rules": map[string]any{"direct": []string{}, "indirect": []string{}, "unrelated": []string{}},
	}
	if rawRules, ok := profile["relevance_rules"].(map[string]any); ok {
		payload["relevance_rules"] = map[string]any{
			"direct":    normalizeStringSlice(rawRules["direct"]),
			"indirect":  normalizeStringSlice(rawRules["indirect"]),
			"unrelated": normalizeStringSlice(rawRules["unrelated"]),
		}
	}
	return payload
}

func decodeClassification(item map[string]any, model string) (store.Classification, error) {
	relevance := normalizedString(item["relevance"])
	if relevance != "direct" && relevance != "indirect" && relevance != "unrelated" {
		return store.Classification{}, fmt.Errorf("classifier returned unsupported relevance: %s", relevance)
	}
	classification := store.Classification{
		Relevance:         relevance,
		Confidence:        floatValue(item["confidence"]),
		TopicTags:         []string{},
		Reason:            normalizedString(item["reason"]),
		RecommendedAction: recommendedActionForRelevance(relevance),
		Model:             model,
	}
	translated := normalizedString(item["translated_title_zh"])
	if translated != "" {
		classification.TranslatedTitleZH = &translated
	}
	return classification, nil
}

func recommendedActionForRelevance(relevance string) string {
	switch relevance {
	case "direct":
		return "read"
	case "unrelated":
		return "skip"
	default:
		return "scan"
	}
}

func normalizeStringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if strings, ok := raw.([]string); ok {
			result := make([]string, 0, len(strings))
			seen := map[string]struct{}{}
			for _, item := range strings {
				clean := normalizedString(item)
				if clean == "" {
					continue
				}
				if _, exists := seen[clean]; exists {
					continue
				}
				seen[clean] = struct{}{}
				result = append(result, clean)
			}
			return result
		}
		return []string{}
	}
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		clean := normalizedString(item)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func normalizedString(raw any) string {
	value := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if value == "<nil>" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func floatValue(raw any) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case json.Number:
		parsed, _ := value.Float64()
		return parsed
	default:
		return 0
	}
}

func normalizedThinking(value string) string {
	if strings.TrimSpace(value) == "" {
		return "disabled"
	}
	return strings.TrimSpace(value)
}

func shouldRetryWithoutThinking(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "timeout") ||
		strings.Contains(message, "timed out") ||
		strings.Contains(message, "deadline exceeded") ||
		strings.Contains(message, "thinking") ||
		strings.Contains(message, "reasoning") ||
		strings.Contains(message, "reasoner") ||
		strings.Contains(message, "429") ||
		strings.Contains(message, "500") ||
		strings.Contains(message, "503") ||
		strings.Contains(message, "504") ||
		strings.Contains(message, "502")
}

func shouldRetryClassifierRequest(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"timeout",
		"timed out",
		"deadline exceeded",
		"temporary",
		"connection reset",
		"connection refused",
		"tls handshake",
		"read classifier response",
		"parse classifier response envelope",
		"classifier response content was empty",
		"429",
		"500",
		"502",
		"503",
		"504",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func logClassifierRetry(action string, message string, err error, model string, operation string, attempt int, contentLength int) {
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "warning",
		Component: "classifier",
		Action:    action,
		Message:   message,
		Error:     errorMessage,
		Data: map[string]any{
			"model":          model,
			"operation":      operation,
			"attempt":        attempt,
			"content_length": contentLength,
		},
	})
}

func clonePayload(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
