package llmusage

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	PricingSnapshotDeepSeekCNY    = "deepseek-cny-2026-08-23-weekdays"
	PricingSnapshotDeepSeekManual = "deepseek-cny-manual"
)

const (
	PricingTierOffPeak = "off_peak"
	PricingTierPeak    = "peak"
)

var chinaStandardTime = time.FixedZone("CST", 8*60*60)

type ResponseUsage struct {
	PromptTokens          int64 `json:"prompt_tokens"`
	PromptCacheHitTokens  int64 `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int64 `json:"prompt_cache_miss_tokens"`
	CompletionTokens      int64 `json:"completion_tokens"`
	CacheBreakdownPresent bool  `json:"-"`
}

type Event struct {
	Role       string
	Operation  string
	BaseURL    string
	Model      string
	OccurredAt time.Time
	Usage      ResponseUsage
}

type PricingBreakdown struct {
	Model                     string `json:"model"`
	Snapshot                  string `json:"snapshot"`
	Tier                      string `json:"tier"`
	CacheHitNanoCNYPerToken   int64  `json:"cache_hit_nano_cny_per_token"`
	CacheMissNanoCNYPerToken  int64  `json:"cache_miss_nano_cny_per_token"`
	CompletionNanoCNYPerToken int64  `json:"completion_nano_cny_per_token"`
}

type TokenRates struct {
	CacheHitNanoCNYPerToken   int64
	CacheMissNanoCNYPerToken  int64
	CompletionNanoCNYPerToken int64
}

type TieredRates struct {
	OffPeak TokenRates
	Peak    TokenRates
}

type DeepSeekPricing struct {
	Snapshot string
	Flash    TieredRates
	Pro      TieredRates
}

type Summary struct {
	RequestCount          int                `json:"request_count"`
	PromptTokens          int64              `json:"prompt_tokens"`
	PromptCacheHitTokens  int64              `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int64              `json:"prompt_cache_miss_tokens"`
	CompletionTokens      int64              `json:"completion_tokens"`
	Models                []string           `json:"models"`
	Operations            []string           `json:"operations"`
	PricingStatus         string             `json:"pricing_status"`
	Pricing               []PricingBreakdown `json:"pricing"`
	EstimatedCostNanoCNY  *int64             `json:"estimated_cost_nano_cny,omitempty"`
	EstimatedCostCNY      *string            `json:"estimated_cost_cny,omitempty"`
}

type Collector struct {
	mu      sync.Mutex
	events  []Event
	pricing DeepSeekPricing
}

func DefaultDeepSeekPricing() DeepSeekPricing {
	return DeepSeekPricing{
		Snapshot: PricingSnapshotDeepSeekCNY,
		Flash: TieredRates{
			OffPeak: TokenRates{CacheHitNanoCNYPerToken: 50, CacheMissNanoCNYPerToken: 1_500, CompletionNanoCNYPerToken: 4_500},
			Peak:    TokenRates{CacheHitNanoCNYPerToken: 100, CacheMissNanoCNYPerToken: 3_000, CompletionNanoCNYPerToken: 9_000},
		},
		Pro: TieredRates{
			OffPeak: TokenRates{CacheHitNanoCNYPerToken: 150, CacheMissNanoCNYPerToken: 4_500, CompletionNanoCNYPerToken: 13_500},
			Peak:    TokenRates{CacheHitNanoCNYPerToken: 300, CacheMissNanoCNYPerToken: 9_000, CompletionNanoCNYPerToken: 27_000},
		},
	}
}

func NewCollector(overrides ...DeepSeekPricing) *Collector {
	pricing := DefaultDeepSeekPricing()
	if len(overrides) > 0 && strings.TrimSpace(overrides[0].Snapshot) != "" {
		pricing = overrides[0]
	}
	return &Collector{pricing: pricing}
}

