package base_test

import (
	"testing"
	"time"

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

func TestComputeCost_WrapperErrorsWhenUnpriced(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: base.UnitInputToken, Rate: 0.001}}}
	info := &types.CostInfo{Quantities: map[string]float64{base.UnitReasoningToken: 20}}
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

func TestMakeCostInfo_PopulatesAllFields(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "second", Rate: 0.0001}}}
	info := base.MakeCostInfo(desc, "openai-stt", base.ProviderTypeSTT,
		map[string]float64{"second": 60}, 250*time.Millisecond)

	require.NotNil(t, info)
	assert.InDelta(t, 0.006, info.TotalCost, 1e-9)
	assert.Equal(t, "openai-stt", info.ProviderName)
	assert.Equal(t, string(base.ProviderTypeSTT), info.Capability)
	assert.Equal(t, 250*time.Millisecond, info.Latency)
	assert.Equal(t, float64(60), info.Quantities["second"])
}

func TestMakeCostInfo_NilPricing_ReturnsQuantitiesWithZeroCost(t *testing.T) {
	info := base.MakeCostInfo(nil, "free-provider", base.ProviderTypeTTS,
		map[string]float64{"character": 100}, 0)
	require.NotNil(t, info)
	assert.Equal(t, 0.0, info.TotalCost)
	assert.Equal(t, float64(100), info.Quantities["character"])
}

func TestMakeCostInfo_UnpricedNonzeroNotSwallowedToZero(t *testing.T) {
	desc := &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "character", Rate: 0.00001}}}
	// quantity present, but unit "second" has no price item
	ci := base.MakeCostInfo(desc, "tts", base.ProviderTypeTTS,
		map[string]float64{"character": 1000, "second": 5}, 0)
	// priced-partial: only the 1000 characters, NOT a swallowed $0
	assert.InDelta(t, 1000*0.00001, ci.TotalCost, 1e-9)
	assert.NotEmpty(t, ci.Breakdown)
}

func TestCostInfoToMetaMap_NilReturnsNil(t *testing.T) {
	assert.Nil(t, base.CostInfoToMetaMap(nil))
}

func TestCostInfoToMetaMap_PopulatesAllKeys(t *testing.T) {
	ci := &types.CostInfo{
		TotalCost:      0.04,
		InputCostUSD:   0.01,
		OutputCostUSD:  0.02,
		InputTokens:    100,
		OutputTokens:   50,
		Capability:     "image",
		ProviderName:   "imagen",
		Quantities:     map[string]float64{"image": 1},
		DimensionMatch: map[string]string{"size": "1024x1024"},
	}
	m := base.CostInfoToMetaMap(ci)

	assert.InDelta(t, 0.04, m["total_cost_usd"], 1e-9)
	assert.Equal(t, 100, m["input_tokens"])
	assert.Equal(t, "image", m["capability"])
	assert.Equal(t, "imagen", m["provider_name"])
	q, ok := m["quantities"].(map[string]float64)
	require.True(t, ok)
	assert.Equal(t, float64(1), q["image"])
	d, ok := m["dimension_match"].(map[string]string)
	require.True(t, ok)
	assert.Equal(t, "1024x1024", d["size"])
}

func TestCostInfoToMetaMap_EmptyOptionalFieldsOmitted(t *testing.T) {
	ci := &types.CostInfo{TotalCost: 0.001}
	m := base.CostInfoToMetaMap(ci)
	_, hasQ := m["quantities"]
	_, hasD := m["dimension_match"]
	assert.False(t, hasQ, "empty Quantities should be omitted")
	assert.False(t, hasD, "empty DimensionMatch should be omitted")
}
