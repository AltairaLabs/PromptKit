package base_test

import (
	"bytes"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestPriceUsage_ExtraPricedUnitPreservesInvariant asserts that a priced unit
// arriving via TokenUsage.Extra (not one of the 7 canonical units) still
// folds into a headline bucket, so TotalCost == In+Out+Cached always holds
// even for custom/future units.
func TestPriceUsage_ExtraPricedUnitPreservesInvariant(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: 0.001},
		{Unit: "custom_token", Rate: 0.001},
	}}
	ci := base.PriceUsage(desc, "acme", base.ProviderTypeInference,
		base.TokenUsage{Input: 100, Extra: map[string]float64{"custom_token": 50}}, nil, 0)
	// TotalCost = 100*.001 + 50*.001 = 0.1 + 0.05 = 0.15
	assert.InDelta(t, 0.15, ci.TotalCost, 1e-9)
	// The custom unit's USD must be included somewhere in the headline split.
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
	assert.InDelta(t, 0.05, ci.OutputCostUSD, 1e-9) // custom unit folds into OutputCostUSD
	assert.InDelta(t, 0.1, ci.InputCostUSD, 1e-9)
}

// TestPriceUsage_LoudUnpricedWarns asserts the unpriced-unit warning actually
// reaches the logger for a non-nil descriptor, and stays silent for a nil
// descriptor. Uses a provider/unit combination unique to this test since
// unpricedWarnOnce dedupes per-process by provider|capability|unit.
func TestPriceUsage_LoudUnpricedWarns(t *testing.T) {
	t.Run("nonzero unpriced unit with non-nil descriptor warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger.SetOutput(&buf)
		defer logger.SetOutput(nil)

		desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}}
		_ = base.PriceUsage(desc, "loud-warn-test-provider", base.ProviderTypeInference,
			base.TokenUsage{Input: 100, Extra: map[string]float64{"loud_warn_test_unit": 7}}, nil, 0)

		require.Contains(t, buf.String(), "loud_warn_test_unit")
		assert.Contains(t, buf.String(), "nonzero token unit has no pricing")
	})

	t.Run("nil descriptor stays silent", func(t *testing.T) {
		var buf bytes.Buffer
		logger.SetOutput(&buf)
		defer logger.SetOutput(nil)

		_ = base.PriceUsage(nil, "loud-warn-test-provider-nil", base.ProviderTypeInference,
			base.TokenUsage{Input: 100, Extra: map[string]float64{"loud_warn_test_unit_nil": 7}}, nil, 0)

		assert.Empty(t, buf.String())
	})
}
