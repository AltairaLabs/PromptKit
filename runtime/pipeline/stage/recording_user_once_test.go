package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordedTranscript returns role:text for each message.created in store.
func recordedTranscript(t *testing.T, store *fakeEventStore) []string {
	t.Helper()
	var got []string
	for _, evt := range store.filterByType(events.EventMessageCreated) {
		d, ok := evt.Data.(*events.MessageCreatedData)
		require.True(t, ok)
		got = append(got, d.Role+":"+d.GetContent())
	}
	return got
}

// The turn's user message passes the input recorder and then, re-emitted by
// the provider stage beside the reply, the output recorder. It must be
// recorded once, or replay shows the user's turn twice (#2567).
func TestRecording_UserMessageRecordedOncePerTurn(t *testing.T) {
	store := &fakeEventStore{}
	pipe, err := NewPipelineBuilder().Chain(
		NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput}),
		NewProviderStage(mock.NewProvider("m", "m-model", false), nil, nil, &ProviderConfig{}),
		NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput}),
	).Build()
	require.NoError(t, err)

	user := realMessage(roleUser, "hello")
	_, err = pipe.ExecuteSync(context.Background(), NewMessageElement(&user))
	require.NoError(t, err)

	assert.Equal(t,
		[]string{"user:hello", "assistant:Mock response from m model m-model"},
		recordedTranscript(t, store))
	idx := []int{}
	for _, evt := range store.filterByType(events.EventMessageCreated) {
		idx = append(idx, evt.Data.(*events.MessageCreatedData).Index)
	}
	assert.Equal(t, []int{0, 1}, idx, "positions stay transcript-absolute")
}

// A user message only the output recorder sees — a duplex or STT transcript,
// produced downstream of the input recorder — is still recorded there.
func TestRecording_OutputRecordsUserTranscriptItAloneSees(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput})

	msgs := []types.Message{
		{Role: roleUser, Content: "spoken words"},
		{Role: "assistant", Content: "answer"},
	}
	in := make(chan StreamElement, len(msgs))
	for i := range msgs {
		in <- NewMessageElement(&msgs[i])
	}
	close(in)
	out := make(chan StreamElement, len(msgs)+2)
	require.NoError(t, rs.Process(context.Background(), in, out))
	for range out { //nolint:revive // draining
	}

	assert.Equal(t, []string{"user:spoken words", "assistant:answer"}, recordedTranscript(t, store))
}
