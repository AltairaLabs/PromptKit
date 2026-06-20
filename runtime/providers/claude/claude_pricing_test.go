package claude

import "testing"

// claudePricing must return the correct per-1K rates for every current model.
// A wrong rate here means we report a wrong USD cost to users — a real
// correctness/liability bug (e.g. haiku-4-5 silently billing at 3x Sonnet).
func TestClaudePricing_CurrentModels(t *testing.T) {
	cases := []struct {
		model               string
		in, out, cachedRate float64
	}{
		// Haiku 4.5 — the exact bug: was hitting the Sonnet default (3x).
		{"claude-haiku-4-5", 0.001, 0.005, 0.0001},
		{"claude-sonnet-4-6", 0.003, 0.015, 0.0003},
		{"claude-opus-4-8", 0.005, 0.025, 0.0005},
		{"claude-opus-4-7", 0.005, 0.025, 0.0005},
		{"claude-opus-4-6", 0.005, 0.025, 0.0005},
		{"claude-fable-5", 0.010, 0.050, 0.001},
		{"claude-mythos-5", 0.010, 0.050, 0.001},
		// Legacy still correct.
		{"claude-3-opus-20240229", 0.015, 0.075, 0.0015},
		{"claude-3-5-haiku-20241022", 0.001, 0.005, 0.0001},
		// Heuristic fallback for unlisted dated snapshots — must NOT fall to Sonnet.
		{"claude-haiku-4-5-20260601", 0.001, 0.005, 0.0001},
		{"claude-opus-4-9-20270101", 0.005, 0.025, 0.0005},
		{"claude-fable-5-1", 0.010, 0.050, 0.001},
	}
	for _, c := range cases {
		in, out, cached := claudePricing(c.model)
		if in != c.in || out != c.out || cached != c.cachedRate {
			t.Errorf("claudePricing(%q) = (%g,%g,%g), want (%g,%g,%g)",
				c.model, in, out, cached, c.in, c.out, c.cachedRate)
		}
	}
}

// A genuinely unknown family stays on the conservative Sonnet default rather
// than guessing zero (which would under-report cost).
func TestClaudePricing_UnknownDefaultsToSonnet(t *testing.T) {
	in, out, cached := claudePricing("some-future-unrelated-model")
	if in != 0.003 || out != 0.015 || cached != 0.0003 {
		t.Errorf("unknown model should default to Sonnet pricing, got (%g,%g,%g)", in, out, cached)
	}
}
