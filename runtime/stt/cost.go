package stt

import (
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// CostInfoToMetaMap serializes a CostInfo into the map[string]any shape
// that the arena statestore expects when reading ancillary cost from
// Message.Meta["stt_cost"]. The keys and types must match exactly what
// addCostFromMetaKey reads (see cost_aggregation.go).
func CostInfoToMetaMap(ci *types.CostInfo) map[string]any {
	if ci == nil {
		return nil
	}
	m := map[string]any{
		"total_cost_usd":  ci.TotalCost,
		"input_cost_usd":  ci.InputCostUSD,
		"output_cost_usd": ci.OutputCostUSD,
		"input_tokens":    ci.InputTokens,
		"output_tokens":   ci.OutputTokens,
		"capability":      ci.Capability,
		"provider_name":   ci.ProviderName,
	}
	if len(ci.Quantities) > 0 {
		m["quantities"] = ci.Quantities
	}
	if len(ci.DimensionMatch) > 0 {
		m["dimension_match"] = ci.DimensionMatch
	}
	return m
}
