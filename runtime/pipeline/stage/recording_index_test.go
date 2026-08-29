package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestRecordingIndex_IsTranscriptAbsolute pins that the recording route sets
// Index at all. Every recorded message.created previously carried Index 0,
// because recordMessageElement built the payload by hand and simply omitted
// the field — while the bus route set it. Same event type, different data.
//
// The load stage replays persisted history before this turn's messages
// (builder.go:375, the input recording stage at :390), so counting every
// message element is what makes the index match the persisted transcript.
func TestRecordingIndex_IsTranscriptAbsolute(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{
		Position:       RecordingPositionInput,
		SessionID:      "sess",
		ConversationID: "conv",
	})

	msgs := []types.Message{
		{Role: "user", Content: "turn 1 question", Source: "statestore"},
		{Role: "assistant", Content: "turn 1 answer", Source: "statestore"},
		{Role: "user", Content: "turn 2 question"},
		{Role: "assistant", Content: "turn 2 answer"},
	}

	in := make(chan StreamElement, len(msgs)+1)
	for i := range msgs {
		elem := NewMessageElement(&msgs[i])
		elem.Meta.FromHistory = msgs[i].Source == "statestore"
		in <- elem
	}
	close(in)

	out := make(chan StreamElement, len(msgs)+4)
	require.NoError(t, rs.Process(context.Background(), in, out))
	for range out { //nolint:revive // draining
	}

	recorded := store.filterByType(events.EventMessageCreated)
	require.Len(t, recorded, len(msgs))

	for i, evt := range recorded {
		data, ok := evt.Data.(*events.MessageCreatedData)
		require.Truef(t, ok, "event %d: want *MessageCreatedData, got %T", i, evt.Data)
		assert.Equalf(t, i, data.Index,
			"event %d (%q) must carry its transcript-absolute index", i, data.Content)
		assert.Equal(t, msgs[i].Content, data.Content)
	}
}

// TestRecordingIndex_RetainsBinary is the recording half of the one deliberate
// difference between the routes. MessageBroadcastStage strips; this does not.
func TestRecordingIndex_RetainsBinary(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput})

	raw := "AAAABBBBCCCC"
	msg := types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{{
			Type:  "image",
			Media: &types.MediaContent{Data: &raw, MIMEType: "image/png"},
		}},
	}

	in := make(chan StreamElement, 2)
	in <- NewMessageElement(&msg)
	close(in)

	out := make(chan StreamElement, 4)
	require.NoError(t, rs.Process(context.Background(), in, out))
	for range out { //nolint:revive // draining
	}

	recorded := store.filterByType(events.EventMessageCreated)
	require.Len(t, recorded, 1)
	data, ok := recorded[0].Data.(*events.MessageCreatedData)
	require.True(t, ok)
	require.Len(t, data.Parts, 1)
	require.NotNil(t, data.Parts[0].Media)
	require.NotNil(t, data.Parts[0].Media.Data,
		"recording keeps binary for lossless replay")
	assert.Equal(t, raw, *data.Parts[0].Media.Data)
}
