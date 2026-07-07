package base

import (
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// unpricedWarnOnce dedupes the loud "nonzero unpriced unit" warning so a
// missing rate cannot flood one line per request. Keyed by provider|capability|unit.
var unpricedWarnOnce sync.Map

// PriceUsage prices a normalized TokenUsage against desc, populating the
// authoritative Quantities+Breakdown and deriving the flat headline view. A
// nonzero unit with no price (and a non-nil descriptor) is surfaced loudly
// (deduped) and contributes to no cost — the total is the priced-partial sum,
// never a fabricated constant and never a swallowed $0. A nil descriptor is a
// free/local provider: zero cost, no warning.
func PriceUsage(
	desc *PricingDescriptor,
	providerName string,
	capability ProviderType,
	usage TokenUsage,
	dims map[string]string,
	latency time.Duration,
) types.CostInfo {
	ci := types.CostInfo{
		Quantities:     usage.Quantities(),
		DimensionMatch: dims,
		ProviderName:   providerName,
		Capability:     string(capability),
		Latency:        latency,
	}
	total, breakdown, unpriced := computeCostPartial(desc, &ci)
	ci.TotalCost = total
	ci.Breakdown = toCostLineItems(providerName, capability, breakdown)
	if len(unpriced) > 0 && desc != nil {
		warnUnpriced(providerName, capability, unpriced)
	}
	deriveHeadlines(&ci)
	return ci
}

// deriveHeadlines fills the flat directional headline fields from the priced
// Breakdown, preserving TotalCost == InputCostUSD + OutputCostUSD + CachedCostUSD.
//
//	InputCostUSD  <- input + cache_write + audio_input
//	CachedCostUSD <- cache_read
//	OutputCostUSD <- output + reasoning + audio_output
//
// Token headlines: InputTokens<-input, CachedTokens<-cache_read,
// OutputTokens<-output (visible-only). Other token counts live in Quantities.
func deriveHeadlines(ci *types.CostInfo) {
	for _, li := range ci.Breakdown {
		switch li.Unit {
		case UnitInputToken, UnitCacheWriteToken, UnitAudioInputToken:
			ci.InputCostUSD += li.USD
		case UnitCacheReadToken:
			ci.CachedCostUSD += li.USD
		case UnitOutputToken, UnitReasoningToken, UnitAudioOutputToken:
			ci.OutputCostUSD += li.USD
		}
	}
	ci.InputTokens = int(ci.Quantities[UnitInputToken])
	ci.CachedTokens = int(ci.Quantities[UnitCacheReadToken])
	ci.OutputTokens = int(ci.Quantities[UnitOutputToken])
}

// warnUnpriced surfaces nonzero units that had no matching price item. Each
// (provider, capability, unit) triple is warned at most once per process.
func warnUnpriced(providerName string, capability ProviderType, unpriced []unpricedUnit) {
	for _, u := range unpriced {
		key := providerName + "|" + string(capability) + "|" + u.Unit
		if _, loaded := unpricedWarnOnce.LoadOrStore(key, struct{}{}); loaded {
			continue
		}
		logger.Warn("cost: nonzero token unit has no pricing; cost understated",
			"provider", providerName, "capability", string(capability),
			"unit", u.Unit, "quantity", u.Quantity)
	}
}

// toCostLineItems converts the internal LineItem breakdown into the exported
// types.CostLineItem shape, tagging each row with provider + capability.
func toCostLineItems(provider string, capability ProviderType, in []LineItem) []types.CostLineItem {
	if len(in) == 0 {
		return nil
	}
	out := make([]types.CostLineItem, len(in))
	for i, li := range in {
		out[i] = types.CostLineItem{
			Provider:   provider,
			Capability: string(capability),
			Unit:       li.Unit,
			Quantity:   li.Quantity,
			USD:        li.USD,
			Dimensions: li.Dimensions,
		}
	}
	return out
}
