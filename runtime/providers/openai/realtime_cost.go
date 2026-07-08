// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// gpt-realtime GA pricing per 1K tokens (USD). Audio costs an order of
// magnitude more than text — pricing one against the other vastly mis-
// estimates the bill. The wire `usage` payload includes a per-type
// breakdown (input_token_details / output_token_details), and we use it.
//
// Source: https://platform.openai.com/docs/pricing (gpt-realtime row).
const (
	defaultAudioInputCostPer1K  = 0.032 // $32 / 1M
	defaultAudioOutputCostPer1K = 0.064 // $64 / 1M
	defaultTextInputCostPer1K   = 0.004 // $4  / 1M
	defaultTextOutputCostPer1K  = 0.016 // $16 / 1M
)

// realtimeK converts a per-1K rate to a per-unit rate for the pricing engine.
const realtimeK = 1000.0

// realtimeDescriptor prices gpt-realtime GA text + audio units. audioInRate /
// audioOutRate, when non-zero, replace the default GA per-1K audio rates
// (applied to AUDIO tokens only — text tokens always bill at the default
// text rates, since audio dominates a realtime bill). Cached input is billed
// at the same per-1K rate as fresh text input on the gpt-realtime GA model
// (a discount for prompt-cached audio is folded in by the server already;
// we don't double-count) — seeded at parity via cache_read_token, refine if
// OpenAI ever publishes a distinct cached-audio rate.
func realtimeDescriptor(audioInRate, audioOutRate float64) *base.PricingDescriptor {
	if audioInRate == 0 {
		audioInRate = defaultAudioInputCostPer1K
	}
	if audioOutRate == 0 {
		audioOutRate = defaultAudioOutputCostPer1K
	}
	return &base.PricingDescriptor{
		Source: base.PricingSourceInline,
		Items: []base.PriceItem{
			{Unit: base.UnitInputToken, Rate: defaultTextInputCostPer1K / realtimeK},
			{Unit: base.UnitOutputToken, Rate: defaultTextOutputCostPer1K / realtimeK},
			{Unit: base.UnitAudioInputToken, Rate: audioInRate / realtimeK},
			{Unit: base.UnitAudioOutputToken, Rate: audioOutRate / realtimeK},
			{Unit: base.UnitCacheReadToken, Rate: defaultTextInputCostPer1K / realtimeK},
		},
	}
}

// realtimeUsageToTokens maps the per-modality breakdown on a realtime
// UsageInfo into canonical base.TokenUsage units, falling back to "all
// input/output tokens are audio" when the breakdown is missing (older API
// responses, mock providers) — over-reporting text at audio rates (~8x) is
// preferable to silently reporting $0 or under-reporting audio at text
// rates for a realtime session.
//
// CachedTokens is a subset of InputTokenDetails.TextTokens on the wire (a
// cache hit still counts toward the text-token total); it is priced as its
// own cache_read_token line and removed from full-price text input so it is
// not double-counted (finding H: cached tokens were previously dropped
// entirely by the old calculateRealtimeCost, which had no cache handling).
func realtimeUsageToTokens(u *UsageInfo) base.TokenUsage {
	inAudio, inText := u.InputTokenDetails.AudioTokens, u.InputTokenDetails.TextTokens
	outAudio, outText := u.OutputTokenDetails.AudioTokens, u.OutputTokenDetails.TextTokens

	// Fall back to "all tokens are audio" if the breakdown is missing.
	if inAudio == 0 && inText == 0 {
		inAudio = u.InputTokens
	}
	if outAudio == 0 && outText == 0 {
		outAudio = u.OutputTokens
	}

	cached := u.InputTokenDetails.CachedTokens
	if cached > inText {
		cached = inText
	}

	return base.TokenUsage{
		Input:       inText - cached,
		CacheRead:   cached,
		AudioInput:  inAudio,
		Output:      outText,
		AudioOutput: outAudio,
	}
}

// realtimeCostInfo computes the USD cost of a single realtime turn from its
// token usage via the shared unit-keyed pricing engine. audioInRate /
// audioOutRate, when non-zero, override the default GA audio per-1K rates.
// Returns nil for nil usage.
func realtimeCostInfo(u *UsageInfo, audioInRate, audioOutRate float64) *types.CostInfo {
	if u == nil {
		return nil
	}
	ci := base.PriceUsage(realtimeDescriptor(audioInRate, audioOutRate),
		"openai-realtime", base.ProviderTypeInference, realtimeUsageToTokens(u), nil, 0)
	return &ci
}
