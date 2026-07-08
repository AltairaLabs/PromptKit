// Package base — canonical token-unit vocabulary shared by all inference
// providers and their pricing tables. The pricing engine still accepts any
// unit string; these constants exist so providers and price items never drift.
package base

// Canonical token units. Providers MUST emit these names; pricing tables MUST
// price them by the same names.
const (
	UnitInputToken       = "input_token"        // full-price, uncached input
	UnitCacheReadToken   = "cache_read_token"   // prompt-cache read (discounted input)
	UnitCacheWriteToken  = "cache_write_token"  //nolint:gosec // prompt-cache creation (premium input), not a credential
	UnitOutputToken      = "output_token"       // visible output only, EXCLUDES reasoning
	UnitReasoningToken   = "reasoning_token"    // thinking tokens, priced additively
	UnitAudioInputToken  = "audio_input_token"  //nolint:gosec // realtime audio in, not a credential
	UnitAudioOutputToken = "audio_output_token" // realtime audio out
)

// TokenUsage is the normalized, provider-agnostic usage a provider hands to
// PriceUsage. Each provider maps its wire usage into these canonical meanings,
// absorbing its own quirks (cache-inclusive vs -exclusive input, reasoning
// bundled into output vs separate) so every field means the same thing here.
type TokenUsage struct {
	Input       int // full-price uncached input
	CacheRead   int
	CacheWrite  int
	Output      int // visible output only
	Reasoning   int
	AudioInput  int
	AudioOutput int
	// Extra carries future/uncommon units keyed by canonical unit name.
	Extra map[string]float64
}

// tokenUsageUnitCount is the number of canonical unit fields on TokenUsage,
// used to pre-size the Quantities map.
const tokenUsageUnitCount = 7

// Quantities converts the usage into the unit-keyed map the pricing engine
// consumes, omitting zero-valued units.
func (u TokenUsage) Quantities() map[string]float64 {
	q := make(map[string]float64, tokenUsageUnitCount)
	set := func(unit string, v int) {
		if v != 0 {
			q[unit] = float64(v)
		}
	}
	set(UnitInputToken, u.Input)
	set(UnitCacheReadToken, u.CacheRead)
	set(UnitCacheWriteToken, u.CacheWrite)
	set(UnitOutputToken, u.Output)
	set(UnitReasoningToken, u.Reasoning)
	set(UnitAudioInputToken, u.AudioInput)
	set(UnitAudioOutputToken, u.AudioOutput)
	for k, v := range u.Extra {
		if v != 0 {
			q[k] = v
		}
	}
	return q
}
