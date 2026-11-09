package recording

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A user message's text normally lives in Parts with Content empty, which is
// how the recording stage writes it. Replay must show that text, not a blank
// user turn (#2567).
func TestReplayPlayer_ReadsUserTextFromParts(t *testing.T) {
	dir := t.TempDir()
	store, err := events.NewFileEventStore(dir)
	require.NoError(t, err)

	text := "Hello from parts"
	base := time.Now()
	for _, e := range []*events.Event{
		{
			Type: events.EventMessageCreated, Timestamp: base, SessionID: "s",
			Data: &events.MessageCreatedData{
				Role:  "user",
				Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}},
			},
		},
		{
			Type: events.EventMessageCreated, Timestamp: base.Add(10 * time.Millisecond), SessionID: "s",
			Data: &events.MessageCreatedData{Role: "assistant", Content: "reply", Index: 1},
		},
	} {
		require.NoError(t, store.Append(context.Background(), e))
	}
	require.NoError(t, store.Sync())
	store.Close()

	rec, err := Load(dir + "/s.jsonl")
	require.NoError(t, err)
	player, err := NewReplayPlayer(rec)
	require.NoError(t, err)

	state := player.GetStateAt(time.Second)
	var got []string
	for _, m := range state.Messages {
		got = append(got, m.Role+":"+m.Content)
	}
	assert.Equal(t, []string{"user:Hello from parts", "assistant:reply"}, got)
}