func (c *Collector) Record(event Event) {
	if c == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *Collector) Summary() Summary {
	if c == nil {
		return Summary{PricingStatus: "unavailable"}
	}
	c.mu.Lock()
	events := append([]Event(nil), c.events...)
	c.mu.Unlock()

	summary := Summary{PricingStatus: "estimated"}
	models := map[string]struct{}{}
	operations := map[string]struct{}{}
	pricingByModel := map[string]PricingBreakdown{}
	var totalCost int64
	for _, event := range events {
		summary.RequestCount++
		summary.PromptTokens += event.Usage.PromptTokens
		summary.PromptCacheHitTokens += event.Usage.PromptCacheHitTokens
		summary.PromptCacheMissTokens += event.Usage.PromptCacheMissTokens
		summary.CompletionTokens += event.Usage.CompletionTokens
		model := strings.TrimSpace(event.Model)
		if model != "" {
			models[model] = struct{}{}
		}
		if operation := strings.TrimSpace(event.Operation); operation != "" {
			operations[operation] = struct{}{}
		}
		rates, ok := deepSeekRates(event.BaseURL, model, event.OccurredAt, c.pricing)
		if !ok || !event.Usage.CacheBreakdownPresent {
			summary.PricingStatus = "unavailable"
			continue
		}
		pricingByModel[model+"|"+rates.Tier] = rates
		totalCost += event.Usage.PromptCacheHitTokens*rates.CacheHitNanoCNYPerToken +
			event.Usage.PromptCacheMissTokens*rates.CacheMissNanoCNYPerToken +
			event.Usage.CompletionTokens*rates.CompletionNanoCNYPerToken
	}
	if len(events) == 0 {
		summary.PricingStatus = "unavailable"
	}
	summary.Models = sortedKeys(models)
	summary.Operations = sortedKeys(operations)
	pricingModels := sortedKeysFromPricing(pricingByModel)
	for _, model := range pricingModels {
		summary.Pricing = append(summary.Pricing, pricingByModel[model])
	}
	if summary.PricingStatus == "estimated" {
		summary.EstimatedCostNanoCNY = &totalCost
		display := fmt.Sprintf("%.6f", float64(totalCost)/1_000_000_000)
		summary.EstimatedCostCNY = &display
	}
	return summary
}

func deepSeekRates(baseURL string, model string, occurredAt time.Time, pricing DeepSeekPricing) (PricingBreakdown, bool) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(parsed.Hostname(), "api.deepseek.com") {
		return PricingBreakdown{}, false
	}
	beijingTime := occurredAt.In(chinaStandardTime)
	tier := PricingTierOffPeak
	hour := beijingTime.Hour()
	weekday := beijingTime.Weekday()
	if weekday >= time.Monday && weekday <= time.Friday && ((hour >= 9 && hour < 12) || (hour >= 14 && hour < 18)) {
		tier = PricingTierPeak
	}
	selectedRates := func(rates TieredRates) TokenRates {
		if tier == PricingTierPeak {
			return rates.Peak
		}
		return rates.OffPeak
	}
	breakdown := func(rates TokenRates) PricingBreakdown {
		return PricingBreakdown{
			Model: model, Snapshot: pricing.Snapshot, Tier: tier,
			CacheHitNanoCNYPerToken:   rates.CacheHitNanoCNYPerToken,
			CacheMissNanoCNYPerToken:  rates.CacheMissNanoCNYPerToken,
			CompletionNanoCNYPerToken: rates.CompletionNanoCNYPerToken,
		}
	}
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "deepseek-v4-flash", "deepseek-chat", "deepseek-reasoner":
		return breakdown(selectedRates(pricing.Flash)), true
	case "deepseek-v4-pro":
		return breakdown(selectedRates(pricing.Pro)), true
	default:
		return PricingBreakdown{}, false
	}
}

func sortedKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func sortedKeysFromPricing(values map[string]PricingBreakdown) []string {
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}
