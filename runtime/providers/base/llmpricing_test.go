package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
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

	// no config: table by model
	got2 := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got2, base.UnitInputToken))

	// unknown model, no config, no flat: nil
	assert.Nil(t, base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "made-up-model"))
}

func TestResolveLLMPricing_NormalizeModelStripsVendorPrefix(t *testing.T) {
	table := map[string]*base.PricingDescriptor{
		"claude-sonnet-4-5": {Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.003}}},
	}
	got := base.ResolveLLMPricing(nil, base.FlatPricing{}, table, "anthropic/claude-sonnet-4-5")
	assert.Equal(t, 0.003, rateOf(got, base.UnitInputToken))
}
