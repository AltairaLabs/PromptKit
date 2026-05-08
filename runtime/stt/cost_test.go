package stt_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestCostInfoToMetaMap_Nil(t *testing.T) {
	if got := stt.CostInfoToMetaMap(nil); got != nil {
		t.Errorf("CostInfoToMetaMap(nil) = %v, want nil", got)
	}
}

func TestCostInfoToMetaMap_Basic(t *testing.T) {
	ci := &types.CostInfo{
		TotalCost:     0.00015,
		InputCostUSD:  0.0001,
		OutputCostUSD: 0.00005,
		InputTokens:   100,
		OutputTokens:  50,
		Capability:    "stt",
		ProviderName:  "openai",
	}
	m := stt.CostInfoToMetaMap(ci)
	if m == nil {
		t.Fatal("CostInfoToMetaMap returned nil for non-nil CostInfo")
	}
	if m["total_cost_usd"] != ci.TotalCost {
		t.Errorf("total_cost_usd = %v, want %v", m["total_cost_usd"], ci.TotalCost)
	}
	if m["capability"] != "stt" {
		t.Errorf("capability = %v, want %q", m["capability"], "stt")
	}
	if m["provider_name"] != "openai" {
		t.Errorf("provider_name = %v, want %q", m["provider_name"], "openai")
	}
	if _, hasQty := m["quantities"]; hasQty {
		t.Error("quantities key should be absent when Quantities is empty")
	}
}

func TestCostInfoToMetaMap_WithQuantities(t *testing.T) {
	ci := &types.CostInfo{
		TotalCost:  0.0001,
		Capability: "stt",
		Quantities: map[string]float64{"second": 1.0},
		DimensionMatch: map[string]string{
			"second": "ok",
		},
	}
	m := stt.CostInfoToMetaMap(ci)
	if m == nil {
		t.Fatal("expected non-nil map")
	}
	if _, ok := m["quantities"]; !ok {
		t.Error("quantities key should be present when Quantities is non-empty")
	}
	if _, ok := m["dimension_match"]; !ok {
		t.Error("dimension_match key should be present when DimensionMatch is non-empty")
	}
}
