package claude

import "testing"

// costFromUsage must price every model at its published per-1M rates
// (https://platform.claude.com/docs/en/about-claude/pricing). A wrong rate
// here means we report a wrong USD cost to users — a real correctness and
// liability bug (e.g. haiku-4-5 silently billing at 3x Sonnet, or every model
// newer than 4.5 pricing at $0).
func TestClaudePricing_CurrentModels(t *testing.T) {
	cases := []struct {
		model             string
		in, out, readRate float64 // USD per 1M tokens
	}{
		// Haiku 4.5 — the original bug: was hitting the Sonnet rate (3x).
		{"claude-haiku-4-5", 1.0, 5.0, 0.1},
		{"claude-haiku-4-5-20251001", 1.0, 5.0, 0.1},
		{"claude-sonnet-4-5", 3.0, 15.0, 0.3},
		{"claude-opus-4-5", 5.0, 25.0, 0.5},
		{"claude-opus-4-5-20251101", 5.0, 25.0, 0.5},
		{"claude-opus-4-1", 15.0, 75.0, 1.5},
		// Every model below priced at $0 before #2282.
		{"claude-sonnet-4-6", 3.0, 15.0, 0.3},
		{"claude-opus-4-6", 5.0, 25.0, 0.5},
		{"claude-opus-4-7", 5.0, 25.0, 0.5},
		{"claude-opus-4-8", 5.0, 25.0, 0.5},
		{"claude-opus-5", 5.0, 25.0, 0.5},
		// Opus 5.5 and Sonnet 5.5 read the cache at 0.05x input, not 0.1x.
		{"claude-opus-5-5", 4.0, 20.0, 0.2},
		{"claude-sonnet-5-5", 2.0, 10.0, 0.1},
		{"claude-sonnet-5", 2.0, 10.0, 0.2},
		{"claude-fable-5", 10.0, 50.0, 1.0},
		{"claude-mythos-5", 10.0, 50.0, 1.0},
		// Fable 5.1 reads the cache at 0.025x input.
		{"claude-fable-5-1", 10.0, 50.0, 0.25},
		{"claude-mythos-5-1", 10.0, 50.0, 0.25},
		// Vendor-qualified ids normalize to the bare key.
		{"anthropic/claude-opus-5-5", 4.0, 20.0, 0.2},
		// Legacy still correct.
		{"claude-3-opus-20240229", 15.0, 75.0, 1.5},
		{"claude-3-5-haiku-20241022", 0.8, 4.0, 0.08},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			assertClaudeRates(t, c.model, 1_000_000, c.in, c.out, c.readRate)
		})
	}
}

// Haiku 5.5 is priced by prompt length: a prompt over 100K tokens — counting
// cache reads and writes, not just uncached input — pays 5x on every unit.
func TestClaudePricing_Haiku55PromptLengthTiers(t *testing.T) {
	const model = "claude-haiku-5-5"
	// Exactly at the threshold: standard rates. Scaled to per-1M below.
	t.Run("at 100K", func(t *testing.T) {
		assertClaudeRates(t, model, 50_000, 0.1, 0.5, 0.01)
	})
	t.Run("over 100K", func(t *testing.T) {
		assertClaudeRates(t, model, 50_001, 0.5, 2.5, 0.05)
	})
	// Cache writes count toward the prompt length too.
	t.Run("cache writes push it over", func(t *testing.T) {
		p := newTestClaudeProvider(t, model)
		ci := p.costFromUsage(claudeUsage{InputTokens: 60_000, CacheCreationInputTokens: 60_000})
		// 60K input at $0.50/M + 60K cache write at 1.25 x $0.50/M.
		want := 0.06*0.5 + 0.06*0.5*1.25
		if pricingAbsDiff(ci.InputCostUSD, want) > 1e-9 {
			t.Errorf("InputCostUSD = %g, want %g (over-100K rates)", ci.InputCostUSD, want)
		}
	})
	// Other models ignore the prompt-size dimension.
	t.Run("other models unaffected", func(t *testing.T) {
		assertClaudeRates(t, "claude-opus-5-5", 50_001, 4.0, 20.0, 0.2)
	})
}

// assertClaudeRates sends n input, n output and n cache-read tokens and checks
// each headline cost against the per-1M rates scaled to n.
func assertClaudeRates(t *testing.T, model string, n int, in, out, read float64) {
	t.Helper()
	p := newTestClaudeProvider(t, model)
	ci := p.costFromUsage(claudeUsage{InputTokens: n, OutputTokens: n, CacheReadInputTokens: n})
	scale := float64(n) / perMillionScale
	const tol = 1e-9
	if pricingAbsDiff(ci.InputCostUSD, in*scale) > tol || pricingAbsDiff(ci.OutputCostUSD, out*scale) > tol ||
		pricingAbsDiff(ci.CachedCostUSD, read*scale) > tol {
		t.Errorf("costFromUsage(%q, %d tokens) = (in %g, out %g, cached %g), want (%g, %g, %g)",
			model, n, ci.InputCostUSD, ci.OutputCostUSD, ci.CachedCostUSD, in*scale, out*scale, read*scale)
	}
}

// Every table entry must price input, output, cache read and cache write, so
// no listed model falls through to $0 on any unit.
func TestClaudePricing_EveryEntryPricesAllUnits(t *testing.T) {
	for model := range claudePricingTable {
		p := newTestClaudeProvider(t, model)
		ci := p.costFromUsage(claudeUsage{
			InputTokens: 1000, OutputTokens: 1000, CacheReadInputTokens: 1000, CacheCreationInputTokens: 1000,
		})
		if len(ci.Breakdown) != 4 {
			t.Errorf("%s: %d priced units, want 4 (input, output, cache read, cache write)", model, len(ci.Breakdown))
		}
		for _, li := range ci.Breakdown {
			if li.USD <= 0 {
				t.Errorf("%s: unit %s priced at %g", model, li.Unit, li.USD)
			}
		}
	}
}

func pricingAbsDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
