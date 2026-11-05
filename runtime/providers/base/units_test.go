package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/stretchr/testify/assert"
)

func TestTokenUsage_Quantities_OnlyNonzero(t *testing.T) {
	u := base.TokenUsage{Input: 100, Output: 50, Reasoning: 20}
	q := u.Quantities()
	assert.Equal(t, map[string]float64{
		base.UnitInputToken:     100,
		base.UnitOutputToken:    50,
		base.UnitReasoningToken: 20,
	}, q)
}

func TestTokenUsage_Quantities_IncludesAllUnitsAndExtra(t *testing.T) {
	u := base.TokenUsage{
		Input: 1, CacheRead: 2, CacheWrite: 3, Output: 4, Reasoning: 5,
		AudioInput: 6, AudioOutput: 7, ImageInput: 9, VideoInput: 10, ImageOutput: 11,
		Extra: map[string]float64{"custom_token": 8},
	}
	q := u.Quantities()
	assert.Equal(t, 1.0, q[base.UnitInputToken])
	assert.Equal(t, 2.0, q[base.UnitCacheReadToken])
	assert.Equal(t, 3.0, q[base.UnitCacheWriteToken])
	assert.Equal(t, 4.0, q[base.UnitOutputToken])
	assert.Equal(t, 5.0, q[base.UnitReasoningToken])
	assert.Equal(t, 6.0, q[base.UnitAudioInputToken])
	assert.Equal(t, 7.0, q[base.UnitAudioOutputToken])
	assert.Equal(t, 8.0, q["custom_token"])
	assert.Equal(t, 9.0, q[base.UnitImageInputToken])
	assert.Equal(t, 10.0, q[base.UnitVideoInputToken])
	assert.Equal(t, 11.0, q[base.UnitImageOutputToken])
}

// TestPriceUsage_ImageAndVideoAreInputCost checks the headline split: image and
// video are input, so they must not fall into the output bucket the way an
// unknown unit does.
func TestPriceUsage_ImageAndVideoAreInputCost(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{
		{Unit: base.UnitImageInputToken, Rate: 0.001},
		{Unit: base.UnitVideoInputToken, Rate: 0.002},
	}}
	ci := base.PriceUsage(desc, "p", base.ProviderTypeInference, base.TokenUsage{ImageInput: 1000, VideoInput: 1000}, nil, 0)
	assert.InDelta(t, 3.0, ci.InputCostUSD, 1e-9)
	assert.Zero(t, ci.OutputCostUSD)
}
