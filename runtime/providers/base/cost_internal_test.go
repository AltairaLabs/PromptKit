package base

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestComputeCostPartial_PricesMatchedCollectsUnmatched(t *testing.T) {
	desc := &PricingDescriptor{Items: []PriceItem{{Unit: UnitInputToken, Rate: 0.001}}}
	info := &types.CostInfo{Quantities: map[string]float64{
		UnitInputToken:     100, // priced
		UnitReasoningToken: 20,  // no matching item
	}}
	total, breakdown, unpriced := computeCostPartial(desc, info)
	assert.InDelta(t, 0.1, total, 1e-9) // only the matched 100*0.001
	assert.Len(t, breakdown, 1)
	assert.Equal(t, []unpricedUnit{{Unit: UnitReasoningToken, Quantity: 20}}, unpriced)
}

func TestComputeCostPartial_NilDescriptorSilent(t *testing.T) {
	info := &types.CostInfo{Quantities: map[string]float64{UnitInputToken: 100}}
	total, breakdown, unpriced := computeCostPartial(nil, info)
	assert.Zero(t, total)
	assert.Nil(t, breakdown)
	assert.Nil(t, unpriced)
}
