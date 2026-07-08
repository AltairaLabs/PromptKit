package providers

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNormalizeOpenAIFinishReason(t *testing.T) {
	cases := map[string]string{
		"stop":           types.FinishReasonStop,
		"eos_token":      types.FinishReasonStop, // vLLM native stop-token reason
		"length":         types.FinishReasonMaxOutputTokens,
		"tool_calls":     types.FinishReasonToolUse,
		"function_call":  types.FinishReasonToolUse,
		"content_filter": types.FinishReasonSafety,
		"":               "",              // unreported passes through
		"weird_new_one":  "weird_new_one", // unknown passes through verbatim
	}
	for raw, want := range cases {
		if got := NormalizeOpenAIFinishReason(raw); got != want {
			t.Errorf("NormalizeOpenAIFinishReason(%q) = %q, want %q", raw, got, want)
		}
	}
}
