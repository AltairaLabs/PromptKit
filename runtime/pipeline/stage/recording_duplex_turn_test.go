package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// duplexTurnElements runs one realtime assistant turn through the duplex
// provider stage's response handling and returns the elements it emits: a
// text chunk, then the turn-completing chunk, which carries the turn's last
// audio and finish reason.
func duplexTurnElements(t *testing.T) []StreamElement {
	t.Helper()
	s := newDuplexStageForUnit()
	out := make(chan StreamElement, 16)
	ctx := context.Background()
	require.NoError(t, s.handleResponseChunk(ctx, &providers.StreamChunk{Content: "Hello there"}, out))
	stop := "stop"
	require.NoError(t, s.handleResponseChunk(ctx, &providers.StreamChunk{
		MediaData:    &providers.StreamMediaData{Data: pcm(320)},
		FinishReason: &stop,
	}, out))
	close(out)
	var elems []StreamElement
	for e := range out {
		elems = append(elems, e)
	}
	return elems
}

// recordElements passes elems through an output-position RecordingStage and
// returns the store it wrote to.
func recordElements(t *testing.T, cfg RecordingStageConfig, elems []StreamElement) *fakeEventStore {
	t.Helper()
	store := &fakeEventStore{}
	cfg.Position = RecordingPositionOutput
	rs := NewRecordingStage(store, cfg)
	in := make(chan StreamElement, len(elems))
	for i := range elems {
		in <- elems[i]
	}
	close(in)
	out := make(chan StreamElement, len(elems))
	require.NoError(t, rs.Process(context.Background(), in, out))
	return store
}

// A realtime turn's assistant Message rides on the element that also carries
// EndOfStream. The recorder used to skip every control element, so a duplex
// recording held no assistant turns at all (promptarena#420).
func TestRecordingStage_RecordsDuplexAssistantTurn(t *testing.T) {
	elems := duplexTurnElements(t)
	last := elems[len(elems)-1]
	require.True(t, last.EndOfStream, "precondition: the turn ends on an EndOfStream element")
	require.NotNil(t, last.Message, "precondition: the turn's Message rides on that element")

	store := recordElements(t, RecordingStageConfig{IncludeAudio: true}, elems)

	created := store.filterByType(events.EventMessageCreated)
	require.Len(t, created, 1, "the assistant turn must be recorded")
	data, ok := created[0].Data.(*events.MessageCreatedData)
	require.True(t, ok)
	assert.Equal(t, roleAssistant, data.Role)
	assert.Equal(t, "Hello there", data.Content)

	// The turn's last audio chunk rides on the same element and must be
	// recorded too, or replay drops the end of every spoken reply.
	assert.Len(t, store.filterByType(events.EventAudioOutput), 1)
}

// Every content field of one element is recorded, in stream order: the text
// delta and audio the element streamed, then the message that completes it.
func TestRecordingStage_RecordsEveryContentFieldOfAnElement(t *testing.T) {
	elems := duplexTurnElements(t)
	last := elems[len(elems)-1]
	text := "!"
	last.Text = &text

	store := recordElements(t,
		RecordingStageConfig{IncludeAudio: true, IncludeStreamingText: true}, []StreamElement{last})

	var got []events.EventType
	for _, e := range store.snapshot() {
		got = append(got, e.Type)
	}
	assert.Equal(t, []events.EventType{
		events.EventMessageTextDelta, events.EventAudioOutput, events.EventMessageCreated,
	}, got)
}

// A bare control signal still records nothing.
func TestRecordingStage_BareControlElementRecordsNothing(t *testing.T) {
	store := recordElements(t, DefaultRecordingStageConfig(), []StreamElement{
		NewEndOfStreamElement(), {EndOfTurn: true}, {Interrupt: true},
	})
	assert.Empty(t, store.snapshot())
}
