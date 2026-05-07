package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeCost_ChatTokens(t *testing.T) {
	desc := &base.PricingDescriptor{
		Currency: "usd",
		Items: []base.PriceItem{
			{Unit: "input_token", Rate: 0.0000025},
			{Unit: "output_token", Rate: 0.00001},
		},
	}
	info := &types.CostInfo{
		Quantities: map[string]float64{"input_token": 1000, "output_token": 500},
	}
	total, breakdown, err := base.ComputeCost(desc, info)
	require.NoError(t, err)
	assert.InDelta(t, 0.0025+0.005, total, 1e-9)
	assert.Len(t, breakdown, 2)
}

func TestComputeCost_TTSCharacters(t *testing.T) {
	desc := &base.PricingDescriptor{
		Items: []base.PriceItem{{Unit: "character", Rate: 0.000015}},
	}
	info := &types.CostInfo{Quantities: map[string]float64{"character": 1240}}
	total, _, err := base.ComputeCost(desc, info)
	require.NoError(t, err)
	assert.InDelta(t, 0.000015*1240, total, 1e-12)
}

func TestComputeCost_ImageWithDimensions_MostSpecificWins(t *testing.T) {
	desc := &base.PricingDescriptor{
		Items: []base.PriceItem{
			{Unit: "image", Rate: 0.04, Dimensions: map[string]string{"size": "1024x1024", "quality": "standard"}},
			{Unit: "image", Rate: 0.08, Dimensions: map[string]string{"size": "1024x1024", "quality": "hd"}},
		},
	}
	info := &types.CostInfo{
		Quantities:     map[string]float64{"image": 1},
		DimensionMatch: map[string]string{"size": "1024x1024", "quality": "hd"},
	}
	total, breakdown, err := base.ComputeCost(desc, info)
	require.NoError(t, err)
	assert.InDelta(t, 0.08, total, 1e-9)
	require.Len(t, breakdown, 1)
	assert.Equal(t, "hd", breakdown[0].Dimensions["quality"])
}

func TestComputeCost_NoMatchingDimensions_ReturnsError(t *testing.T) {
	desc := &base.PricingDescriptor{
		Items: []base.PriceItem{
			{Unit: "image", Rate: 0.04, Dimensions: map[string]string{"size": "512x512"}},
		},
	}
	info := &types.CostInfo{
		Quantities:     map[string]float64{"image": 1},
		DimensionMatch: map[string]string{"size": "1024x1024"},
	}
	_, _, err := base.ComputeCost(desc, info)
	assert.Error(t, err)
}

func TestComputeCost_NilPricing_ReturnsZero(t *testing.T) {
	info := &types.CostInfo{Quantities: map[string]float64{"image": 1}}
	total, breakdown, err := base.ComputeCost(nil, info)
	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
	assert.Empty(t, breakdown)
}

func TestComputeCost_EmptyQuantities_ReturnsZero(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "input_token", Rate: 0.001}}}
	info := &types.CostInfo{}
	total, breakdown, err := base.ComputeCost(desc, info)
	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
	assert.Empty(t, breakdown)
}
