package base_test

import (
	"bytes"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rateOf(d *base.PricingDescriptor, unit string) float64 {
	for _, it := range d.Items {
		if it.Unit == unit {
			return it.Rate
		}
	}
	return -1
}

func TestResolveLLMPricing_Order(t *testing.T) {
	cfg := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.009}}}
	flat := base.FlatPricing{Input: 0.001, Output: 0.002} // per-1K
	table := map[string]*base.PricingDescriptor{
		"claude-sonnet-4-5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
	}

	// explicit config wins
	assert.Equal(t, 0.009, base.ResolveLLMPricing(cfg, flat, table, "claude-sonnet-4-5").Items[0].Rate)

	// flat config maps to per-unit (0.002/1000 output) when no descriptor
	got := base.ResolveLLMPricing(nil, flat, table, "claude-sonnet-4-5")
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitInputToken), 1e-12)
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitOutputToken), 1e-12)
	// flat config also prices reasoning at the output rate, so a thinking model
	// under flat config does not silently understate cost by the reasoning amount.
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitReasoningToken), 1e-12)
	// flat config is one input and one output rate for every modality, so a
	// provider that splits audio, image or video out still prices them.
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitAudioInputToken), 1e-12)
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitImageInputToken), 1e-12)
	assert.InDelta(t, 0.001/1000, rateOf(got, base.UnitVideoInputToken), 1e-12)
	assert.InDelta(t, 0.002/1000, rateOf(got, base.UnitAudioOutputToken), 1e-12)

	// no config: table by model
	got2 := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got2, base.UnitInputToken))

	// unknown model with a table: an empty descriptor, so PriceUsage warns
	unknown := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "made-up-model")
	require.NotNil(t, unknown)
	assert.Empty(t, unknown.Items)

	// no table and no config (a self-hosted server): nil, priced quietly at $0
	assert.Nil(t, base.ResolveLLMPricing(nil, base.FlatPricing{}, nil, "any-model"))
}

// TestResolveLLMPricing_UnknownModelWarns checks the property the empty
// descriptor exists for: a paid provider's new model is reported, not billed
// as a silent $0.
func TestResolveLLMPricing_UnknownModelWarns(t *testing.T) {
	base.ResetUnpricedWarningsForTest() // once-per-process warning; keep -count>1 repeatable
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	defer logger.SetOutput(nil)

	table := map[string]*base.PricingDescriptor{
		"known-model": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}},
	}
	desc := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "unknown-model-warn-test")
	ci := base.PriceUsage(desc, "unknown-model-warn-test-provider", base.ProviderTypeInference,
		base.TokenUsage{Input: 100}, nil, 0)

	assert.Zero(t, ci.TotalCost)
	assert.Contains(t, buf.String(), "nonzero token unit has no pricing")
}

func TestResolveLLMPricing_NormalizeModelStripsVendorPrefix(t *testing.T) {
	table := map[string]*base.PricingDescriptor{
		"claude-sonnet-4-5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
	}
	got := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "anthropic/claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got, base.UnitInputToken))
}
