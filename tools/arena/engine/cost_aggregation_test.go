package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestAddSelfPlayCostFromMeta_FloatJSONShape(t *testing.T) {
	// JSON-decoded shape: numeric fields land as float64.
	total := types.CostInfo{}
	meta := map[string]interface{}{
		"self_play_cost": map[string]interface{}{
			"input_tokens":    float64(180),
			"output_tokens":   float64(152),
			"cached_tokens":   float64(0),
			"input_cost_usd":  float64(0.000054),
			"output_cost_usd": float64(0.0000912),
			"total_cost_usd":  float64(0.0001452),
		},
	}
	addSelfPlayCostFromMeta(&total, meta)

	if total.InputTokens != 180 {
		t.Errorf("InputTokens = %d, want 180", total.InputTokens)
	}
	if total.OutputTokens != 152 {
		t.Errorf("OutputTokens = %d, want 152", total.OutputTokens)
	}
	if total.TotalCost <= 0 {
		t.Errorf("TotalCost = %v, want > 0", total.TotalCost)
	}
}

func TestAddSelfPlayCostFromMeta_AddsToExistingTotal(t *testing.T) {
	// Confirm we ADD rather than overwrite — total may already hold the
	// assistant turn cost when the self-play turn is rolled in.
	total := types.CostInfo{
		InputTokens:  100,
		OutputTokens: 50,
		TotalCost:    0.01,
	}
	meta := map[string]interface{}{
		"self_play_cost": map[string]interface{}{
			"input_tokens":   float64(180),
			"output_tokens":  float64(152),
			"total_cost_usd": float64(0.0001),
		},
	}
	addSelfPlayCostFromMeta(&total, meta)

	if total.InputTokens != 280 {
		t.Errorf("InputTokens = %d, want 280", total.InputTokens)
	}
	if total.OutputTokens != 202 {
		t.Errorf("OutputTokens = %d, want 202", total.OutputTokens)
	}
	want := 0.01 + 0.0001
	if total.TotalCost < want-1e-9 || total.TotalCost > want+1e-9 {
		t.Errorf("TotalCost = %v, want ~%v", total.TotalCost, want)
	}
}

func TestAddSelfPlayCostFromMeta_NoMeta(t *testing.T) {
	// No meta, nil meta, missing key, wrong shape — all silent no-ops.
	total := types.CostInfo{}
	addSelfPlayCostFromMeta(&total, nil)
	addSelfPlayCostFromMeta(&total, map[string]interface{}{})
	addSelfPlayCostFromMeta(&total, map[string]interface{}{"self_play_cost": "not a map"})
	addSelfPlayCostFromMeta(nil, map[string]interface{}{"self_play_cost": map[string]interface{}{"total_cost_usd": 0.5}})

	if total.TotalCost != 0 || total.InputTokens != 0 {
		t.Errorf("expected total to remain zero, got %+v", total)
	}
}

func TestAddSelfPlayCostFromMeta_IntShape(t *testing.T) {
	// In-memory shape (before persistence): ints stay ints.
	total := types.CostInfo{}
	meta := map[string]interface{}{
		"self_play_cost": map[string]interface{}{
			"input_tokens":   100,
			"output_tokens":  int64(50),
			"total_cost_usd": float32(0.0002),
		},
	}
	addSelfPlayCostFromMeta(&total, meta)

	if total.InputTokens != 100 || total.OutputTokens != 50 {
		t.Errorf("token rollup wrong: %+v", total)
	}
	if total.TotalCost < 0.0001 || total.TotalCost > 0.0003 {
		t.Errorf("TotalCost = %v, want ~0.0002", total.TotalCost)
	}
}
