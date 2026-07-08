package base

import (
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// AggregateCost merges per-message CostInfo into one roll-up: it sums
// Quantities, merges Breakdown line items by (provider, capability, unit,
// dimensions), and re-derives the flat directional headlines so the
// TotalCost == Input+Output+Cached invariant holds on the aggregate. This is
// the single source of truth for cost roll-up — no caller sums CostInfo by hand.
//
// A part with an empty Breakdown (headline-only: pre-migration provider
// results, or older/serialized CostInfo values that never populated
// Breakdown/Quantities) is never dropped — its flat fields are synthesized
// into breakdown lines so the cost flows through the same merge path. A part
// with a non-empty Breakdown uses only that Breakdown (its flat fields are
// re-derived output, not re-read) to avoid double-counting.
func AggregateCost(parts ...*types.CostInfo) types.CostInfo {
	out := types.CostInfo{Quantities: map[string]float64{}}
	lines := map[string]*types.CostLineItem{}
	var order []string

	for _, p := range parts {
		if p == nil {
			continue
		}
		if out.ProviderName == "" {
			out.ProviderName = p.ProviderName
			out.Capability = p.Capability
		}
		for unit, qty := range p.Quantities {
			out.Quantities[unit] += qty
		}
		if len(p.Breakdown) > 0 {
			for i := range p.Breakdown {
				mergeLine(lines, &order, p.Breakdown[i])
			}
			continue
		}
		var synthUSD float64
		for _, li := range synthesizeLines(p) {
			if _, ok := p.Quantities[li.Unit]; !ok {
				out.Quantities[li.Unit] += li.Quantity
			}
			synthUSD += li.USD
			mergeLine(lines, &order, li)
		}
		// Preserve any cost the flat TotalCost carries that the synthesized
		// lines didn't account for (e.g. imagen's per-image TotalCost with no
		// token buckets, or replay's token counts priced at $0 with the real
		// cost only in TotalCost). Without this, a headline-only part whose
		// TotalCost isn't fully explained by Input/Output/CachedCostUSD would
		// silently lose the difference.
		residual := p.TotalCost - synthUSD
		if residual > 1e-12 || residual < -1e-12 {
			mergeLine(lines, &order, types.CostLineItem{
				Provider:   p.ProviderName,
				Capability: p.Capability,
				Unit:       UnitOutputToken,
				Quantity:   0,
				USD:        residual,
			})
		}
	}

	for _, k := range order {
		li := *lines[k]
		out.Breakdown = append(out.Breakdown, li)
		out.TotalCost += li.USD
	}
	deriveHeadlines(&out)
	if len(out.Quantities) == 0 {
		out.Quantities = nil
	}
	return out
}

// mergeLine folds li into the accumulating breakdown, keyed by (provider,
// capability, unit, dimensions): repeated keys sum Quantity and USD, new
// keys are recorded in first-seen order so output is deterministic.
func mergeLine(lines map[string]*types.CostLineItem, order *[]string, li types.CostLineItem) {
	key := lineKey(li)
	if agg, ok := lines[key]; ok {
		agg.Quantity += li.Quantity
		agg.USD += li.USD
		return
	}
	cp := li
	lines[key] = &cp
	*order = append(*order, key)
}

// synthesizeLines builds Breakdown-shaped lines from a headline-only
// CostInfo's flat fields (Input/Cached/OutputTokens and their *CostUSD
// siblings), tagged with the part's own ProviderName/Capability. This lets
// values that never populated Breakdown/Quantities flow through the same
// merge + deriveHeadlines path as unified-path values, so AggregateCost
// never silently drops cost.
func synthesizeLines(p *types.CostInfo) []types.CostLineItem {
	var out []types.CostLineItem
	add := func(unit string, qty float64, usd float64) {
		if qty != 0 || usd != 0 {
			out = append(out, types.CostLineItem{
				Provider:   p.ProviderName,
				Capability: p.Capability,
				Unit:       unit,
				Quantity:   qty,
				USD:        usd,
			})
		}
	}
	add(UnitInputToken, float64(p.InputTokens), p.InputCostUSD)
	add(UnitCacheReadToken, float64(p.CachedTokens), p.CachedCostUSD)
	add(UnitOutputToken, float64(p.OutputTokens), p.OutputCostUSD)
	return out
}

func lineKey(li types.CostLineItem) string {
	dims := make([]string, 0, len(li.Dimensions))
	for k, v := range li.Dimensions {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	return li.Provider + "|" + li.Capability + "|" + li.Unit + "|" + strings.Join(dims, ",")
}
