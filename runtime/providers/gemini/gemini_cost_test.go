package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// newTestGeminiProvider builds a bare Provider with only the model set, so
// costFromUsage resolves pricing purely from geminiPricingTable (no config
// descriptor, no legacy flat Pricing).
func newTestGeminiProvider(t *testing.T, model string) *Provider {
	t.Helper()
	return NewProvider("test-gemini", model, "https://generativelanguage.googleapis.com/v1beta", providers.ProviderDefaults{}, false)
}

// TestGeminiCost_AddsThinkingTokens is the regression test for finding F:
// Gemini's usageMetadata reports thoughtsTokenCount (Gemini 2.5+ "thinking"
// models) separately from candidatesTokenCount, billed ON TOP of it at the
// output rate. The old CalculateCost path never read thoughtsTokenCount at
// all, so every thinking token was silently free. costFromUsage must price it.
func TestGeminiCost_AddsThinkingTokens(t *testing.T) {
	p := newTestGeminiProvider(t, "gemini-2.5-pro")
	base_ := geminiUsage{PromptTokenCount: 1000, CandidatesTokenCount: 500, CachedContentTokenCount: 200}
	withThinking := base_
	withThinking.ThoughtsTokenCount = 300

	ci := p.costFromUsage(withThinking)
	assert.Equal(t, 300.0, ci.Quantities[base.UnitReasoningToken])
	assert.Greater(t, ci.TotalCost, p.costFromUsage(base_).TotalCost) // thinking billed on top

	// input is cache-exclusive: prompt - cached
	assert.Equal(t, 800, ci.InputTokens)
}

// TestGeminiCost_UnknownModelNoWrongConstant is the regression test for
// finding K: an unmatched model must price as $0 (surfaced via the loud
// unpriced-unit warning path), never silently fall back to a guessed constant
// like "assume Gemini 1.5 Pro pricing" (the old geminiPricing() default arm).
func TestGeminiCost_UnknownModelNoWrongConstant(t *testing.T) {
	p := newTestGeminiProvider(t, "gemini-9.9-imaginary")
	ci := p.costFromUsage(geminiUsage{PromptTokenCount: 1000, CandidatesTokenCount: 500})
	assert.Zero(t, ci.TotalCost) // loud warn + $0, NOT gemini-1.5-pro constants
}

// TestGeminiUsageToTokens_MapsAllFields verifies the field-by-field mapping
// from the wire geminiUsage shape into canonical base.TokenUsage units,
// including that PromptTokenCount is cache-INCLUSIVE (must subtract
// CachedContentTokenCount to get the canonical, full-price Input unit).
func TestGeminiUsageToTokens_MapsAllFields(t *testing.T) {
	u := geminiUsage{
		PromptTokenCount: 1000, CandidatesTokenCount: 500,
		CachedContentTokenCount: 200, ThoughtsTokenCount: 300,
	}
	got := geminiUsageToTokens(u)
	assert.Equal(t, base.TokenUsage{Input: 800, CacheRead: 200, Output: 500, Reasoning: 300}, got)
}

// TestGeminiCost_KnownModelPricesThinking pins the actual USD amounts for a
// known table entry so a future edit to the thinking-rate/table values is
// caught, not just "greater than zero".
func TestGeminiCost_KnownModelPricesThinking(t *testing.T) {
	p := newTestGeminiProvider(t, "gemini-2.5-pro")
	ci := p.costFromUsage(geminiUsage{
		PromptTokenCount: 1_000_000, CandidatesTokenCount: 1_000_000, ThoughtsTokenCount: 1_000_000,
	})

	// gemini-2.5-pro: $1.25/$10.00 per 1M; thinking priced at the output rate
	// ($10/M). OutputCostUSD folds in output + reasoning.
	assert.InDelta(t, 1_000_000.0, ci.Quantities[base.UnitInputToken], 1e-9)
	assert.InDelta(t, 1.25, ci.InputCostUSD, 1e-9)
	assert.InDelta(t, 10.0+10.0, ci.OutputCostUSD, 1e-9)
}

// TestGeminiCost_CalculateCostWrapperMatchesCostFromUsage verifies the public
// CalculateCost(tokensIn, tokensOut, cachedTokens) signature (kept for the
// Provider interface contract) is a pure wrapper over costFromUsage. Gemini's
// legacy signature treats tokensIn as cache-INCLUSIVE (matching promptTokenCount
// on the wire), so it maps straight through to geminiUsage.PromptTokenCount.
func TestGeminiCost_CalculateCostWrapperMatchesCostFromUsage(t *testing.T) {
	p := newTestGeminiProvider(t, "gemini-2.5-flash")
	want := p.costFromUsage(geminiUsage{
		PromptTokenCount: 1000, CandidatesTokenCount: 500, CachedContentTokenCount: 200,
	})
	got := p.CalculateCost(1000, 500, 200)

	// Compare the priced fields directly rather than the whole struct: Breakdown
	// row order comes from a map iteration and is not guaranteed stable across
	// two independent calls.
	assert.Equal(t, want.TotalCost, got.TotalCost)
	assert.Equal(t, want.InputCostUSD, got.InputCostUSD)
	assert.Equal(t, want.OutputCostUSD, got.OutputCostUSD)
	assert.Equal(t, want.CachedCostUSD, got.CachedCostUSD)
	assert.Equal(t, want.InputTokens, got.InputTokens)
	assert.Equal(t, want.OutputTokens, got.OutputTokens)
	assert.Equal(t, want.CachedTokens, got.CachedTokens)
	assert.Equal(t, want.Quantities, got.Quantities)
}
