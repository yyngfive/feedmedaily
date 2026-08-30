package llmusage_test

import (
	"testing"
	"time"

	"github.com/yyngfive/scirssagent/internal/llmusage"
)

func TestCollectorSummarizesOfficialDeepSeekFlashUsage(t *testing.T) {
	collector := llmusage.NewCollector()
	collector.Record(llmusage.Event{
		Role:       "classifier",
		Operation:  "classification",
		BaseURL:    "https://api.deepseek.com",
		Model:      "deepseek-v4-flash",
		OccurredAt: time.Date(2026, 8, 22, 4, 36, 0, 0, time.UTC),
		Usage: llmusage.ResponseUsage{
			PromptTokens:          95_249,
			PromptCacheHitTokens:  52_608,
			PromptCacheMissTokens: 42_641,
			CompletionTokens:      12_547,
			CacheBreakdownPresent: true,
		},
	})

	summary := collector.Summary()
	if summary.RequestCount != 1 || summary.PromptCacheHitTokens != 52_608 || summary.PromptCacheMissTokens != 42_641 || summary.CompletionTokens != 12_547 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.PricingStatus != "estimated" || summary.EstimatedCostNanoCNY == nil || *summary.EstimatedCostNanoCNY != 123_053_400 {
		t.Fatalf("unexpected pricing: %#v", summary)
	}
	if summary.EstimatedCostCNY == nil || *summary.EstimatedCostCNY != "0.123053" {
		t.Fatalf("estimated cost display = %#v", summary.EstimatedCostCNY)
	}
	if len(summary.Models) != 1 || summary.Models[0] != "deepseek-v4-flash" {
		t.Fatalf("models = %#v", summary.Models)
	}
	if len(summary.Pricing) != 1 || summary.Pricing[0].Snapshot != llmusage.PricingSnapshotDeepSeekCNY || summary.Pricing[0].Tier != "off_peak" || summary.Pricing[0].CacheHitNanoCNYPerToken != 50 {
		t.Fatalf("pricing snapshot = %#v", summary.Pricing)
	}
}

func TestCollectorUsesOffPeakPricingOnBeijingWeekends(t *testing.T) {
	collector := llmusage.NewCollector()
	collector.Record(llmusage.Event{
		BaseURL:    "https://api.deepseek.com",
		Model:      "deepseek-v4-flash",
		OccurredAt: time.Date(2026, 8, 23, 7, 22, 2, 0, time.UTC), // Sunday 15:22 in Beijing.
		Usage: llmusage.ResponseUsage{
			PromptTokens: 30_454, PromptCacheHitTokens: 17_280,
			PromptCacheMissTokens: 13_174, CompletionTokens: 4_070,
			CacheBreakdownPresent: true,
		},
	})

	summary := collector.Summary()
	if summary.EstimatedCostCNY == nil || *summary.EstimatedCostCNY != "0.038940" {
		t.Fatalf("weekend cost = %#v, want 0.038940", summary.EstimatedCostCNY)
	}
	if len(summary.Pricing) != 1 || summary.Pricing[0].Tier != llmusage.PricingTierOffPeak {
		t.Fatalf("weekend pricing = %#v", summary.Pricing)
	}
}

func TestCollectorUsesManualPricingSnapshot(t *testing.T) {
	pricing := llmusage.DefaultPricing()
	pricing.Snapshot = "deepseek-cny-manual"
	pricing.Flash.Peak = llmusage.TokenRates{
		CacheHitNanoCNYPerToken:   200,
		CacheMissNanoCNYPerToken:  4_000,
		CompletionNanoCNYPerToken: 10_000,
	}
	collector := llmusage.NewCollector(pricing)
	collector.Record(llmusage.Event{
		BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash",
		OccurredAt: time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC), // Monday 15:00 in Beijing.
		Usage: llmusage.ResponseUsage{
			PromptTokens: 3_000_000, PromptCacheHitTokens: 1_000_000,
			PromptCacheMissTokens: 1_000_000, CompletionTokens: 1_000_000,
			CacheBreakdownPresent: true,
		},
	})

	summary := collector.Summary()
	if summary.EstimatedCostCNY == nil || *summary.EstimatedCostCNY != "14.200000" {
		t.Fatalf("manual cost = %#v, want 14.200000", summary.EstimatedCostCNY)
	}
	if len(summary.Pricing) != 1 || summary.Pricing[0].Snapshot != "deepseek-cny-manual" {
		t.Fatalf("manual pricing snapshot = %#v", summary.Pricing)
	}
}

