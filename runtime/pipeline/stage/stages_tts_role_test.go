package stage_test

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingTTS captures every text handed to synthesis, so a test can assert on
// what the stage chose to speak rather than merely that it spoke.
type recordingTTS struct {
	mockTTSService

	mu     sync.Mutex
	spoken []string
}

func newRecordingTTS() *recordingTTS {
	r := &recordingTTS{}
	r.synthesizeFunc = func(_ context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		r.mu.Lock()
		r.spoken = append(r.spoken, text)
		r.mu.Unlock()
		return io.NopCloser(strings.NewReader(generateTestPCM(100))), nil
	}
	return r
}

func (r *recordingTTS) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.spoken))
	copy(out, r.spoken)
	return out
}

// messageElement builds a StreamElement carrying a role-tagged text message.
func messageElement(role, text string) stage.StreamElement {
	msg := types.Message{Role: role, Content: text}
	return stage.NewMessageElement(&msg)
}

// TestTTSStageSpeaksOnlyTheAssistant guards the speech-out contract: a stage
// that turns text into the voice the caller hears must speak the assistant's
// words and nothing else.
//
// The continuous multi-turn ProviderStage re-emits each turn's user transcript
// downstream before generating, so the UI and save stages see what was said
// right away. In the VAD voice topology TTS sits directly downstream of it, so
// an unfiltered stage synthesizes the caller's own question back at them before
// answering it. Roles other than assistant — user transcripts, tool results —
// are data for later stages, not lines to be read aloud.
func TestTTSStageSpeaksOnlyTheAssistant(t *testing.T) {
	svc := newRecordingTTS()
	ttsStage := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	inputs := []stage.StreamElement{
		messageElement("user", "what is the capital of france"),
		messageElement("assistant", "The capital of France is Paris."),
	}
	runStage(t, ttsStage, inputs, 5*time.Second)

	assert.Equal(t, []string{"The capital of France is Paris."}, svc.texts(),
		"only the assistant's reply may be spoken; the caller's own transcript must not be read back")
}

// recordingPlainTTS implements the narrower stage.TTSService used by the plain
// TTSStage, recording what it was asked to say.
type recordingPlainTTS struct {
	mu     sync.Mutex
	spoken []string
}

func (r *recordingPlainTTS) MIMEType() string { return "audio/pcm" }

func (r *recordingPlainTTS) Synthesize(_ context.Context, text string) ([]byte, error) {
	r.mu.Lock()
	r.spoken = append(r.spoken, text)
	r.mu.Unlock()
	return generateTestPCMAudio(100), nil
}

func (r *recordingPlainTTS) texts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.spoken))
	copy(out, r.spoken)
	return out
}

// TestPlainTTSStageSpeaksOnlyTheAssistant holds the plain stage to the same
// contract as the interruption-aware one. It is not on the VAD voice path
// today, but it carries the identical text extraction, so any topology that
// puts it downstream of a provider inherits the same read-the-caller-back bug.
func TestPlainTTSStageSpeaksOnlyTheAssistant(t *testing.T) {
	svc := &recordingPlainTTS{}
	ttsStage := stage.NewTTSStage(svc, stage.DefaultTTSConfig())

	runStage(t, ttsStage, []stage.StreamElement{
		messageElement("user", "what is the capital of france"),
		messageElement("assistant", "The capital of France is Paris."),
	}, 5*time.Second)

	assert.Equal(t, []string{"The capital of France is Paris."}, svc.texts(),
		"only the assistant's reply may be spoken")
}

// TestTTSStageDoesNotSpeakStreamingDeltas is the second half of why the VAD
// path double-spoke: the streaming ProviderStage emits each reply as both a run
// of incremental Text deltas (for live-text consumers) and a final assistant
// Message (the canonical record). The TTS provider takes a complete string and
// streams only audio out — synthesizing per-fragment deltas would be one HTTP
// request per token and choppy audio — so the deltas must not be spoken; the
// Message is. A complete Text element (no delta marker) is still spoken, which
// is the contract the rest of the TTS tests rely on.
func TestTTSStageDoesNotSpeakStreamingDeltas(t *testing.T) {
	svc := newRecordingTTS()
	ttsStage := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	delta := "Paris."
	deltaElem := stage.StreamElement{Text: &delta}
	deltaElem.Meta.StreamingDelta = true

	inputs := []stage.StreamElement{
		deltaElem,
		messageElement("assistant", "The capital of France is Paris."),
	}
	runStage(t, ttsStage, inputs, 5*time.Second)

	assert.Equal(t, []string{"The capital of France is Paris."}, svc.texts(),
		"streaming deltas must not be synthesized; only the complete assistant reply is spoken")
}

// TestTTSStageForwardsUnspokenMessages is the other half of the contract:
// declining to speak a message must not drop it. Downstream save and UI stages
// still need the user transcript, so it passes through untouched.
func TestTTSStageForwardsUnspokenMessages(t *testing.T) {
	svc := newRecordingTTS()
	ttsStage := stage.NewTTSStageWithInterruption(svc, stage.DefaultTTSStageWithInterruptionConfig())

	results := runStage(t, ttsStage, []stage.StreamElement{
		messageElement("user", "what is the capital of france"),
	}, 5*time.Second)

	var forwarded []string
	for _, elem := range results {
		if elem.Message != nil {
			forwarded = append(forwarded, elem.Message.Content)
		}
	}
	require.Contains(t, forwarded, "what is the capital of france",
		"a message the stage declines to speak must still reach downstream stages")
}
