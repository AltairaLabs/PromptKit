package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
)

func TestPriceUsage_ReasoningAdditivePricedAtOwnRate(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 0.001},
		{Unit: base.UnitOutputToken, Rate: 0.002},
		{Unit: base.UnitReasoningToken, Rate: 0.002},
	}}
	ci := base.PriceUsage(desc, "gemini", base.ProviderTypeInference,
		base.TokenUsage{Input: 100, Output: 50, Reasoning: 30}, nil, 0)
	// TotalCost = 100*.001 + 50*.002 + 30*.002 = 0.1 + 0.1 + 0.06 = 0.26
	assert.InDelta(t, 0.26, ci.TotalCost, 1e-9)
	// Directional fold: reasoning USD lands in OutputCostUSD.
	assert.InDelta(t, 0.16, ci.OutputCostUSD, 1e-9)
	assert.InDelta(t, 0.10, ci.InputCostUSD, 1e-9)
	// Invariant.
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
	// Reasoning token count NOT in OutputTokens headline; only in Quantities.
	assert.Equal(t, 50, ci.OutputTokens)
	assert.Equal(t, 30.0, ci.Quantities[base.UnitReasoningToken])
}

func TestPriceUsage_CacheWriteFoldsIntoInputCost(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 0.001},
		{Unit: base.UnitCacheWriteToken, Rate: 0.00125},
		{Unit: base.UnitCacheReadToken, Rate: 0.0001},
	}}
	ci := base.PriceUsage(desc, "claude", base.ProviderTypeInference,
		base.TokenUsage{Input: 100, CacheWrite: 200, CacheRead: 300}, nil, 0)
	assert.InDelta(t, 100*0.001+200*0.00125, ci.InputCostUSD, 1e-9)
	assert.InDelta(t, 300*0.0001, ci.CachedCostUSD, 1e-9)
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
	assert.Equal(t, 300, ci.CachedTokens)
}

func TestPriceUsage_NilDescriptorSilentZero(t *testing.T) {
	ci := base.PriceUsage(nil, "ollama", base.ProviderTypeInference,
		base.TokenUsage{Input: 100, Output: 50}, nil, 0)
	assert.Zero(t, ci.TotalCost)
	assert.Equal(t, 100, ci.InputTokens) // headline counts still populated
	assert.Equal(t, 50, ci.OutputTokens)
}

func TestPriceUsage_UnpricedNonzeroIsLoudButPartial(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}}
	ci := base.PriceUsage(desc, "gemini", base.ProviderTypeInference,
		base.TokenUsage{Input: 100, Reasoning: 30}, nil, 0) // reasoning unpriced
	assert.InDelta(t, 0.1, ci.TotalCost, 1e-9) // priced-partial, not $0, not fabricated
}
