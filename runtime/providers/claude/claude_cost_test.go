package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// newTestClaudeProvider builds a bare Provider with only the model set, so
// costFromUsage resolves pricing purely from claudePricingTable (no config
// descriptor, no legacy flat Pricing).
func newTestClaudeProvider(t *testing.T, model string) *Provider {
	t.Helper()
	return NewProvider("test-claude", model, "https://api.anthropic.com", providers.ProviderDefaults{}, false)
}

// TestClaudeCost_PricesCacheWrite is the regression test for finding G: Claude
// reports cache_creation_input_tokens (cache WRITE, ~1.25x input) on the wire,
// but the old CalculateCost path only ever accepted a single "cachedTokens"
// (cache READ) argument, so every cache-write token was silently dropped and
// cost was understated. costFromUsage must price it.
func TestClaudeCost_PricesCacheWrite(t *testing.T) {
	p := newTestClaudeProvider(t, "claude-sonnet-4-5")
	u := claudeUsage{
		InputTokens: 1000, OutputTokens: 500,
		CacheCreationInputTokens: 200, CacheReadInputTokens: 300,
	}
	ci := p.costFromUsage(u)
	assert.Greater(t, ci.Quantities[base.UnitCacheWriteToken], 0.0)
	assert.Greater(t, ci.InputCostUSD, 0.0)

	// Cache-write must contribute cost (previously dropped -> understated).
	uNoWrite := claudeUsage{InputTokens: 1000, OutputTokens: 500, CacheReadInputTokens: 300}
	assert.Greater(t, ci.TotalCost, p.costFromUsage(uNoWrite).TotalCost)

	// TotalCost stays the sum of the flat headline buckets.
	assert.InDelta(t, ci.TotalCost, ci.InputCostUSD+ci.OutputCostUSD+ci.CachedCostUSD, 1e-9)
}

// TestClaudeUsageToTokens_MapsAllFields verifies the field-by-field mapping
// from the wire claudeUsage shape into canonical base.TokenUsage units,
// including that InputTokens is used as-is (Anthropic's input_tokens is
// already cache-EXCLUSIVE — never subtract cache reads/writes from it).
func TestClaudeUsageToTokens_MapsAllFields(t *testing.T) {
	u := claudeUsage{
		InputTokens: 1000, OutputTokens: 500,
		CacheCreationInputTokens: 200, CacheReadInputTokens: 300,
	}
	got := claudeUsageToTokens(u)
	assert.Equal(t, base.TokenUsage{Input: 1000, CacheRead: 300, CacheWrite: 200, Output: 500}, got)
}

// TestClaudeCost_KnownModelPricesCacheReadAndWrite pins the actual USD amounts
// for a known table entry so a future edit to the multipliers or table values
// is caught, not just "greater than zero".
func TestClaudeCost_KnownModelPricesCacheReadAndWrite(t *testing.T) {
	p := newTestClaudeProvider(t, "claude-sonnet-4-5")
	ci := p.costFromUsage(claudeUsage{
		InputTokens: 1_000_000, OutputTokens: 1_000_000,
		CacheReadInputTokens: 1_000_000, CacheCreationInputTokens: 1_000_000,
	})

	// claude-sonnet-4-5: $3/$15 per 1M; cache_read = 0.1x input ($0.3/M),
	// cache_write = 1.25x input ($3.75/M). InputCostUSD folds in input + cache_write.
	assert.InDelta(t, 1_000_000.0, ci.Quantities[base.UnitInputToken], 1e-9)
	assert.InDelta(t, 3.0+3.75, ci.InputCostUSD, 1e-9)
	assert.InDelta(t, 0.3, ci.CachedCostUSD, 1e-9)
	assert.InDelta(t, 15.0, ci.OutputCostUSD, 1e-9)
}

// TestClaudeCost_UnknownModelNoWrongConstant matches the sibling providers'
// (e.g. Gemini) fix for the same class of bug: an unmatched model must price
// as $0 (surfaced via the loud-unpriced-unit warning path), never silently
// fall back to a guessed constant like "assume Sonnet pricing".
func TestClaudeCost_UnknownModelNoWrongConstant(t *testing.T) {
	p := newTestClaudeProvider(t, "claude-9-9-imaginary")
	ci := p.costFromUsage(claudeUsage{InputTokens: 1000, OutputTokens: 500})
	assert.Zero(t, ci.TotalCost)
}

// TestClaudeCost_CalculateCostWrapperMatchesCostFromUsage verifies the public
// CalculateCost(tokensIn, tokensOut, cachedTokens) signature (kept for the
// Provider interface contract) is a pure wrapper over costFromUsage, treating
// cachedTokens as cache READS only (it has no cache-write parameter).
func TestClaudeCost_CalculateCostWrapperMatchesCostFromUsage(t *testing.T) {
	p := newTestClaudeProvider(t, "claude-sonnet-4-5")
	want := p.costFromUsage(claudeUsage{InputTokens: 1000, OutputTokens: 500, CacheReadInputTokens: 200})
	got := p.CalculateCost(1000, 500, 200)

	// Compare the priced fields directly rather than the whole struct: Breakdown
	// row order comes from a map iteration (computeCostPartial ranges over
	// Quantities) and is not guaranteed stable across two independent calls.
	assert.Equal(t, want.TotalCost, got.TotalCost)
	assert.Equal(t, want.InputCostUSD, got.InputCostUSD)
	assert.Equal(t, want.OutputCostUSD, got.OutputCostUSD)
	assert.Equal(t, want.CachedCostUSD, got.CachedCostUSD)
	assert.Equal(t, want.InputTokens, got.InputTokens)
	assert.Equal(t, want.OutputTokens, got.OutputTokens)
	assert.Equal(t, want.CachedTokens, got.CachedTokens)
	assert.Equal(t, want.Quantities, got.Quantities)
}
