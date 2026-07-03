// Package openai provides OpenAI Realtime API streaming support.
package openai

import "github.com/AltairaLabs/PromptKit/runtime/types"

// gpt-realtime GA pricing per 1K tokens (USD). Audio costs an order of
// magnitude more than text — pricing one against the other vastly mis-
// estimates the bill. The wire `usage` payload includes a per-type
// breakdown (input_token_details / output_token_details), and we use it.
//
// Cached input is billed at the same per-1K rate as fresh input on the
// gpt-realtime GA model (10% discount for prompt-cached audio is folded
// in by the server already; we don't double-count).
//
// Source: https://platform.openai.com/docs/pricing (gpt-realtime row).
const (
	defaultAudioInputCostPer1K  = 0.032 // $32 / 1M
	defaultAudioOutputCostPer1K = 0.064 // $64 / 1M
	defaultTextInputCostPer1K   = 0.004 // $4  / 1M
	defaultTextOutputCostPer1K  = 0.016 // $16 / 1M
)

// calculateRealtimeCost computes the USD cost of a single realtime turn from its
// token usage. audioInRateOverride / audioOutRateOverride, when non-zero,
// replace the default GA audio per-1K rates (applied to AUDIO tokens only —
// text tokens always bill at the default text rates, since audio dominates a
// realtime bill).
//
// When the per-type token breakdown is missing (input_token_details /
// output_token_details all zero — older API responses, mock providers) it falls
// back to treating ALL tokens as audio. Over-reporting text at audio rates
// (~8x) is preferable to silently reporting $0 or under-reporting audio at text
// rates for a realtime session. Returns nil for nil usage.
func calculateRealtimeCost(usage *UsageInfo, audioInRateOverride, audioOutRateOverride float64) *types.CostInfo {
	if usage == nil {
		return nil
	}

	audioInRate := audioInRateOverride
	if audioInRate == 0 {
		audioInRate = defaultAudioInputCostPer1K
	}
	audioOutRate := audioOutRateOverride
	if audioOutRate == 0 {
		audioOutRate = defaultAudioOutputCostPer1K
	}

	inAudio := usage.InputTokenDetails.AudioTokens
	inText := usage.InputTokenDetails.TextTokens
	outAudio := usage.OutputTokenDetails.AudioTokens
	outText := usage.OutputTokenDetails.TextTokens

	// Fall back to "all tokens are audio" if the breakdown is missing.
	if inAudio == 0 && inText == 0 {
		inAudio = usage.InputTokens
	}
	if outAudio == 0 && outText == 0 {
		outAudio = usage.OutputTokens
	}

	inputCost := float64(inAudio)/tokensPerThousand*audioInRate +
		float64(inText)/tokensPerThousand*defaultTextInputCostPer1K
	outputCost := float64(outAudio)/tokensPerThousand*audioOutRate +
		float64(outText)/tokensPerThousand*defaultTextOutputCostPer1K

	return &types.CostInfo{
		InputTokens:   usage.InputTokens,
		OutputTokens:  usage.OutputTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		TotalCost:     inputCost + outputCost,
	}
}
