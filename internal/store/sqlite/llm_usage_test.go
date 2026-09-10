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
	if items[0].EstimatedCostNanoCNY == nil || *items[0].EstimatedCostNanoCNY != 22_040 {
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
	if len(items) != 1 || items[0].EstimatedCostNanoCNY == nil || *items[0].EstimatedCostNanoCNY != 93_881_160 {
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
	if len(items) != 1 || items[0].EstimatedCostCNY == nil || *items[0].EstimatedCostCNY != "0.029800" {
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

func TestOpenOrCreateRepairsPricingSupersededBySeptemberPriceChanges(t *testing.T) {
	tests := []struct {
		name        string
		jobID       string
		model       string
		pricingJSON string
		costNano    int64
		costCNY     string
		completedAt string
		listSince   time.Time
		wantCostCNY string
		wantRepair  bool
	}{
		{
			// DeepSeek cut Flash prices at 12:00 Beijing on 2026-09-10, so this
			// 13:00 Beijing row was still priced on the previous rate card.
			name: "deepseek flash after cutover", jobID: "flash-cutover", model: "deepseek-flash",
			pricingJSON: `[{"model":"deepseek-flash","snapshot":"deepseek-cny-2026-08-23-weekdays","tier":"off_peak","cache_hit_nano_cny_per_token":50,"cache_miss_nano_cny_per_token":1500,"completion_nano_cny_per_token":4500}]`,
			costNano:    6_050_000_000, costCNY: "6.050000", completedAt: "2026-09-10T05:00:00Z",
			listSince:   time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
			wantCostCNY: "5.020000", wantRepair: true,
		},
		{
			// A row completed before the cutover keeps its historical estimate.
			name: "deepseek flash before cutover", jobID: "flash-before", model: "deepseek-flash",
			pricingJSON: `[{"model":"deepseek-flash","snapshot":"deepseek-cny-2026-08-23-weekdays","tier":"off_peak","cache_hit_nano_cny_per_token":50,"cache_miss_nano_cny_per_token":1500,"completion_nano_cny_per_token":4500}]`,
			costNano:    6_050_000_000, costCNY: "6.050000", completedAt: "2026-09-10T02:00:00Z",
			listSince:   time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
			wantCostCNY: "6.050000", wantRepair: false,
		},
		{
			// Zhipu's GLM-5.3-Flash promotional rates ended at 24:00 Beijing on
			// 2026-09-09, doubling the standard input and output prices.
			name: "glm promo after promo end", jobID: "glm-promo", model: "glm-5.3-flash",
			pricingJSON: `[{"model":"glm-5.3-flash","snapshot":"zhipu-glm-5.3-flash-cny-2026-08-28-promo","tier":"standard","cache_hit_nano_cny_per_token":115,"cache_miss_nano_cny_per_token":400,"completion_nano_cny_per_token":1400}]`,
			costNano:    1_915_000_000, costCNY: "1.915000", completedAt: "2026-09-10T01:00:00Z",
			listSince:   time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
			wantCostCNY: "3.830000", wantRepair: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "literature.sqlite")
			store, err := OpenOrCreate(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`
INSERT INTO llm_usage_jobs (
  job_id, job_type, status, model, request_count, prompt_tokens,
  prompt_cache_hit_tokens, prompt_cache_miss_tokens, completion_tokens,
  pricing_status, pricing_json, estimated_cost_nano_cny, estimated_cost_cny, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, test.jobID, "sync", "completed", test.model, 1, 3_000_000, 1_000_000, 1_000_000, 1_000_000,
				"estimated", test.pricingJSON, test.costNano, test.costCNY, test.completedAt); err != nil {
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
			items, err := reopened.ListLLMUsage(test.listSince)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].EstimatedCostCNY == nil || *items[0].EstimatedCostCNY != test.wantCostCNY {
				t.Fatalf("repaired usage = %#v, want %s", items, test.wantCostCNY)
			}
			repaired := items[0].Pricing[0].Snapshot != "deepseek-cny-2026-08-23-weekdays" && items[0].Pricing[0].Snapshot != "zhipu-glm-5.3-flash-cny-2026-08-28-promo"
			if repaired != test.wantRepair {
				t.Fatalf("pricing snapshot = %#v, want repaired=%v", items[0].Pricing, test.wantRepair)
			}
		})
	}
}
