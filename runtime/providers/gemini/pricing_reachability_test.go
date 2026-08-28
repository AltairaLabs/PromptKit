package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
)

// TestPricingReachableForServedModels pins the property the table exists for:
// a model the API actually serves must resolve to a price.
//
// This is not a formality. The 3.x rows were keyed "gemini-3-pro",
// "gemini-3-flash" and "gemini-3-flash-lite", which match NO released model —
// every real id carries a minor version. ResolveLLMPricing has no substring
// fallback by design, so all of them resolved to nothing and billed at ZERO
// with a warning that is easy to miss in a busy log.
//
// A table keyed on names nobody uses looks complete and reports nothing, which
// is why this asserts reachability by ID rather than that the map is non-empty.
func TestPricingReachableForServedModels(t *testing.T) {
	served := []string{
		// 3.x — the generation that was entirely unreachable.
		"gemini-3.7-flash",
		"gemini-3.6-flash",
		"gemini-3.5-flash",
		"gemini-3.5-flash-lite",
		"gemini-3.1-flash-lite",
		"gemini-3.1-pro-preview",
		"gemini-3-flash-preview",
		// Still served, still priced.
		"gemini-2.5-flash",
		"gemini-2.5-pro",
		"gemini-2.5-flash-lite",
	}
	for _, model := range served {
		t.Run(model, func(t *testing.T) {
			d := base.ResolveLLMPricing(nil, base.FlatPricing{}, geminiPricingTable, model)
			require.NotNilf(t, d, "%s resolves to no pricing, so every call bills $0", model)
			assert.NotEmptyf(t, d.Items, "%s resolved to an empty descriptor", model)
		})
	}
}

// TestFlashVersionsArePricedIndependently guards against the shortcut that
// caused the original gap: collapsing 3.x flash onto one shared rate.
//
// They differ by up to 2x — 3.7 and 3.6 are 0.75/3.75 introductory, 3.5 is
// 1.50/9.00 — so a single family descriptor cannot be right for all of them.
// Verified 2026-08-28 against ai.google.dev/gemini-api/docs/pricing.
func TestFlashVersionsArePricedIndependently(t *testing.T) {
	price := func(model string) float64 {
		d := base.ResolveLLMPricing(nil, base.FlatPricing{}, geminiPricingTable, model)
		require.NotNil(t, d, model)
		require.NotEmpty(t, d.Items, model)
		return d.Items[0].Rate
	}

	assert.NotEqual(t, price("gemini-3.5-flash"), price("gemini-3.7-flash"),
		"3.5 and 3.7 Flash bill at different rates; sharing one descriptor mis-bills one of them")
	assert.Equal(t, price("gemini-3.7-flash"), price("gemini-3.6-flash"),
		"3.6 and 3.7 Flash share the same introductory rate")
}
