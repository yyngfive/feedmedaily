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

var legacyDeepSeekPricingRepairSince = time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)

func repairLegacyDeepSeekPricing(db *sql.DB) error {
	rows, err := db.Query(`
SELECT job_id, model, request_count, prompt_tokens,
       prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens, pricing_json, completed_at
FROM llm_usage_jobs
WHERE (pricing_json LIKE '%deepseek-cny-2026-07-24%' AND completed_at >= ?)
   OR (pricing_json LIKE '%deepseek-cny-2026-08-21%' AND pricing_json LIKE '%"tier":"peak"%')
ORDER BY completed_at
`, legacyDeepSeekPricingRepairSince.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("query legacy DeepSeek pricing: %w", err)
	}
	type legacyUsage struct {
		jobID, model, pricingJSON, completedAt              string
		requestCount                                        int
		promptTokens, cacheHit, cacheMiss, completionTokens int64
	}
	legacy := []legacyUsage{}
	for rows.Next() {
		var item legacyUsage
		if err := rows.Scan(&item.jobID, &item.model, &item.requestCount, &item.promptTokens, &item.cacheHit, &item.cacheMiss, &item.completionTokens, &item.pricingJSON, &item.completedAt); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan legacy DeepSeek pricing: %w", err)
		}
		legacy = append(legacy, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate legacy DeepSeek pricing: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy DeepSeek pricing rows: %w", err)
	}

	for _, item := range legacy {
		completedAt, err := parseTime(item.completedAt)
		if err != nil {
			return fmt.Errorf("parse legacy DeepSeek completed_at: %w", err)
		}
		if !strings.Contains(item.pricingJSON, "deepseek-cny-2026-07-24") {
			weekday := completedAt.In(time.FixedZone("CST", 8*60*60)).Weekday()
			if weekday != time.Saturday && weekday != time.Sunday {
				continue
			}
		}
		collector := llmusage.NewCollector()
		collector.Record(llmusage.Event{
			BaseURL: "https://api.deepseek.com", Model: item.model, OccurredAt: completedAt,
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
			return fmt.Errorf("encode repaired DeepSeek pricing: %w", err)
		}
		if _, err := db.Exec(`
UPDATE llm_usage_jobs
SET pricing_status = ?, pricing_json = ?, estimated_cost_nano_cny = ?, estimated_cost_cny = ?
WHERE job_id = ?
`, summary.PricingStatus, string(pricingJSON), summary.EstimatedCostNanoCNY, summary.EstimatedCostCNY, item.jobID); err != nil {
			return fmt.Errorf("repair DeepSeek pricing for job %s: %w", item.jobID, err)
		}
	}
	return nil
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
