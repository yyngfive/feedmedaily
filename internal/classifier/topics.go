package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yyngfive/scirssagent/internal/logging"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

const topicAssignInstructions = `Assign each paper to at most one topic using the user-supplied topic registry and tagged rules.

- The registry lists topic ids and labels; each direct/indirect relevance rule carries at most one topic id.
- For each paper, consider the topic of every rule whose criteria the paper satisfies, and pick the single best-fitting topic id.
- If no tagged rule fits the paper, or none of the candidate topics fits, set topic to "" (empty string).
- Use topic ids exactly as they appear in the registry. Never invent topic ids.`

// AssignTopicsBatch 为存量 related 论文做 topic-only 补跑：不重新判相关性，
// 只依据注册表和带标规则输出单个主题 id，空串表示“判定过但无主题”。
// 注册表为空或没有任何带标规则时跳过 LLM 调用，直接返回全空结果。
func AssignTopicsBatch(papers []store.Paper, profile map[string]any, cfg LLMConfig) (map[int64]string, error) {
	ctx := cfg.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		if strings.TrimSpace(cfg.Model) != "" {
			return nil, fmt.Errorf("API key is required for classifier model %s.", cfg.Model)
		}
		return nil, fmt.Errorf("classifier API key is required for topic assignment.")
	}
	valid := validTopicIDs(profile)
	hasTaggedRules := false
	if rawRules, ok := profile["relevance_rules"].(map[string]any); ok {
		for _, section := range []string{"direct", "indirect"} {
			for _, raw := range normalizeRulePayload(rawRules[section]) {
				if tags, ok := raw["topics"].([]string); ok && len(tags) > 0 {
					hasTaggedRules = true
					break
				}
			}
			if hasTaggedRules {
				break
			}
		}
	}
	result := map[int64]string{}
	if len(valid) == 0 || !hasTaggedRules {
		// 没有可路由的主题：全部记为“判定过、无主题”，由调用方写哨兵。
		for _, paper := range papers {
			result[paper.ID] = ""
		}
		return result, nil
	}

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
			{"role": "user", "content": topicAssignmentPrompt(indexed, profile)},
		},
		"temperature":     0,
		"max_tokens":      max(300, 120*len(papers)),
		"response_format": map[string]string{"type": "json_object"},
	}
	applyProviderControls(cfg, payload, false)

	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    "topic_assignment_started",
		Message:   fmt.Sprintf("Assigning topics to %d paper(s)", len(papers)),
		Data:      map[string]any{"model": cfg.Model, "items": len(papers)},
	})

	var decoded map[string]any
	lastContent := ""
	var lastParseErr error
	for attempt := 1; attempt <= classifierMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content, err := requestJSONContentWithFallback(cfg, payload, "topic_assignment")
		if err != nil {
			return nil, err
		}
		lastContent = content
		if strings.TrimSpace(content) == "" {
			lastParseErr = fmt.Errorf("empty topic assignment response content")
			continue
		}
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			lastParseErr = fmt.Errorf("parse topic assignment response: %w", err)
			if attempt < classifierMaxAttempts {
				logClassifierRetry("topic_assignment_parse_retry", "Retrying after malformed topic assignment JSON.", lastParseErr, cfg.Model, "topic_assignment", attempt, len(content))
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
		return nil, fmt.Errorf("topic assignment returned empty JSON content length=%d", len(lastContent))
	}
	rawItems, ok := decoded["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("topic assignment JSON response missing items list")
	}
	byID := map[string]string{}
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		itemID := strings.TrimSpace(fmt.Sprintf("%v", item["id"]))
		if itemID == "" {
			continue
		}
		topic := normalizedString(item["topic"])
		if _, ok := valid[topic]; ok {
			byID[itemID] = topic
		} else {
			// 未注册 id 一律按“无主题”处理（防幻觉）。
			byID[itemID] = ""
		}
	}
	missing := make([]string, 0)
	for _, item := range indexed {
		topic, ok := byID[item.ID]
		if !ok {
			missing = append(missing, item.ID)
			continue
		}
		result[item.PaperID] = topic
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("topic assignment response missing papers: %s", strings.Join(missing, ", "))
	}
	_, _ = logging.WriteDefault(logging.Event{
		Level:     "info",
		Component: "classifier",
		Action:    "topic_assignment_completed",
		Message:   fmt.Sprintf("Assigned topics to %d paper(s)", len(result)),
		Data:      map[string]any{"model": cfg.Model, "items": len(result)},
	})
	return result, nil
}

func topicAssignmentPrompt(papers []promptPaper, profile map[string]any) string {
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
      "topic": "topic id from the profile topics registry, or empty string"
    }
  ]
}

Items:
%s
`, topicAssignInstructions, string(profileJSON), string(itemsJSON)))
}
