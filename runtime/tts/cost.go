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
	return base.MakeCostInfo(
		desc,
		p.ImplName(),
		base.ProviderTypeTTS,
		map[string]float64{"character": float64(charCount)},
		latency,
	)
}

// CostInfoToMetaMap is kept here as a deprecated alias for back-compat with
// existing call sites. Prefer base.CostInfoToMetaMap directly.
//
// Deprecated: use base.CostInfoToMetaMap.
func CostInfoToMetaMap(ci *types.CostInfo) map[string]any {
	return base.CostInfoToMetaMap(ci)
}
