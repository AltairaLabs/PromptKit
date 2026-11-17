package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Gemini Live reports why a turn completed in serverContent.turnCompleteReason.
// A turn its own safety filters ended (prohibited input, generated content or
// audio failing a check) is reported as finish reason safety, with the raw
// reason under the chunk's turn_complete_reason metadata.
func TestLiveTurnCompleteReason_SafetyReasonsAreSafety(t *testing.T) {
	cases := []struct {
		name, wire, want string
		withModelTurn    bool
	}{
		{"prohibited input, no content", "PROHIBITED_INPUT_CONTENT", types.FinishReasonSafety, false},
		{"generated audio, with content", "GENERATED_AUDIO_SAFETY", types.FinishReasonSafety, true},
		{"generated content", "GENERATED_CONTENT_SAFETY", types.FinishReasonSafety, false},
		{"blocklist", "BLOCKLIST", types.FinishReasonSafety, false},
		{"need more input is a normal end", "NEED_MORE_INPUT", types.FinishReasonStop, false},
		{"no reason", "", types.FinishReasonStop, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newProtocolTestSession(t)
			raw := `{"serverContent":{"turnComplete":true`
			if tc.wire != "" {
				raw += `,"turnCompleteReason":"` + tc.wire + `"`
			}
			if tc.withModelTurn {
				raw += `,"modelTurn":{"parts":[{"text":"hi"}]}`
			}
			raw += `}}`
			var msg ServerMessage
			require.NoError(t, json.Unmarshal([]byte(raw), &msg))
			s.replyStarted()
			require.NoError(t, s.processServerMessage(&msg))

			done := finishing(drainChunks(t, s))
			require.Len(t, done, 1)
			assert.Equal(t, tc.want, *done[0].FinishReason)
			if tc.wire == "" {
				assert.NotContains(t, done[0].Metadata, "turn_complete_reason")
			} else {
				assert.Equal(t, tc.wire, done[0].Metadata["turn_complete_reason"])
			}
		})
	}
}
