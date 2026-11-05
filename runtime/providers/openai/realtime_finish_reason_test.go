package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestRealtimeFinishReason maps each Realtime response status onto the
// canonical vocabulary, so a voice turn reports why it ended in the same words
// as a text turn (#2307).
func TestRealtimeFinishReason(t *testing.T) {
	incomplete := func(reason string) interface{} {
		return map[string]interface{}{"type": "incomplete", "reason": reason}
	}
	cases := []struct {
		name    string
		status  string
		details interface{}
		want    string
	}{
		{"completed", "completed", nil, types.FinishReasonStop},
		{"canceled by barge-in", "cancelled", map[string]interface{}{"reason": "turn_detected"}, //nolint:misspell // wire value
			types.FinishReasonInterrupted},
		{"failed", "failed", nil, "error"},
		{"output budget", "incomplete", incomplete("max_output_tokens"), types.FinishReasonMaxOutputTokens},
		{"content filter", "incomplete", incomplete("content_filter"), types.FinishReasonSafety},
		{"incomplete without a known reason", "incomplete", incomplete("something_new"), "incomplete"},
		{"incomplete without details", "incomplete", nil, "incomplete"},
		{"unknown status passes through", "paused", nil, "paused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := realtimeFinishReason(&ResponseInfo{Status: tc.status, StatusDetails: tc.details})
			assert.Equal(t, tc.want, got)
		})
	}
}
