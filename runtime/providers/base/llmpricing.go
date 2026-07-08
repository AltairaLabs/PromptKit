package base

import "strings"

// FlatPricing is the legacy per-1K input/output rate a config may carry. It is
// mapped to canonical input_token/output_token price items when no richer
// descriptor is supplied. base defines its own type (rather than reusing a
// providers-package type) to avoid importing the providers package, which
// would create an import cycle.
type FlatPricing struct {
	Input  float64 // per 1K input tokens
	Output float64 // per 1K output tokens
}

// per1KScale converts a per-1K rate to a per-unit rate.
const per1KScale = 1000.0

// ResolveLLMPricing selects the pricing descriptor for a model, in order:
// explicit config descriptor > flat config pricing > embedded default table >
// nil (paid provider; PriceUsage will warn on nonzero unpriced units).
func ResolveLLMPricing(
	configDesc *PricingDescriptor, flat FlatPricing,
	table map[string]*PricingDescriptor, model string,
) *PricingDescriptor {
	if configDesc != nil && len(configDesc.Items) > 0 {
		return configDesc
	}
	if flat.Input > 0 && flat.Output > 0 {
		// Reasoning tokens bill at the output rate for every provider that
		// splits them out (OpenAI o-series, Gemini thinking), so a flat config
		// — which does carry a known output rate — must price reasoning too, or
		// a thinking model under flat config understates cost by the reasoning
		// amount. (Cache tokens are deliberately NOT priced here: a flat config
		// carries no provider-specific cache multiplier, so they go loud-unpriced.)
		return &PricingDescriptor{Source: PricingSourceInline, Items: []PriceItem{
			{Unit: UnitInputToken, Rate: flat.Input / per1KScale},
			{Unit: UnitOutputToken, Rate: flat.Output / per1KScale},
			{Unit: UnitReasoningToken, Rate: flat.Output / per1KScale},
		}}
	}
	if d, ok := table[normalizeModel(model)]; ok {
		return d
	}
	return nil
}

// normalizeModel strips a leading vendor prefix (everything up to and
// including the last "/") so vendor-qualified model ids (e.g.
// "anthropic/claude-sonnet-4-5") match bare table keys. Providers whose wire
// model ids differ in other ways should pass an already-normalized model.
func normalizeModel(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		model = model[i+1:]
	}
	return model
}
