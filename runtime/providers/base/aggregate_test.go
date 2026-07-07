package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestAggregateCost_MergesBreakdownAndReDerivesHeadlines(t *testing.T) {
	a := &types.CostInfo{
		Quantities: map[string]float64{base.UnitInputToken: 100, base.UnitOutputToken: 50},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: base.UnitInputToken, Quantity: 100, USD: 0.1},
			{Provider: "claude", Capability: "inference", Unit: base.UnitOutputToken, Quantity: 50, USD: 0.1},
		},
		InputCostUSD: 0.1, OutputCostUSD: 0.1, TotalCost: 0.2,
		InputTokens: 100, OutputTokens: 50,
	}
	b := &types.CostInfo{
		Quantities: map[string]float64{base.UnitInputToken: 10, base.UnitReasoningToken: 5},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: base.UnitInputToken, Quantity: 10, USD: 0.01},
			{Provider: "claude", Capability: "inference", Unit: base.UnitReasoningToken, Quantity: 5, USD: 0.01},
		},
	}
	got := base.AggregateCost(a, b)
	// input line merged: 110 tokens, 0.11 USD
	assert.Equal(t, 110.0, got.Quantities[base.UnitInputToken])
	assert.Equal(t, 5.0, got.Quantities[base.UnitReasoningToken])
	assert.InDelta(t, 0.11, got.InputCostUSD, 1e-9)  // input line
	assert.InDelta(t, 0.11, got.OutputCostUSD, 1e-9) // output 0.1 + reasoning 0.01
	assert.InDelta(t, 0.22, got.TotalCost, 1e-9)
	assert.InDelta(t, got.TotalCost, got.InputCostUSD+got.OutputCostUSD+got.CachedCostUSD, 1e-9)
	assert.Equal(t, 110, got.InputTokens)
}

func TestAggregateCost_SkipsNilAndDoesNotDropGranularData(t *testing.T) {
	// Regression guard: a message carrying Breakdown must not vanish from the roll-up.
	msg := &types.CostInfo{
		Quantities: map[string]float64{base.UnitCacheWriteToken: 40},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: base.UnitCacheWriteToken, Quantity: 40, USD: 0.05},
		},
	}
	got := base.AggregateCost(nil, msg, nil)
	assert.Equal(t, 40.0, got.Quantities[base.UnitCacheWriteToken])
	assert.InDelta(t, 0.05, got.InputCostUSD, 1e-9) // cache_write folds into input side
	assert.InDelta(t, 0.05, got.TotalCost, 1e-9)
}

func TestAggregateCost_HeadlineOnlyPartNotDropped(t *testing.T) {
	// Regression guard: a headline-only CostInfo (no Breakdown/Quantities,
	// e.g. a pre-migration provider result or an older/serialized message)
	// must not silently aggregate to $0.
	legacy := &types.CostInfo{
		InputTokens:   1000,
		OutputTokens:  500,
		CachedTokens:  100,
		InputCostUSD:  0.1,
		OutputCostUSD: 0.2,
		CachedCostUSD: 0.01,
		TotalCost:     0.31,
		ProviderName:  "legacy",
		Capability:    "inference",
	}
	got := base.AggregateCost(legacy)
	assert.InDelta(t, 0.31, got.TotalCost, 1e-9)
	assert.InDelta(t, 0.1, got.InputCostUSD, 1e-9)
	assert.InDelta(t, 0.2, got.OutputCostUSD, 1e-9)
	assert.InDelta(t, 0.01, got.CachedCostUSD, 1e-9)
	assert.InDelta(t, got.TotalCost, got.InputCostUSD+got.OutputCostUSD+got.CachedCostUSD, 1e-9)
	assert.Equal(t, 1000.0, got.Quantities[base.UnitInputToken])
}

func TestAggregateCost_MixedBreakdownAndHeadlineOnly(t *testing.T) {
	// One part carries a real Breakdown, the other is headline-only. Both
	// must contribute, with no double-counting.
	withBreakdown := &types.CostInfo{
		Quantities: map[string]float64{base.UnitInputToken: 100, base.UnitOutputToken: 50},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: base.UnitInputToken, Quantity: 100, USD: 0.1},
			{Provider: "claude", Capability: "inference", Unit: base.UnitOutputToken, Quantity: 50, USD: 0.1},
		},
		InputCostUSD: 0.1, OutputCostUSD: 0.1, TotalCost: 0.2,
		InputTokens: 100, OutputTokens: 50,
	}
	headlineOnly := &types.CostInfo{
		InputTokens:   200,
		OutputTokens:  100,
		InputCostUSD:  0.05,
		OutputCostUSD: 0.03,
		TotalCost:     0.08,
		ProviderName:  "legacy",
		Capability:    "inference",
	}
	got := base.AggregateCost(withBreakdown, headlineOnly)
	assert.InDelta(t, withBreakdown.TotalCost+headlineOnly.TotalCost, got.TotalCost, 1e-9)
	assert.Equal(t, 300.0, got.Quantities[base.UnitInputToken])
	assert.Equal(t, 150.0, got.Quantities[base.UnitOutputToken])
	assert.InDelta(t, got.TotalCost, got.InputCostUSD+got.OutputCostUSD+got.CachedCostUSD, 1e-9)
}
