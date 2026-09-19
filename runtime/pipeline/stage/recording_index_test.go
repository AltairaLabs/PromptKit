package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

	// Built with realMessage so the user turns carry text in Parts with Content
	// empty, as real ones do — see the note on realMessage.
	msgs := []types.Message{
		historyOf(realMessage("user", "turn 1 question")),
		historyOf(realMessage("assistant", "turn 1 answer")),
		realMessage("user", "turn 2 question"),
		realMessage("assistant", "turn 2 answer"),
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

	// Only the two NEW messages are recorded — replayed history is counted for
	// position but not re-recorded (#1879) — and they carry the transcript
	// positions that follow the history, not 0 and 1.
	recorded := store.filterByType(events.EventMessageCreated)
	require.Len(t, recorded, 2)

	wantIndex := []int{2, 3}
	wantText := []string{"turn 2 question", "turn 2 answer"}
	for i, evt := range recorded {
		data, ok := evt.Data.(*events.MessageCreatedData)
		require.Truef(t, ok, "event %d: want *MessageCreatedData, got %T", i, evt.Data)
		assert.Equalf(t, wantIndex[i], data.Index,
			"event %d (%q) must carry its transcript-absolute index", i, data.GetContent())
		assert.Equal(t, wantText[i], data.GetContent(),
			"the recorded event must carry the same readable text as the message")
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

// TestRecordingIndex_ResetsPerExecution pins that the index means something on
// turn 2.
//
// A pipeline is built once and re-executed per turn (sdk/sdk.go:687), so a
// counter held on the stage keeps climbing while the load stage replays the
// transcript — turn 2 of a 2-message history recorded indices 2..5 instead of
// 0..3. The single-Process test above cannot catch that.
func TestRecordingIndex_ResetsPerExecution(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput})

	runTurn := func(msgs []types.Message) {
		in := make(chan StreamElement, len(msgs)+1)
		for i := range msgs {
			in <- NewMessageElement(&msgs[i])
		}
		close(in)
		out := make(chan StreamElement, len(msgs)+4)
		require.NoError(t, rs.Process(context.Background(), in, out))
		for range out { //nolint:revive // draining
		}
	}

	// Turn 1: one user message.
	runTurn([]types.Message{realMessage("user", "first")})

	// Turn 2: turn 1 replayed as history, plus a new message.
	runTurn([]types.Message{
		historyOf(realMessage("user", "first")),
		historyOf(realMessage("assistant", "answer")),
		realMessage("user", "second"),
	})

	// One new message per turn: turn 1's "first", then turn 2's "second".
	// History is not re-recorded, so two events total.
	recorded := store.filterByType(events.EventMessageCreated)
	require.Len(t, recorded, 2)

	// The counter is per-execution, so turn 2's message indexes from the
	// transcript (position 2, after the two replayed) rather than continuing
	// from turn 1's high-water mark.
	turn1, ok := recorded[0].Data.(*events.MessageCreatedData)
	require.True(t, ok)
	turn2, ok := recorded[1].Data.(*events.MessageCreatedData)
	require.True(t, ok)

	assert.Equal(t, 0, turn1.Index, "turn 1's message is at transcript position 0")
	assert.Equal(t, 2, turn2.Index,
		"turn 2's message must index from the transcript, not from turn 1")
}

// historyOf marks a message as replayed from the state store, the way
// StateStoreLoadStage stamps it.
func historyOf(m types.Message) types.Message {
	m.Source = "statestore"
	return m
}

// TestRecordingStage_DoesNotReRecordHistory is the fix for #1879.
//
// The load stage runs before the input RecordingStage (builder.go), so replayed
// history flowed through it every turn and was appended again. An N-turn
// recording held turn 1 N times, and recording/replay.go appends every
// message.created in time order — so a replayed transcript repeated its early
// messages.
//
// Each message is recorded once, on the turn it was new. The union across turns
// is still the complete transcript, so replay reconstructs it correctly without
// any change to the readers.
func TestRecordingStage_DoesNotReRecordHistory(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput})

	runTurn := func(msgs []types.Message) {
		in := make(chan StreamElement, len(msgs)+1)
		for i := range msgs {
			in <- NewMessageElement(&msgs[i])
		}
		close(in)
		out := make(chan StreamElement, len(msgs)+4)
		require.NoError(t, rs.Process(context.Background(), in, out))
		for range out { //nolint:revive // draining
		}
	}

	// Turn 1: one new user message.
	runTurn([]types.Message{realMessage("user", "first")})

	// Turn 2: turn 1 replayed as history, plus one new message.
	runTurn([]types.Message{
		historyOf(realMessage("user", "first")),
		historyOf(realMessage("assistant", "answer")),
		realMessage("user", "second"),
	})

	recorded := store.filterByType(events.EventMessageCreated)

	var contents []string
	for _, evt := range recorded {
		data, ok := evt.Data.(*events.MessageCreatedData)
		require.True(t, ok)
		contents = append(contents, data.GetContent())
	}

	assert.Equal(t, []string{"first", "second"}, contents,
		"each message must be recorded once, on the turn it was new")

	// Index still counts history, so positions stay transcript-absolute.
	require.Len(t, recorded, 2)
	first, _ := recorded[0].Data.(*events.MessageCreatedData)
	second, _ := recorded[1].Data.(*events.MessageCreatedData)
	assert.Equal(t, 0, first.Index)
	assert.Equal(t, 2, second.Index,
		"the new message sits at transcript position 2, after the two replayed")
}
