package openai

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A context note is a system message added to the conversation, with no
// response asked for: the model reads it with the turns that follow and says
// nothing to it. The provider's session forwards it.
func TestRealtimeContextNote_AddsASystemItemAndAsksForNoResponse(t *testing.T) {
	received := make(chan map[string]any, 16)
	srv := newClientEventServer(t, received)
	session := openProviderSession(t, srv, audioStreamRequest())
	noter, ok := session.(providers.ContextNoter)
	require.True(t, ok, "the provider's session takes context notes")

	ctx := context.Background()
	require.NoError(t, noter.AddContextNote(ctx, "Your previous reply was withheld."))
	require.NoError(t, session.SendSystemContext(ctx, "marker"))

	var after []map[string]any
	timeout := time.After(5 * time.Second)
	seenSetup := false
	for {
		select {
		case ev := <-received:
			if !seenSetup && ev["type"] == realtimeEventSessionUpdate {
				seenSetup = true // the session's own configuration
				continue
			}
			if ev["type"] == realtimeEventSessionUpdate {
				require.Len(t, after, 1, "only the note's item before the marker: %v", after)
				item, _ := after[0]["item"].(map[string]any)
				assert.Equal(t, realtimeEventItemCreate, after[0]["type"])
				assert.Equal(t, map[string]any{
					"type": "message", "role": "system",
					"content": []any{map[string]any{"type": "input_text", "text": "Your previous reply was withheld."}},
				}, item)
				return
			}
			after = append(after, ev)
		case <-timeout:
			require.FailNow(t, "the marker never arrived", "%v", after)
		}
	}
}

// A closed session takes no note.
func TestRealtimeContextNote_RefusedWhereItCannotBeAdded(t *testing.T) {
	closed := &RealtimeSession{closed: true}
	assert.EqualError(t, closed.AddContextNote(context.Background(), "note"), errSessionClosed)
}
