package persist

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Audio a cue stage played is recorded as assistant audio generated from the
// cue and tagged with the cue's kind; model audio beside it keeps "model" and
// no kind.
func TestRecordingStage_CueAudioIsTaggedWithItsKind(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput, IncludeAudio: true})

	model := core.StreamElement{Audio: &core.AudioData{Samples: []byte{1, 2}, SampleRate: 24000, Channels: 1}}
	cued := core.StreamElement{Audio: &core.AudioData{Samples: []byte{3, 4}, SampleRate: 24000, Channels: 1}}
	cued.Meta.CueAudio = &core.Cue{Kind: core.CueGuardrailMessage, Text: "one moment"}
	runTestStage(t, rs, []core.StreamElement{model, cued})

	got := store.filterByType(events.EventAudioOutput)
	require.Len(t, got, 2)
	var from, kinds []string
	for _, e := range got {
		d := e.Data.(*events.AudioEventData)
		from = append(from, d.GeneratedFrom)
		kinds = append(kinds, d.CueKind)
	}
	assert.Equal(t, []string{"model", "cue"}, from)
	assert.Equal(t, []string{"", "guardrail_message"}, kinds)
}

// The input recorder marks the user message it recorded, and the output
// recorder does not record a marked message again, so a turn's user message is
// recorded once. The caller's message is left unmarked.
func TestRecordingStage_OutputSkipsWhatTheInputRecorded(t *testing.T) {
	store := &fakeEventStore{}
	in := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput})
	out := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput})

	user := types.Message{Role: core.RoleUser, Content: "hello"}
	forwarded := runTestStage(t, in, []core.StreamElement{core.NewMessageElement(&user)})
	require.Len(t, forwarded, 1)
	assert.Equal(t, "recorded_input", forwarded[0].Message.Source)
	assert.Empty(t, user.Source, "the recorder marks its own copy")

	reply := types.Message{Role: core.RoleAssistant, Content: "hi"}
	runTestStage(t, out, []core.StreamElement{forwarded[0], core.NewMessageElement(&reply)})

	var got []string
	var idx []int
	for _, e := range store.filterByType(events.EventMessageCreated) {
		d := e.Data.(*events.MessageCreatedData)
		got = append(got, d.Role+":"+d.GetContent())
		idx = append(idx, d.Index)
	}
	assert.Equal(t, []string{"user:hello", "assistant:hi"}, got)
	assert.Equal(t, []int{0, 1}, idx, "the skipped copy still counts for position")
}

// Audio without a rate or channel count is recorded as 16 kHz mono, and its
// duration is derived from its bytes at that rate.
func TestRecordingStage_AudioWithoutFormatIs16kMono(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput, IncludeAudio: true})

	runTestStage(t, rs, []core.StreamElement{{Audio: &core.AudioData{Samples: make([]byte, 3200)}}})

	got := store.filterByType(events.EventAudioInput)
	require.Len(t, got, 1)
	assert.Equal(t, events.AudioMetadata{
		SampleRate: 16000, Channels: 1, Encoding: "pcm_linear16", DurationMs: 100,
	}, got[0].Data.(*events.AudioEventData).Metadata)
}

// An image at the input position is recorded as the user's image input.
func TestRecordingStage_InputImageIsTheUsers(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput, IncludeImages: true})

	img := &core.ImageData{Data: []byte{9, 8, 7}, MIMEType: "image/png", Width: 2, Height: 3, Format: "png"}
	runTestStage(t, rs, []core.StreamElement{{Image: img}})

	require.Len(t, store.snapshot(), 1)
	evt := store.snapshot()[0]
	assert.Equal(t, events.EventImageInput, evt.Type)
	assert.Equal(t, &events.ImageEventData{
		Direction: "input",
		Actor:     "user",
		Payload:   events.BinaryPayload{InlineData: []byte{9, 8, 7}, MIMEType: "image/png", Size: 3},
		Metadata:  events.VideoMetadata{Width: 2, Height: 3, Encoding: "png"},
	}, evt.Data)
}

// failingEventStore fails every Append.
type failingEventStore struct {
	fakeEventStore
	calls int
}

func (f *failingEventStore) Append(context.Context, *events.Event) error {
	f.calls++
	return errors.New("disk full")
}

// A store that fails an append does not stop the element: it is forwarded
// unchanged.
func TestRecordingStage_FailedAppendStillForwards(t *testing.T) {
	store := &failingEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionOutput})

	msg := types.Message{Role: core.RoleAssistant, Content: "hi"}
	elem := core.NewMessageElement(&msg)
	elem.Timestamp = time.Unix(10, 0)
	out := runTestStage(t, rs, []core.StreamElement{elem})

	assert.Equal(t, 1, store.calls)
	require.Len(t, out, 1)
	assert.Equal(t, "hi", out[0].Message.Content)
	assert.Equal(t, time.Unix(10, 0), out[0].Timestamp)
}

// A bare control signal records nothing.
func TestRecordingStage_BareControlElementRecordsNothing(t *testing.T) {
	store := &fakeEventStore{}
	cfg := DefaultRecordingStageConfig()
	cfg.Position = RecordingPositionOutput
	runTestStage(t, NewRecordingStage(store, cfg), []core.StreamElement{
		core.NewEndOfStreamElement(), {EndOfTurn: true}, {Interrupt: true},
	})
	assert.Empty(t, store.snapshot())
}

// Video is opt-in: a video element is not recorded without IncludeVideo, and
// with it, only the video element is.
func TestRecordingStage_VideoIsOptIn(t *testing.T) {
	video := core.StreamElement{Video: &core.VideoData{Data: []byte{1}, MIMEType: "video/h264", Width: 4, Height: 2}}
	text := core.NewTextElement("x")

	off := &fakeEventStore{}
	runTestStage(t, NewRecordingStage(off, RecordingStageConfig{Position: RecordingPositionOutput}),
		[]core.StreamElement{video})
	assert.Empty(t, off.snapshot())

	on := &fakeEventStore{}
	runTestStage(t, NewRecordingStage(on, RecordingStageConfig{Position: RecordingPositionOutput, IncludeVideo: true}),
		[]core.StreamElement{video, text})
	require.Len(t, on.snapshot(), 1)
	assert.Equal(t, events.EventVideoFrame, on.snapshot()[0].Type)
}

// The output recorder marks nothing: the message it forwards keeps its Source.
func TestRecordingStage_OutputRecorderForwardsTheMessageUnmarked(t *testing.T) {
	rs := NewRecordingStage(&fakeEventStore{}, RecordingStageConfig{Position: RecordingPositionOutput})
	msg := types.Message{Role: core.RoleUser, Content: "spoken"}
	out := runTestStage(t, rs, []core.StreamElement{core.NewMessageElement(&msg)})
	require.Len(t, out, 1)
	assert.Equal(t, "", out[0].Message.Source)
}

// Stereo audio's duration counts both channels' bytes: 3200 bytes of 16 kHz
// stereo is 50 ms.
func TestRecordingStage_StereoAudioDuration(t *testing.T) {
	store := &fakeEventStore{}
	rs := NewRecordingStage(store, RecordingStageConfig{Position: RecordingPositionInput, IncludeAudio: true})
	runTestStage(t, rs, []core.StreamElement{{Audio: &core.AudioData{
		Samples: make([]byte, 3200), SampleRate: 16000, Channels: 2,
	}}})

	got := store.filterByType(events.EventAudioInput)
	require.Len(t, got, 1)
	assert.Equal(t, int64(50), got[0].Data.(*events.AudioEventData).Metadata.DurationMs)
}
