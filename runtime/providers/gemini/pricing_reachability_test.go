package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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
		"gemini-3.8-flash",
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
		// Live API models, priced through the same table since the session
		// stopped using its own heuristic.
		"gemini-3.8-live",
		"gemini-3.8-live-extended-thinking",
		"gemini-3.1-flash-live-preview",
		"gemini-2.5-flash-native-audio-preview-12-2025",
		// Image-generation models.
		"gemini-nano-banana-2.1",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-lite-image",
		"gemini-3-pro-image",
		"gemini-2.5-flash-image",
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

// rateFor returns a descriptor's per-1M-token rate for one unit, or -1 when the
// unit is unpriced.
func rateFor(t *testing.T, model, unit string) float64 {
	t.Helper()
	d := base.ResolveLLMPricing(nil, base.FlatPricing{}, geminiPricingTable, model)
	require.NotNil(t, d, model)
	for _, it := range d.Items {
		if it.Unit == unit && len(it.Dimensions) == 0 {
			return it.Rate * perMillionScale
		}
	}
	return -1
}

// TestCacheReadsBillAtTenPercentOfInput pins the cache rate every model on the
// pricing page uses. The table priced cache hits at 50%, overstating every
// cached token fivefold, and nothing failed.
func TestCacheReadsBillAtTenPercentOfInput(t *testing.T) {
	for model := range geminiPricingTable {
		in, cached := rateFor(t, model, base.UnitInputToken), rateFor(t, model, base.UnitCacheReadToken)
		assert.InDeltaf(t, in*0.1, cached, 1e-9, "%s: cache read %.4f, input %.4f", model, cached, in)
	}
}

// TestModalityRates pins the input and output modalities that bill apart from
// text, checked against aistudio.google.com/docs/pricing on 2026-11-04.
func TestModalityRates(t *testing.T) {
	cases := []struct {
		model, unit string
		want        float64
	}{
		{"gemini-2.5-flash", base.UnitInputToken, 0.30},
		{"gemini-2.5-flash", base.UnitAudioInputToken, 1.00},
		{"gemini-2.5-flash", base.UnitImageInputToken, 0.30},
		{"gemini-2.5-flash-lite", base.UnitAudioInputToken, 0.30},
		{"gemini-3-flash-preview", base.UnitAudioInputToken, 1.00},
		{"gemini-3.1-flash-lite", base.UnitAudioInputToken, 0.50},
		{"gemini-3.8-flash", base.UnitAudioInputToken, 0.75},
		{"gemini-3.8-live", base.UnitInputToken, 0.75},
		{"gemini-3.8-live", base.UnitAudioInputToken, 3.00},
		{"gemini-3.8-live", base.UnitVideoInputToken, 1.00},
		{"gemini-3.8-live", base.UnitOutputToken, 4.50},
		{"gemini-3.8-live", base.UnitAudioOutputToken, 12.00},
		{"gemini-2.5-flash-native-audio-preview-12-2025", base.UnitAudioInputToken, 3.00},
		{"gemini-2.5-flash-native-audio-preview-12-2025", base.UnitImageInputToken, 3.00},
		{"gemini-2.5-flash-native-audio-preview-12-2025", base.UnitAudioOutputToken, 12.00},
		// Generated images bill far above text output.
		{"gemini-nano-banana-2.1", base.UnitOutputToken, 7.50},
		{"gemini-nano-banana-2.1", base.UnitImageOutputToken, 30.00},
		{"gemini-3.1-flash-image", base.UnitInputToken, 0.50},
		{"gemini-3.1-flash-image", base.UnitImageOutputToken, 60.00},
		{"gemini-3.1-flash-lite-image", base.UnitImageOutputToken, 30.00},
		{"gemini-3-pro-image", base.UnitOutputToken, 12.00},
		{"gemini-3-pro-image", base.UnitImageOutputToken, 120.00},
		{"gemini-2.5-flash-image", base.UnitOutputToken, 2.50},
		{"gemini-2.5-flash-image", base.UnitImageOutputToken, 30.00},
		{"gemini-2.5-flash-image", base.UnitReasoningToken, -1},
		// A text model does not price image out.
		{"gemini-3.8-flash", base.UnitImageOutputToken, -1},
		// A text-only model does not price audio out, so stray audio is warned
		// about instead of billed at a guess.
		{"gemini-3.8-flash", base.UnitAudioOutputToken, -1},
	}
	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.unit, func(t *testing.T) {
			assert.InDelta(t, tc.want, rateFor(t, tc.model, tc.unit), 1e-9)
		})
	}
}
