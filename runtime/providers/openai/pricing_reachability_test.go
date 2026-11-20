package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// servedModelRates is the Standard-tier, short-context (<=272K input) list
// price per 1M tokens — input, cached input, output — that
// https://developers.openai.com/api/docs/pricing published on 2026-11-20 for
// every text model it lists. A model OpenAI serves must price through the
// public cost path, not fall through to $0.
var servedModelRates = map[string][3]float64{
	"gpt-6.2-astra": {8.00, 0.40, 40.00},
	"gpt-6.2-sol":   {2.00, 0.10, 10.00},
	"gpt-6-astra":   {10.00, 1.00, 50.00},
	"gpt-6.1-sol":   {2.00, 0.10, 10.00},
	"gpt-6.1-terra": {1.00, 0.05, 5.00},
	"gpt-6-luna":    {0.10, 0.01, 0.50},
	"gpt-6-sol":     {2.00, 0.20, 10.00},
	"gpt-5.6-sol":   {4.00, 0.40, 20.00},
	"gpt-5.6-terra": {2.00, 0.20, 12.00},
	"gpt-5.6-luna":  {0.20, 0.02, 1.20},
	"gpt-5.5":       {5.00, 0.50, 30.00},
	"gpt-5.5-pro":   {30.00, 30.00, 180.00}, // no cached-input discount
	"gpt-5.4":       {2.50, 0.25, 15.00},
	"gpt-5.4-mini":  {0.75, 0.075, 4.50},
	"gpt-5.4-nano":  {0.20, 0.02, 1.25},
	"gpt-5.4-pro":   {30.00, 30.00, 180.00}, // no cached-input discount
	"gpt-5.2":       {1.75, 0.175, 14.00},
	"gpt-5.2-pro":   {21.00, 21.00, 168.00}, // no cached-input discount
	"gpt-5.1":       {1.25, 0.125, 10.00},
	"gpt-5":         {1.25, 0.125, 10.00},
	"gpt-5-mini":    {0.25, 0.025, 2.00},
	"gpt-5-nano":    {0.05, 0.005, 0.40},
	"gpt-5-pro":     {15.00, 15.00, 120.00}, // no cached-input discount
	"gpt-4.1":       {2.00, 0.50, 8.00},
	"gpt-4.1-mini":  {0.40, 0.10, 1.60},
	"gpt-4o":        {2.50, 1.25, 10.00},
	"gpt-4o-mini":   {0.15, 0.075, 0.60},
	"o3-pro":        {20.00, 20.00, 80.00}, // no cached-input discount
	"o3":            {2.00, 0.50, 8.00},
}

// TestCost_ServedModelsPriceAtListRate drives each served model through the
// provider's cost path with 1M uncached input, 1M cached input and 1M output
// tokens, so each headline equals the per-1M list rate exactly.
func TestCost_ServedModelsPriceAtListRate(t *testing.T) {
	for model, r := range servedModelRates {
		t.Run(model, func(t *testing.T) {
			ci := newTestOpenAIProvider(t, model).costFromUsage(openAIUsage{
				PromptTokens:        2_000_000,
				CompletionTokens:    1_000_000,
				PromptTokensDetails: &openAIPromptDetails{CachedTokens: 1_000_000},
			})
			assert.InDelta(t, r[0], ci.InputCostUSD, 1e-9, "input")
			assert.InDelta(t, r[1], ci.CachedCostUSD, 1e-9, "cached input")
			assert.InDelta(t, r[2], ci.OutputCostUSD, 1e-9, "output")
			assert.InDelta(t, r[0]+r[1]+r[2], ci.TotalCost, 1e-9, "total")
		})
	}
}

// A dated snapshot the model page lists prices like its alias.
func TestCost_DatedSnapshotPricesLikeAlias(t *testing.T) {
	for snapshot, alias := range map[string]string{
		"gpt-5.5-2026-04-23": "gpt-5.5",
		"gpt-5.4-2026-03-05": "gpt-5.4",
		"gpt-5.2-2025-12-11": "gpt-5.2",
		"gpt-5-2025-08-07":   "gpt-5",
	} {
		ci := newTestOpenAIProvider(t, snapshot).costFromUsage(openAIUsage{
			PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
		})
		r := servedModelRates[alias]
		assert.InDeltaf(t, r[0]+r[2], ci.TotalCost, 1e-9, "snapshot %s", snapshot)
	}
}

// servedModelWindows is the input budget the compactor gets for each model:
// the page's stated max input where it states one (GPT-6.2: 922K of 1.05M),
// else the context window minus the 128K max output — the relation the stated
// figures follow (GPT-6.2: 1.05M - 128K = 922K; GPT-5: 400K - 128K = 272K).
// Windows and max output from https://developers.openai.com/api/docs/models/<id>,
// read 2026-11-20.
var servedModelWindows = map[string]int{
	"gpt-6.2-astra": 922_000,
	"gpt-6.2-sol":   922_000,
	"gpt-6-astra":   922_000,
	"gpt-6.1-sol":   922_000,
	"gpt-6.1-terra": 922_000,
	"gpt-6-luna":    922_000,
	"gpt-6-sol":     922_000,
	"gpt-5.6-sol":   922_000,
	"gpt-5.6-terra": 922_000,
	"gpt-5.6-luna":  922_000,
	"gpt-5.5":       922_000,
	"gpt-5.5-pro":   922_000,
	"gpt-5.4":       922_000,
	"gpt-5.4-pro":   922_000,
	"gpt-5.4-mini":  272_000,
	"gpt-5.4-nano":  272_000,
	"gpt-5.2":       272_000,
	"gpt-5.2-pro":   272_000,
	"gpt-5.1":       272_000,
	"gpt-5-pro":     272_000, // GPT-5's stated 272K max input; the page gives 272K max output
	"o3-pro":        200_000, // total window, like o3
}

func TestMaxContextTokens_ServedModels(t *testing.T) {
	for model, want := range servedModelWindows {
		if got := contextWindowOf(t, model, nil); got != want {
			t.Errorf("%s window = %d, want %d", model, got, want)
		}
	}
	if got := contextWindowOf(t, "gpt-5.5-2026-04-23", nil); got != 922_000 {
		t.Errorf("dated snapshot window = %d, want 922000", got)
	}
}
