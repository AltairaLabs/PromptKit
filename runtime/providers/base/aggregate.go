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
		for i := range p.Breakdown {
			li := p.Breakdown[i]
			key := lineKey(li)
			if agg, ok := lines[key]; ok {
				agg.Quantity += li.Quantity
				agg.USD += li.USD
			} else {
				cp := li
				lines[key] = &cp
				order = append(order, key)
			}
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

func lineKey(li types.CostLineItem) string {
	dims := make([]string, 0, len(li.Dimensions))
	for k, v := range li.Dimensions {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	return li.Provider + "|" + li.Capability + "|" + li.Unit + "|" + strings.Join(dims, ",")
}
