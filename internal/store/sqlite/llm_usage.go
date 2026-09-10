package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yyngfive/scirssagent/internal/llmusage"
)

type LLMUsageRecord struct {
	JobID                 string                      `json:"job_id"`
	JobType               string                      `json:"job_type"`
	Status                string                      `json:"status"`
	Model                 string                      `json:"model"`
	RequestCount          int                         `json:"request_count"`
	PromptTokens          int64                       `json:"prompt_tokens"`
	PromptCacheHitTokens  int64                       `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int64                       `json:"prompt_cache_miss_tokens"`
	CompletionTokens      int64                       `json:"completion_tokens"`
	PricingStatus         string                      `json:"pricing_status"`
	Pricing               []llmusage.PricingBreakdown `json:"pricing"`
	EstimatedCostNanoCNY  *int64                      `json:"estimated_cost_nano_cny,omitempty"`
	EstimatedCostCNY      *string                     `json:"estimated_cost_cny,omitempty"`
	CompletedAt           time.Time                   `json:"completed_at"`
}

// chinaStandardTime matches the provider billing timezone used for peak hours.
var chinaStandardTime = time.FixedZone("CST", 8*60*60)

func repairIncompleteCacheBreakdownPricing(db *sql.DB) error {
	rows, err := db.Query(`
SELECT job_id, model, prompt_tokens, prompt_cache_hit_tokens,
       prompt_cache_miss_tokens, completion_tokens, pricing_json
FROM llm_usage_jobs
WHERE pricing_status = 'unavailable'
  AND prompt_tokens > prompt_cache_hit_tokens + prompt_cache_miss_tokens
  AND pricing_json <> '[]'
ORDER BY completed_at
`)
	if err != nil {
		return fmt.Errorf("query incomplete cache pricing: %w", err)
	}
	type incompleteUsage struct {
		jobID, model, pricingJSON                     string
		promptTokens, cacheHit, cacheMiss, completion int64
	}
	incomplete := []incompleteUsage{}
	for rows.Next() {
		var item incompleteUsage
		if err := rows.Scan(&item.jobID, &item.model, &item.promptTokens, &item.cacheHit, &item.cacheMiss, &item.completion, &item.pricingJSON); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan incomplete cache pricing: %w", err)
		}
		incomplete = append(incomplete, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate incomplete cache pricing: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close incomplete cache pricing rows: %w", err)
	}

	for _, item := range incomplete {
		var pricing []llmusage.PricingBreakdown
		if err := json.Unmarshal([]byte(item.pricingJSON), &pricing); err != nil {
			continue
		}
		if len(pricing) != 1 || !strings.EqualFold(strings.TrimSpace(item.model), strings.TrimSpace(pricing[0].Model)) {
			continue
		}
		rates := pricing[0]
		if rates.CacheHitNanoCNYPerToken < 0 || rates.CacheMissNanoCNYPerToken < 0 || rates.CompletionNanoCNYPerToken < 0 {
			continue
		}
		normalizedMiss := item.promptTokens - item.cacheHit
		cost := item.cacheHit*rates.CacheHitNanoCNYPerToken +
			normalizedMiss*rates.CacheMissNanoCNYPerToken +
			item.completion*rates.CompletionNanoCNYPerToken
		display := fmt.Sprintf("%.6f", float64(cost)/1_000_000_000)
		if _, err := db.Exec(`
UPDATE llm_usage_jobs
SET prompt_cache_miss_tokens = ?, pricing_status = 'estimated',
    estimated_cost_nano_cny = ?, estimated_cost_cny = ?
WHERE job_id = ? AND pricing_status = 'unavailable'
`, normalizedMiss, cost, display, item.jobID); err != nil {
			return fmt.Errorf("repair incomplete cache pricing for job %s: %w", item.jobID, err)
		}
	}
	return nil
}

// stalePricingRule describes a default price snapshot that a later official price
// change replaced. Usage rows that were priced with that snapshot after the change
// happened are repriced with the current default rates; rows recorded while the
// snapshot was still current keep their historical estimate.
type stalePricingRule struct {
	snapshot string
	since    time.Time
	// peakWeekendOnly limits the rule to rows whose recorded tier was peak on a
	// weekend, the only rows one superseded snapshot mispriced.
	peakWeekendOnly bool
}

var stalePricingRules = []stalePricingRule{
	// The 2026-07-24 snapshot priced every request at off-peak rates, before the
	// weekday peak/off-peak model arrived with the 2026-08-21 snapshot.
	{snapshot: "deepseek-cny-2026-07-24", since: time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)},
	// The 2026-08-21 snapshot charged weekend peak hours at weekday peak rates.
	{snapshot: "deepseek-cny-2026-08-21", since: time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC), peakWeekendOnly: true},
	// DeepSeek cut Flash prices at 12:00 Beijing on 2026-09-10.
	{snapshot: "deepseek-cny-2026-08-23", since: time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)},
	// Zhipu's GLM-5.3-Flash promotional rates ended at 24:00 Beijing on 2026-09-09.
	{snapshot: "zhipu-glm-5.3-flash-cny-2026-08-28-promo", since: time.Date(2026, 9, 9, 16, 0, 0, 0, time.UTC)},
}

// repairSupersededPricing reprices only the rows whose stored snapshot is known to
// have been superseded after they were written. Manually saved prices and every
// other historical row are left untouched.
func repairSupersededPricing(db *sql.DB) error {
	conditions := make([]string, 0, len(stalePricingRules))
	arguments := make([]any, 0, len(stalePricingRules)*2)
	for _, rule := range stalePricingRules {
		conditions = append(conditions, `(pricing_json LIKE ? AND completed_at >= ?)`)
		arguments = append(arguments, "%"+rule.snapshot+"%", rule.since.Format(time.RFC3339Nano))
	}
	rows, err := db.Query(`
SELECT job_id, model, request_count, prompt_tokens,
       prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens, pricing_json, completed_at
FROM llm_usage_jobs
WHERE `+strings.Join(conditions, " OR ")+`
ORDER BY completed_at
`, arguments...)
	if err != nil {
		return fmt.Errorf("query superseded pricing: %w", err)
	}
	type staleUsage struct {
		jobID, model, pricingJSON, completedAt              string
		requestCount                                        int
		promptTokens, cacheHit, cacheMiss, completionTokens int64
	}
	stale := []staleUsage{}
	for rows.Next() {
		var item staleUsage
		if err := rows.Scan(&item.jobID, &item.model, &item.requestCount, &item.promptTokens, &item.cacheHit, &item.cacheMiss, &item.completionTokens, &item.pricingJSON, &item.completedAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan superseded pricing: %w", err)
		}
		stale = append(stale, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate superseded pricing: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close superseded pricing rows: %w", err)
	}

	for _, item := range stale {
		completedAt, err := parseTime(item.completedAt)
		if err != nil {
			return fmt.Errorf("parse superseded pricing completed_at: %w", err)
		}
		rule, ok := matchingStalePricingRule(item.pricingJSON, completedAt)
		if !ok {
			continue
		}
		if rule.peakWeekendOnly {
			weekday := completedAt.In(chinaStandardTime).Weekday()
			if weekday != time.Saturday && weekday != time.Sunday {
				continue
			}
			if !strings.Contains(item.pricingJSON, `"tier":"peak"`) {
				continue
			}
		}
		baseURL, ok := pricingRepairBaseURL(item.model)
		if !ok {
			continue
		}
		collector := llmusage.NewCollector()
		collector.Record(llmusage.Event{
			BaseURL: baseURL, Model: item.model, OccurredAt: completedAt,
			Usage: llmusage.ResponseUsage{
				PromptTokens: item.promptTokens, PromptCacheHitTokens: item.cacheHit,
				PromptCacheMissTokens: item.cacheMiss, CompletionTokens: item.completionTokens,
				CacheBreakdownPresent: true,
			},
		})
		summary := collector.Summary()
		if summary.PricingStatus != "estimated" {
			continue
		}
		summary.RequestCount = item.requestCount
		pricingJSON, err := json.Marshal(summary.Pricing)
		if err != nil {
			return fmt.Errorf("encode repaired pricing: %w", err)
		}
		if _, err := db.Exec(`
UPDATE llm_usage_jobs
SET pricing_status = ?, pricing_json = ?, estimated_cost_nano_cny = ?, estimated_cost_cny = ?
WHERE job_id = ?
`, summary.PricingStatus, string(pricingJSON), summary.EstimatedCostNanoCNY, summary.EstimatedCostCNY, item.jobID); err != nil {
			return fmt.Errorf("repair pricing for job %s: %w", item.jobID, err)
		}
	}
	return nil
}

func matchingStalePricingRule(pricingJSON string, completedAt time.Time) (stalePricingRule, bool) {
	for _, rule := range stalePricingRules {
		if strings.Contains(pricingJSON, rule.snapshot) && !completedAt.Before(rule.since) {
			return rule, true
		}
	}
	return stalePricingRule{}, false
}

func pricingRepairBaseURL(model string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "glm-5.3-flash" {
		return "https://open.bigmodel.cn/api/paas/v4", true
	}
	if strings.Contains(normalized, "deepseek") {
		return "https://api.deepseek.com", true
	}
	return "", false
}

func (s *Store) SaveLLMUsage(jobID string, jobType string, status string, summary llmusage.Summary, completedAt time.Time) error {
	pricingJSON, err := json.Marshal(summary.Pricing)
	if err != nil {
		return fmt.Errorf("encode LLM pricing snapshot: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO llm_usage_jobs (
  job_id, job_type, status, model, request_count, prompt_tokens,
  prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
  pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET
  status = excluded.status,
  model = excluded.model,
  request_count = excluded.request_count,
  prompt_tokens = excluded.prompt_tokens,
  prompt_cache_hit_tokens = excluded.prompt_cache_hit_tokens,
  prompt_cache_miss_tokens = excluded.prompt_cache_miss_tokens,
  completion_tokens = excluded.completion_tokens,
  pricing_status = excluded.pricing_status,
  pricing_json = excluded.pricing_json,
  estimated_cost_nano_cny = excluded.estimated_cost_nano_cny,
  estimated_cost_cny = excluded.estimated_cost_cny,
  completed_at = excluded.completed_at
`, jobID, jobType, status, strings.Join(summary.Models, ", "), summary.RequestCount, summary.PromptTokens,
		summary.PromptCacheHitTokens, summary.PromptCacheMissTokens, summary.CompletionTokens,
		summary.PricingStatus, string(pricingJSON), summary.EstimatedCostNanoCNY, summary.EstimatedCostCNY, completedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save LLM usage: %w", err)
	}
	return nil
}

