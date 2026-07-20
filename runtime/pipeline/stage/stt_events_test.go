package stage

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTranscriptSTT returns a set transcript for any audio.
type fixedTranscriptSTT struct{ text string }

func (s *fixedTranscriptSTT) Name() string                      { return "fixed-transcript" }
func (s *fixedTranscriptSTT) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (s *fixedTranscriptSTT) Pricing() *base.PricingDescriptor  { return nil }
func (s *fixedTranscriptSTT) Validate() error                   { return nil }
func (s *fixedTranscriptSTT) Init(context.Context) error        { return nil }
func (s *fixedTranscriptSTT) HealthCheck(context.Context) error { return nil }
func (s *fixedTranscriptSTT) Close() error                      { return nil }
func (s *fixedTranscriptSTT) Transcribe(
	context.Context, base.STTRequest,
) (base.STTResponse, error) {
	return base.STTResponse{Text: s.text}, nil
}

// TestSTTStage_EmitsTranscriptionEvent covers EventAudioTranscription having no
// producer.
//
// The event type is declared (events/types.go), its payload is fully specified
// (AudioTranscriptionData), and two consumers already read it: session export
// writes it out as subtitles, and annotated_session queries it by type. Nothing
// emits it, so both consumers see nothing and any subscriber waits forever
// without error.
//
// The voice-sales-assist example threads a manual callback through its stage
// graph for exactly this reason, noting that subscribing "would silently never
// fire".
func TestSTTStage_EmitsTranscriptionEvent(t *testing.T) {
	const transcript = "I'd like a home insurance quote"

	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.AudioTranscriptionData, 4)
	bus.Subscribe(events.EventAudioTranscription, func(e *events.Event) {
		if data, ok := e.Data.(*events.AudioTranscriptionData); ok {
			received <- data
		}
	})

	emitter := events.NewEmitter(bus, "exec-1", "session-1", "conv-1")
	s := NewSTTStageWithEmitter(
		&fixedTranscriptSTT{text: transcript},
		DefaultSTTStageConfig(),
		emitter,
	)

	input := make(chan StreamElement, 1)
	input <- StreamElement{Audio: &AudioData{
		Samples:    make([]byte, 32000), // 1s @ 16kHz PCM16
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
	}}
	close(input)

	output := make(chan StreamElement, 8)
	require.NoError(t, s.Process(context.Background(), input, output))
	for range output { //nolint:revive // drain
	}

	select {
	case data := <-received:
		assert.Equal(t, transcript, data.Text, "the event should carry the transcribed text")
		assert.True(t, data.IsFinal, "a completed STT turn is a final transcription")
	case <-time.After(2 * time.Second):
		t.Error("no EventAudioTranscription was emitted for a completed transcription; " +
			"the event type is declared and consumed but has no producer, so subscribers " +
			"and session export silently see nothing")
	}
}

// TestSTTStage_WithoutEmitterStillTranscribes guards that the emitter stays
// optional: an STTStage built without one must behave exactly as before.
func TestSTTStage_WithoutEmitterStillTranscribes(t *testing.T) {
	const transcript = "no emitter configured"

	s := NewSTTStage(&fixedTranscriptSTT{text: transcript}, DefaultSTTStageConfig())

	input := make(chan StreamElement, 1)
	input <- StreamElement{Audio: &AudioData{
		Samples:    make([]byte, 32000),
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
	}}
	close(input)

	output := make(chan StreamElement, 8)
	require.NoError(t, s.Process(context.Background(), input, output))

	var sawText bool
	for elem := range output {
		if elem.Text != nil && *elem.Text == transcript {
			sawText = true
		}
	}
	assert.True(t, sawText, "transcription must still work with no emitter wired")
}
