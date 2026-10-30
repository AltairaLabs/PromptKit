package claude

import "testing"

// costFromUsage must price every model in claudePricingTable at its published
// per-1M rates, with cache reads at 10% of the input rate. A wrong rate here
// means we report a wrong USD cost to users — a real correctness/liability bug
// (e.g. haiku-4-5 silently billing at 3x Sonnet).
func TestClaudePricing_CurrentModels(t *testing.T) {
	cases := []struct {
		model               string
		in, out, cachedRate float64 // USD per 1M tokens
	}{
		// Haiku 4.5 — the exact bug: was hitting the Sonnet rate (3x).
		{"claude-haiku-4-5", 1.0, 5.0, 0.1},
		{"claude-haiku-4-5-20251001", 1.0, 5.0, 0.1},
		{"claude-sonnet-4-5", 3.0, 15.0, 0.3},
		// Legacy still correct.
		{"claude-3-opus-20240229", 15.0, 75.0, 1.5},
		{"claude-3-5-haiku-20241022", 1.0, 5.0, 0.1},
	}
	const tol = 1e-9
	for _, c := range cases {
		p := newTestClaudeProvider(t, c.model)
		ci := p.costFromUsage(claudeUsage{
			InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadInputTokens: 1_000_000,
		})
		if pricingAbsDiff(ci.InputCostUSD, c.in) > tol || pricingAbsDiff(ci.OutputCostUSD, c.out) > tol ||
			pricingAbsDiff(ci.CachedCostUSD, c.cachedRate) > tol {
			t.Errorf("costFromUsage(%q) = (in %g, out %g, cached %g), want (%g, %g, %g)",
				c.model, ci.InputCostUSD, ci.OutputCostUSD, ci.CachedCostUSD, c.in, c.out, c.cachedRate)
		}
	}
}

func pricingAbsDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