func (s *Store) ListLLMUsage(since time.Time) ([]LLMUsageRecord, error) {
	rows, err := s.db.Query(`
SELECT job_id, job_type, status, model, request_count, prompt_tokens,
       prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
       pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
FROM llm_usage_jobs
WHERE completed_at >= ?
ORDER BY completed_at DESC, job_id DESC
`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return []LLMUsageRecord{}, nil
		}
		return nil, fmt.Errorf("query LLM usage: %w", err)
	}
	defer rows.Close()

	items := []LLMUsageRecord{}
	for rows.Next() {
		var item LLMUsageRecord
		var pricingJSON string
		var cost sql.NullInt64
		var costCNY sql.NullString
		var completedAt string
		if err := rows.Scan(&item.JobID, &item.JobType, &item.Status, &item.Model, &item.RequestCount, &item.PromptTokens,
			&item.PromptCacheHitTokens, &item.PromptCacheMissTokens, &item.CompletionTokens,
			&item.PricingStatus, &pricingJSON, &cost, &costCNY, &completedAt); err != nil {
			return nil, fmt.Errorf("scan LLM usage: %w", err)
		}
		if err := json.Unmarshal([]byte(pricingJSON), &item.Pricing); err != nil {
			return nil, fmt.Errorf("parse LLM pricing snapshot: %w", err)
		}
		parsed, err := parseTime(completedAt)
		if err != nil {
			return nil, fmt.Errorf("parse LLM usage completed_at: %w", err)
		}
		item.CompletedAt = parsed
		if cost.Valid {
			value := cost.Int64
			item.EstimatedCostNanoCNY = &value
		}
		if costCNY.Valid {
			value := costCNY.String
			item.EstimatedCostCNY = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate LLM usage: %w", err)
	}
	return items, nil
}