func TestCollectorUsesDeepSeekProPricingAndAggregatesRequests(t *testing.T) {
	collector := llmusage.NewCollector()
	for _, operation := range []string{"profile_generation", "profile_validation"} {
		collector.Record(llmusage.Event{
			Role: "profile", Operation: operation, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro",
			OccurredAt: time.Date(2026, 8, 24, 7, 0, 0, 0, time.UTC),
			Usage:      llmusage.ResponseUsage{PromptTokens: 2_000_000, PromptCacheHitTokens: 1_000_000, PromptCacheMissTokens: 1_000_000, CompletionTokens: 1_000_000, CacheBreakdownPresent: true},
		})
	}

	summary := collector.Summary()
	if summary.RequestCount != 2 || summary.PromptCacheHitTokens != 2_000_000 || summary.PromptCacheMissTokens != 2_000_000 || summary.CompletionTokens != 2_000_000 {
		t.Fatalf("unexpected aggregate: %#v", summary)
	}
	if summary.EstimatedCostNanoCNY == nil || *summary.EstimatedCostNanoCNY != 72_600_000_000 {
		t.Fatalf("unexpected pro cost: %#v", summary.EstimatedCostNanoCNY)
	}
	if len(summary.Operations) != 2 {
		t.Fatalf("operations = %#v", summary.Operations)
	}
}

func TestCollectorPricesOfficialGLM53FlashUsage(t *testing.T) {
	collector := llmusage.NewCollector()
	collector.Record(llmusage.Event{
		BaseURL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5.3-flash",
		Usage: llmusage.ResponseUsage{
			PromptTokens: 2_000_000, PromptCacheHitTokens: 1_000_000,
			PromptCacheMissTokens: 1_000_000, CompletionTokens: 1_000_000,
			CacheBreakdownPresent: true,
		},
	})

	summary := collector.Summary()
	if summary.PricingStatus != "estimated" || summary.EstimatedCostCNY == nil || *summary.EstimatedCostCNY != "1.915000" {
		t.Fatalf("GLM estimate = %#v", summary)
	}
	if len(summary.Pricing) != 1 || summary.Pricing[0].Snapshot != llmusage.PricingSnapshotGLM53FlashCNY || summary.Pricing[0].Tier != "standard" {
		t.Fatalf("GLM pricing snapshot = %#v", summary.Pricing)
	}
}

func TestCollectorPricesQwenAndMiMoOfficialMainlandRates(t *testing.T) {
	tests := []struct {
		name, baseURL, model, snapshot, cost string
	}{
		{"qwen", "https://dashscope.aliyuncs.com/compatible-mode/v1", "qwen3.8-flash", llmusage.PricingSnapshotQwen38FlashCNY, "3.600000"},
		{"mimo", "https://api.xiaomimimo.com/v1", "mimo-v2.5", llmusage.PricingSnapshotMiMoV25CNY, "3.020000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector := llmusage.NewCollector()
			collector.Record(llmusage.Event{BaseURL: test.baseURL, Model: test.model, Usage: llmusage.ResponseUsage{
				PromptTokens: 2_000_000, PromptCacheHitTokens: 1_000_000, PromptCacheMissTokens: 1_000_000,
				CompletionTokens: 1_000_000, CacheBreakdownPresent: true,
			}})
			summary := collector.Summary()
			if summary.EstimatedCostCNY == nil || *summary.EstimatedCostCNY != test.cost {
				t.Fatalf("cost = %#v, want %s", summary.EstimatedCostCNY, test.cost)
			}
			if len(summary.Pricing) != 1 || summary.Pricing[0].Snapshot != test.snapshot {
				t.Fatalf("pricing = %#v", summary.Pricing)
			}
		})
	}
}

func TestCollectorDoesNotPriceUnknownProviderOrModel(t *testing.T) {
	for name, event := range map[string]llmusage.Event{
		"proxy": {
			Role: "classifier", Operation: "classification", BaseURL: "https://proxy.example.com", Model: "deepseek-v4-flash",
			Usage: llmusage.ResponseUsage{PromptTokens: 3, PromptCacheHitTokens: 1, PromptCacheMissTokens: 2, CompletionTokens: 4, CacheBreakdownPresent: true},
		},
		"unknown model": {
			Role: "classifier", Operation: "classification", BaseURL: "https://api.deepseek.com", Model: "future-model",
			Usage: llmusage.ResponseUsage{PromptTokens: 3, PromptCacheHitTokens: 1, PromptCacheMissTokens: 2, CompletionTokens: 4, CacheBreakdownPresent: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			collector := llmusage.NewCollector()
			collector.Record(event)
			summary := collector.Summary()
			if summary.PricingStatus != "unavailable" || summary.EstimatedCostNanoCNY != nil || summary.EstimatedCostCNY != nil {
				t.Fatalf("unexpected pricing: %#v", summary)
			}
			if summary.PromptCacheHitTokens != 1 || summary.PromptCacheMissTokens != 2 || summary.CompletionTokens != 4 {
				t.Fatalf("tokens should still be retained: %#v", summary)
			}
		})
	}
}
