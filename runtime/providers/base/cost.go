package base

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// LineItem is one row of a cost breakdown for reports.
type LineItem struct {
	Unit       string            `json:"unit"`
	Quantity   float64           `json:"quantity"`
	Rate       float64           `json:"rate"`
	USD        float64           `json:"usd"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// unpricedUnit is a nonzero quantity with no matching price item.
type unpricedUnit struct {
	Unit     string
	Quantity float64
}

// computeCostPartial prices every unit it can match against desc and returns
// the nonzero units it could not price rather than failing outright. A nil
// descriptor (free/local provider) returns zero cost and no unpriced units.
//
// Matching rule: a PriceItem matches a quantity unit if PriceItem.Unit == unit AND
// every key in PriceItem.Dimensions has a matching value in info.DimensionMatch.
// When multiple items match, the one with the most dimension keys wins (most specific).
func computeCostPartial(
	desc *PricingDescriptor, info *types.CostInfo,
) (totalUSD float64, breakdown []LineItem, unpriced []unpricedUnit) {
	if desc == nil || info == nil || len(info.Quantities) == 0 {
		return 0, nil, nil
	}
	breakdown = make([]LineItem, 0, len(info.Quantities))
	for unit, qty := range info.Quantities {
		if qty == 0 {
			continue
		}
		item, ok := matchPriceItem(desc.Items, unit, info.DimensionMatch)
		if !ok {
			unpriced = append(unpriced, unpricedUnit{Unit: unit, Quantity: qty})
			continue
		}
		usd := qty * item.Rate
		totalUSD += usd
		breakdown = append(breakdown, LineItem{
			Unit:       unit,
			Quantity:   qty,
			Rate:       item.Rate,
			USD:        usd,
			Dimensions: copyMap(item.Dimensions),
		})
	}
	return totalUSD, breakdown, unpriced
}

// ComputeCost multiplies raw quantities by matching rates from the pricing
// descriptor. Returns total USD, the breakdown, and an error if ANY nonzero
// unit lacks a match (back-compat: callers relying on the strict error).
//
// nil pricing returns zero cost without error (free / local provider).
func ComputeCost(desc *PricingDescriptor, info *types.CostInfo) (totalUSD float64, breakdown []LineItem, err error) {
	total, bd, unpriced := computeCostPartial(desc, info)
	if len(unpriced) > 0 {
		return 0, nil, fmt.Errorf("no pricing match for unit=%q dimensions=%v",
			unpriced[0].Unit, info.DimensionMatch)
	}
	return total, bd, nil
}

// matchPriceItem returns the most-specific matching PriceItem for the given unit and dimensions.
func matchPriceItem(items []PriceItem, unit string, dims map[string]string) (*PriceItem, bool) {
	var best *PriceItem
	bestSpecificity := -1
	for i := range items {
		it := &items[i]
		if it.Unit != unit {
			continue
		}
		if !dimensionsSubset(it.Dimensions, dims) {
			continue
		}
		s := len(it.Dimensions)
		if s > bestSpecificity {
			best = it
			bestSpecificity = s
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

// dimensionsSubset returns true when every k=v in required matches provided[k].
// An empty required always matches.
func dimensionsSubset(required, provided map[string]string) bool {
	for k, v := range required {
		if provided[k] != v {
			return false
		}
	}
	return true
}

func copyMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// PricingFromAdditionalConfig extracts a *PricingDescriptor from a provider
// spec's AdditionalConfig map (under the "pricing" key) by JSON-round-tripping
// the value into the typed descriptor. Returns nil when the key is absent or
// the value can't be coerced — callers fall back to package-level defaults.
//
// Used by the TTS and STT factories that translate pkg/config provider specs
// into runtime services.
func PricingFromAdditionalConfig(additional map[string]any) *PricingDescriptor {
	raw, ok := additional["pricing"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var desc PricingDescriptor
	if err := json.Unmarshal(data, &desc); err != nil {
		return nil
	}
	return &desc
}

// MakeCostInfo builds a *types.CostInfo from raw quantities and prices every
// unit it can match against the supplied descriptor. Returns the CostInfo
// with quantities and identity tags populated even if pricing is nil —
// callers can decide whether to drop a nil-cost entry. Any nonzero quantity
// unit that has no matching price item is NOT silently swallowed to $0: the
// priced-partial total is kept and the gap is surfaced via a deduped warning
// (see warnUnpriced), so operators see undercounted cost instead of a false
// zero. This is the shared cost-construction path for ancillary providers
// (TTS, STT, image gen) that report a single-quantity unit at call time.
func MakeCostInfo(
	desc *PricingDescriptor,
	providerName string,
	capability ProviderType,
	quantities map[string]float64,
	latency time.Duration,
) *types.CostInfo {
	info := &types.CostInfo{
		Quantities:   quantities,
		ProviderName: providerName,
		Capability:   string(capability),
		Latency:      latency,
	}
	if desc == nil {
		return info
	}
	total, breakdown, unpriced := computeCostPartial(desc, info)
	info.TotalCost = total
	info.Breakdown = toCostLineItems(providerName, capability, breakdown)
	if len(unpriced) > 0 {
		warnUnpriced(providerName, capability, unpriced)
	}
	return info
}

// CostInfoToMetaMap serializes a CostInfo into the map[string]any shape
// the arena statestore expects when reading ancillary cost from
// Message.Meta keys (tts_cost, stt_cost, etc.). The keys and types must
// match what PromptArena's statestore telemetry costInfoFromMeta reads.
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
