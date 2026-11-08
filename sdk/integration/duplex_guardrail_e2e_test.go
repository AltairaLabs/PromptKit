package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// guardedDuplexPackJSON blocks "forbidden" in the model's output and "secret"
// in the user's input.
const guardedDuplexPackJSON = `{
	"id": "duplex-guarded", "version": "1.0.0", "description": "guardrails in a realtime session",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "system_template": "Be helpful.",
		"validators": [
			{"type": "banned_words", "params": {"words": ["forbidden"]}, "message": "Output blocked."},
			{"type": "banned_words", "params": {"words": ["secret"], "direction": "input"}, "message": "Input blocked."}
		]}}
}`

// TestDuplexGuardrails_BlockedTurnsKeepTheirPlace runs a realtime session in
// which one input and one reply are blocked. Each blocked turn's complete
// chunk says safety, nothing blocked reaches the caller, and the recorded and
// saved transcripts hold every turn once, in the order the user had them.
func TestDuplexGuardrails_BlockedTurnsKeepTheirPlace(t *testing.T) {
	st := statestore.NewMemoryStore()
	conv, store := openRecordedDuplex(t, guardedDuplexPackJSON,
		scriptedDuplexProvider("reply one", "this is forbidden", "reply three"),
		sdk.WithStateStore(st))
	ch, err := conv.Response()
	require.NoError(t, err)

	type turn struct {
		in, want, finish string
	}
	turns := []turn{
		{"first", "reply one", "stop"},
		{"my secret", "Input blocked.", "safety"},
		{"second", "Output blocked.", "safety"},
		{"third", "reply three", "stop"},
	}
	for _, tc := range turns {
		chunks := duplexTurn(t, conv, ch, tc.in)
		assert.Equal(t, tc.finish, chunks[len(chunks)-1].Metadata["finish_reason"], "turn %q", tc.in)
		var text strings.Builder
		for _, c := range chunks {
			text.WriteString(c.Delta)
		}
		assert.NotContains(t, text.String(), "forbidden", "blocked output reached the caller on %q", tc.in)
	}
	sessionID := conv.ID()
	require.NoError(t, conv.Close())

	_, transcript := recordedTranscript(t, store, sessionID)
	state, err := st.Load(context.Background(), sessionID)
	require.NoError(t, err)
	var saved []transcriptEntry
	for _, m := range state.Messages {
		saved = append(saved, transcriptEntry{m.Role, m.GetContent()})
	}
	var want []transcriptEntry
	for _, tc := range turns {
		want = append(want, transcriptEntry{"user", tc.in}, transcriptEntry{"assistant", tc.want})
	}
	assert.Equal(t, want, transcript, "recorded transcript")
	assert.Equal(t, want, saved, "saved history")
}
