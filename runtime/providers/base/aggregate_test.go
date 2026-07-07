package base_test

import (
	"testing"

	. "github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestAggregateCost_MergesBreakdownAndReDerivesHeadlines(t *testing.T) {
	a := &types.CostInfo{
		Quantities: map[string]float64{UnitInputToken: 100, UnitOutputToken: 50},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: UnitInputToken, Quantity: 100, USD: 0.1},
			{Provider: "claude", Capability: "inference", Unit: UnitOutputToken, Quantity: 50, USD: 0.1},
		},
		InputCostUSD: 0.1, OutputCostUSD: 0.1, TotalCost: 0.2,
		InputTokens: 100, OutputTokens: 50,
	}
	b := &types.CostInfo{
		Quantities: map[string]float64{UnitInputToken: 10, UnitReasoningToken: 5},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: UnitInputToken, Quantity: 10, USD: 0.01},
			{Provider: "claude", Capability: "inference", Unit: UnitReasoningToken, Quantity: 5, USD: 0.01},
		},
	}
	got := AggregateCost(a, b)
	// input line merged: 110 tokens, 0.11 USD
	assert.Equal(t, 110.0, got.Quantities[UnitInputToken])
	assert.Equal(t, 5.0, got.Quantities[UnitReasoningToken])
	assert.InDelta(t, 0.11, got.InputCostUSD, 1e-9)  // input line
	assert.InDelta(t, 0.11, got.OutputCostUSD, 1e-9) // output 0.1 + reasoning 0.01
	assert.InDelta(t, 0.22, got.TotalCost, 1e-9)
	assert.InDelta(t, got.TotalCost, got.InputCostUSD+got.OutputCostUSD+got.CachedCostUSD, 1e-9)
	assert.Equal(t, 110, got.InputTokens)
}

func TestAggregateCost_SkipsNilAndDoesNotDropGranularData(t *testing.T) {
	// Regression guard: a message carrying Breakdown must not vanish from the roll-up.
	msg := &types.CostInfo{
		Quantities: map[string]float64{UnitCacheWriteToken: 40},
		Breakdown: []types.CostLineItem{
			{Provider: "claude", Capability: "inference", Unit: UnitCacheWriteToken, Quantity: 40, USD: 0.05},
		},
	}
	got := AggregateCost(nil, msg, nil)
	assert.Equal(t, 40.0, got.Quantities[UnitCacheWriteToken])
	assert.InDelta(t, 0.05, got.InputCostUSD, 1e-9) // cache_write folds into input side
	assert.InDelta(t, 0.05, got.TotalCost, 1e-9)
}
