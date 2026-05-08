package tts

import (
	"time"
	"unicode/utf8"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// pricer is the optional interface that TTS Service implementations may satisfy
// to expose their pricing descriptor and identity to the cost-stamping layer.
// It is checked via a type assertion at the call site — Service itself is
// intentionally kept narrow to avoid breaking existing implementations.
type pricer interface {
	Pricing() *base.PricingDescriptor
	ImplName() string
	ModelName() string
}

// ComputeTTSCost computes the cost for a TTS call given the synthesized text
// and the service. The svc parameter accepts any value; non-pricer
// implementations (including mock services) return nil cost without error.
//
// The returned CostInfo has:
//   - Quantities["character"] set to the UTF-8 rune count of text
//   - TotalCost set to the computed USD amount
//   - Capability set to "tts"
//   - ProviderName set from ImplName()
//   - Latency set to the provided duration
func ComputeTTSCost(svc any, text string, latency time.Duration) *types.CostInfo {
	p, ok := svc.(pricer)
	if !ok {
		return nil
	}
	desc := p.Pricing()
	if desc == nil {
		return nil // free/local provider — no pricing configured
	}
	charCount := utf8.RuneCountInString(text)

	info := &types.CostInfo{
		Quantities:   map[string]float64{"character": float64(charCount)},
		ProviderName: p.ImplName(),
		Capability:   string(base.ProviderTypeTTS),
		Latency:      latency,
	}

	usd, _, err := base.ComputeCost(desc, info)
	if err != nil {
		// Pricing mismatch — return the quantities without a dollar amount
		// rather than dropping the cost entry entirely.
		return info
	}
	info.TotalCost = usd
	return info
}

// CostInfoToMetaMap serializes a CostInfo into the map[string]any shape
// that addCostFromMetaKey (and the arena statestore) expect when reading
// ancillary cost from Message.Meta. The keys and types must match exactly
// what addCostFromMetaKey reads (see cost_aggregation.go).
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
