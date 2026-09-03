package sqlite

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/llmusage"
)

func TestLLMUsagePersistsAndFiltersByCompletionTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	collector := llmusage.NewCollector()
	newTime := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	collector.Record(llmusage.Event{
		BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", Operation: "classification", OccurredAt: newTime,
		Usage: llmusage.ResponseUsage{PromptTokens: 12, PromptCacheHitTokens: 2, PromptCacheMissTokens: 10, CompletionTokens: 3, CacheBreakdownPresent: true},
	})
	oldTime := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if err := store.SaveLLMUsage("old", "sync", "completed", collector.Summary(), oldTime); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLLMUsage("new", "reclassify", "failed", collector.Summary(), newTime); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenRead(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListLLMUsage(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].JobID != "new" || items[0].Status != "failed" || items[0].RequestCount != 1 {
		t.Fatalf("unexpected usage rows: %#v", items)
	}
	if items[0].EstimatedCostNanoCNY == nil || *items[0].EstimatedCostNanoCNY != 28_600 {
		t.Fatalf("unexpected persisted cost: %#v", items[0].EstimatedCostNanoCNY)
	}
}

func TestOpenOrCreateRepairsConfirmedAugust22LegacyPricing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	legacyPricing := `[{"model":"deepseek-v4-flash","snapshot":"deepseek-cny-2026-07-24","cache_hit_nano_cny_per_token":20,"cache_miss_nano_cny_per_token":1000,"completion_nano_cny_per_token":2000}]`
	if _, err := store.db.Exec(`
INSERT INTO llm_usage_jobs (
  job_id, job_type, status, model, request_count, prompt_tokens,
  prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
  pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, "legacy-sync", "sync", "completed", "deepseek-v4-flash", 28, 95_249, 52_608, 42_641, 12_547,
		"estimated", legacyPricing, 68_787_160, "0.068787", "2026-08-22T04:36:23Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListLLMUsage(time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EstimatedCostNanoCNY == nil || *items[0].EstimatedCostNanoCNY != 123_053_400 {
		t.Fatalf("unexpected repaired usage: %#v", items)
	}
	if items[0].RequestCount != 28 || len(items[0].Pricing) != 1 || items[0].Pricing[0].Tier != llmusage.PricingTierOffPeak || items[0].Pricing[0].Snapshot != llmusage.PricingSnapshotDeepSeekCNY {
		t.Fatalf("unexpected repaired pricing snapshot: %#v", items[0])
	}
}

func TestOpenOrCreateRepairsWeekendRowsMispricedAsPeak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	buggyPricing := `[{"model":"deepseek-v4-flash","snapshot":"deepseek-cny-2026-08-21","tier":"peak","cache_hit_nano_cny_per_token":100,"cache_miss_nano_cny_per_token":3000,"completion_nano_cny_per_token":9000}]`
	if _, err := store.db.Exec(`
INSERT INTO llm_usage_jobs (
  job_id, job_type, status, model, request_count, prompt_tokens,
  prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
  pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, "weekend-sync", "sync", "completed", "deepseek-v4-flash", 9, 30_454, 17_280, 13_174, 4_070,
		"estimated", buggyPricing, 77_880_000, "0.077880", "2026-08-23T07:22:02Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListLLMUsage(time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EstimatedCostCNY == nil || *items[0].EstimatedCostCNY != "0.038940" {
		t.Fatalf("repaired weekend usage = %#v", items)
	}
	if len(items[0].Pricing) != 1 || items[0].Pricing[0].Tier != llmusage.PricingTierOffPeak {
		t.Fatalf("repaired weekend pricing = %#v", items[0].Pricing)
	}
}

func TestOpenOrCreateRepairsSingleRateUsageWithMissingCacheBreakdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "literature.sqlite")
	store, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	pricing := `[{"model":"mimo-v2.5","snapshot":"xiaomi-mimo-cny-manual","tier":"standard","cache_hit_nano_cny_per_token":20,"cache_miss_nano_cny_per_token":1000,"completion_nano_cny_per_token":2000}]`
	if _, err := store.db.Exec(`
INSERT INTO llm_usage_jobs (
  job_id, job_type, status, model, request_count, prompt_tokens,
  prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
  pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, "mimo-sync", "sync", "completed", "mimo-v2.5", 57, 177_142, 110_656, 62_905, 27_493,
		"unavailable", pricing, nil, nil, "2026-09-03T04:43:24Z"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err := reopened.ListLLMUsage(time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EstimatedCostCNY == nil || *items[0].EstimatedCostCNY != "0.123685" {
		t.Fatalf("repaired MiMo usage = %#v", items)
	}
	if items[0].PricingStatus != "estimated" || items[0].PromptCacheMissTokens != 66_486 {
		t.Fatalf("repaired MiMo cache breakdown = %#v", items[0])
	}
}
