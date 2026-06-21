package types

import "testing"

func TestCanonicalFinishReasonValues(t *testing.T) {
	cases := map[string]string{
		FinishReasonStop:            "stop",
		FinishReasonMaxOutputTokens: "max_output_tokens",
		FinishReasonToolUse:         "tool_use",
		FinishReasonSafety:          "safety",
		FinishReasonRefusal:         "refusal",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("constant = %q, want %q", got, want)
		}
	}
}
