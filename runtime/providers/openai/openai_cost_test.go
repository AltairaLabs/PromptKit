package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// newTestOpenAIProvider builds a bare Provider with only the model set, so
// costFromUsage resolves pricing purely from openaiPricingTable (no config
// descriptor, no legacy flat Pricing).
func newTestOpenAIProvider(t *testing.T, model string) *Provider {
	t.Helper()
	return NewProvider("test-openai", model, "https://api.openai.com/v1", providers.ProviderDefaults{}, false)
}

// TestOpenAICost_ReasoningSplitOutOfOutput is the regression test for the
// reasoning-token normalization: OpenAI reports completion_tokens INCLUSIVE
// of reasoning_tokens, so the old bespoke CalculateCost (which only ever
// took a flat tokensOut count) billed reasoning tokens as ordinary visible
// output and never surfaced them as their own quantity. costFromUsage must
// split reasoning out of the output headline while still pricing it (at the
// output rate) so the total cost is unchanged but the breakdown is honest.
func TestOpenAICost_ReasoningSplitOutOfOutput(t *testing.T) {
	p := newTestOpenAIProvider(t, "o4-mini")
	u := openAIUsage{PromptTokens: 1000, CompletionTokens: 800} // completion INCLUDES reasoning
	u.CompletionTokensDetails = &openAICompletionDetails{ReasoningTokens: 300}
	ci := p.costFromUsage(u)

	// Output headline is visible-only: 800 - 300 = 500.
	assert.Equal(t, 500, ci.OutputTokens)
	assert.Equal(t, 300.0, ci.Quantities[base.UnitReasoningToken])

	// Cost invariant holds; total equals output+reasoning at output rate + input.
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
}

// TestOpenAIUsageToTokens_MapsAllFields verifies the field-by-field mapping
// from the wire openAIUsage shape into canonical base.TokenUsage units,
// including that prompt_tokens is cache-INCLUSIVE (cached must be
// subtracted out) and completion_tokens is reasoning-INCLUSIVE (reasoning
// must be subtracted out of the visible output count).
func TestOpenAIUsageToTokens_MapsAllFields(t *testing.T) {
	u := openAIUsage{
		PromptTokens:            1000,
		CompletionTokens:        800,
		PromptTokensDetails:     &openAIPromptDetails{CachedTokens: 300},
		CompletionTokensDetails: &openAICompletionDetails{ReasoningTokens: 200},
	}
	got := openAIUsageToTokens(u)
	assert.Equal(t, base.TokenUsage{Input: 700, CacheRead: 300, Output: 600, Reasoning: 200}, got)
}

// TestOpenAIUsageToTokens_NilDetailsDefaultToZero verifies that a response
// without prompt_tokens_details/completion_tokens_details (older models,
// mock providers) treats prompt_tokens/completion_tokens as already
// cache-exclusive/reasoning-exclusive rather than panicking on the nil
// pointers.
func TestOpenAIUsageToTokens_NilDetailsDefaultToZero(t *testing.T) {
	got := openAIUsageToTokens(openAIUsage{PromptTokens: 1000, CompletionTokens: 500})
	assert.Equal(t, base.TokenUsage{Input: 1000, Output: 500}, got)
}

// TestOpenAICost_KnownModelPricesCacheAndReasoning pins the actual USD
// amounts for a known table entry so a future edit to the table is caught,
// not just "greater than zero".
func TestOpenAICost_KnownModelPricesCacheAndReasoning(t *testing.T) {
	p := newTestOpenAIProvider(t, "gpt-4o")
	ci := p.costFromUsage(openAIUsage{
		PromptTokens:            2_000_000,
		CompletionTokens:        1_000_000,
		PromptTokensDetails:     &openAIPromptDetails{CachedTokens: 1_000_000},
		CompletionTokensDetails: &openAICompletionDetails{ReasoningTokens: 500_000},
	})

	// gpt-4o: $2.50/$10.00 per 1M; cache_read = 50% of input ($1.25/M).
	// InputCostUSD folds in input only (no cache_write unit for OpenAI).
	assert.InDelta(t, 1_000_000.0, ci.Quantities[base.UnitInputToken], 1e-9)
	assert.InDelta(t, 2.50, ci.InputCostUSD, 1e-9)
	assert.InDelta(t, 1.25, ci.CachedCostUSD, 1e-9)
	// Output cost folds in visible output (500K @ $10/M = $5) + reasoning
	// (500K @ $10/M, same rate = $5) = $10.
	assert.InDelta(t, 10.0, ci.OutputCostUSD, 1e-9)
}

// TestOpenAICost_UnknownModelNoWrongConstant matches the sibling providers'
// (e.g. Claude, Gemini) fix for the same class of bug: an unmatched model
// must price as $0 (surfaced via the loud-unpriced-unit warning path),
// never silently fall back to a guessed constant like "assume GPT-4o pricing".
func TestOpenAICost_UnknownModelNoWrongConstant(t *testing.T) {
	p := newTestOpenAIProvider(t, "llama-3.1-70b-instruct")
	ci := p.costFromUsage(openAIUsage{PromptTokens: 1000, CompletionTokens: 500})
	assert.Zero(t, ci.TotalCost)
}

// TestOpenAICost_CalculateCostWrapperMatchesCostFromUsage verifies the
// public CalculateCost(tokensIn, tokensOut, cachedTokens) signature (kept
// for the Provider interface contract) is a pure wrapper over costFromUsage,
// treating cachedTokens as cache reads only (it has no reasoning parameter).
func TestOpenAICost_CalculateCostWrapperMatchesCostFromUsage(t *testing.T) {
	p := newTestOpenAIProvider(t, "gpt-4o")
	want := p.costFromUsage(openAIUsage{
		PromptTokens: 1000, CompletionTokens: 500,
		PromptTokensDetails: &openAIPromptDetails{CachedTokens: 200},
	})
	got := p.CalculateCost(1000, 500, 200)

	// Compare the priced fields directly rather than the whole struct:
	// Breakdown row order comes from a map iteration (computeCostPartial
	// ranges over Quantities) and is not guaranteed stable across two
	// independent calls.
	assert.Equal(t, want.TotalCost, got.TotalCost)
	assert.Equal(t, want.InputCostUSD, got.InputCostUSD)
	assert.Equal(t, want.OutputCostUSD, got.OutputCostUSD)
	assert.Equal(t, want.CachedCostUSD, got.CachedCostUSD)
	assert.Equal(t, want.InputTokens, got.InputTokens)
	assert.Equal(t, want.OutputTokens, got.OutputTokens)
	assert.Equal(t, want.CachedTokens, got.CachedTokens)
	assert.Equal(t, want.Quantities, got.Quantities)
}

// TestOpenAICost_ResponsesUsageMapsCachedTokens verifies
// responsesUsageToOpenAIUsage carries InputTokensCached through to the
// cache_read unit for the Responses API cost path, and that a nil usage
// prices as zero rather than panicking.
func TestOpenAICost_ResponsesUsageMapsCachedTokens(t *testing.T) {
	p := newTestOpenAIProvider(t, "gpt-4o")
	u := &responsesUsage{InputTokens: 1000, OutputTokens: 500, InputTokensCached: 200}
	ci := p.costFromUsage(responsesUsageToOpenAIUsage(u))

	assert.Equal(t, 800, ci.InputTokens)
	assert.Equal(t, 200, ci.CachedTokens)
	assert.Equal(t, 500, ci.OutputTokens)

	zero := p.costFromUsage(responsesUsageToOpenAIUsage(nil))
	assert.Zero(t, zero.TotalCost)
}
