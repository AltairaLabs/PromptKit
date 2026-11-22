package voice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// blipTurn runs an AudioTurnStage over a turn the VAD only ever called
// Starting (a cough, a door, a short word), sharing the handler h, and
// returns what the stage emitted.
func blipTurn(t *testing.T, h *audio.InterruptionHandler) []core.StreamElement {
	t.Helper()
	return blipTurnWith(t, h, true)
}

func blipTurnWith(t *testing.T, h *audio.InterruptionHandler, turnBased bool) []core.StreamElement {
	t.Helper()
	config := voice.DefaultAudioTurnConfig()
	config.EmitEndOfTurn = turnBased
	config.MinSpeechDuration = 50 * time.Millisecond
	config.SilenceDuration = 100 * time.Millisecond
	config.VAD = &mockVADAnalyzer{states: []audio.VADState{audio.VADStateStarting, audio.VADStateQuiet}}
	config.InterruptionHandler = h
	s, err := voice.NewAudioTurnStage(config)
	require.NoError(t, err)
	// 100ms loud, then 100ms quiet: one turn (the mock VAD restarts its
	// script when the turn resets it, so more audio would be a second blip).
	var inputs []core.StreamElement
	for i := 0; i < 2; i++ {
		inputs = append(inputs, makeAudioElement(generateTestPCMAudio(3200), 16000))
	}
	return runStage(t, s, inputs, 2*time.Second)
}

// sttOf is an STT stage sharing h that transcribes every turn as text.
func sttOf(h *audio.InterruptionHandler, text string) *voice.STTStage {
	cfg := voice.DefaultSTTStageConfig()
	cfg.Retry.MaxAttempts = 1
	cfg.InterruptionHandler = h
	return voice.NewSTTStage(&mockSTTService{
		transcribeFunc: func(context.Context, base.STTRequest) (base.STTResponse, error) {
			return base.STTResponse{Text: text}, nil
		},
	}, cfg)
}

// A turn the VAD never called Speaking does not interrupt a reply when it
// ends: it may be a cough or a door. It carries the barge-in to the STT stage
// instead, which interrupts only once the turn turns out to be words.
func TestAudioTurnStage_UnvoicedTurnDuringAReplyWaitsForItsTranscript(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse() // a reply is pending

	got := blipTurn(t, h)

	require.NotEmpty(t, got)
	for _, e := range got {
		assert.False(t, e.Interrupt, "the turn does not interrupt before it is transcribed")
	}
	require.NotNil(t, got[0].Audio)
	assert.True(t, got[0].Meta.BargeInIfTranscribed, "the turn's audio carries the barge-in to the STT stage")
	assert.False(t, h.WasInterrupted())

	// With the bot idle there is nothing to barge in on.
	idle := blipTurn(t, audio.NewInterruptionHandler(audio.InterruptionImmediate, nil))
	require.NotEmpty(t, idle)
	assert.False(t, idle[0].Meta.BargeInIfTranscribed, "no reply, no barge-in")

	// A stage that is not turn-based owes no reply per turn.
	h = audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	untimed := blipTurnWith(t, h, false)
	require.NotEmpty(t, untimed)
	for _, e := range untimed {
		assert.False(t, e.Interrupt)
		assert.False(t, e.Meta.BargeInIfTranscribed, "not turn-based: no barge-in on turn end")
	}
}

// Noise that transcribes to nothing leaves the reply alone: no Interrupt, and
// the reply is still owed (the bot still counts as responding) after the
// noise's own turn ended.
func TestSTTStage_UntranscribedBargeInLeavesTheReply(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()     // the pending reply
	got := blipTurn(t, h) // the noise: its turn begins a response too
	require.NotEmpty(t, got)

	out := runStage(t, sttOf(h, "  "), got, 2*time.Second)

	for _, e := range out {
		assert.False(t, e.Interrupt, "noise does not interrupt the reply")
	}
	assert.True(t, h.IsBotSpeaking(), "the pending reply is still owed")
	assert.False(t, h.WasInterrupted())
	h.EndResponse() // the pending reply's turn ends
	assert.False(t, h.IsBotSpeaking(), "then nothing is owed: the noise's turn ended its own")
}

// A turn carrying the barge-in that transcribes to words interrupts the reply
// at once, ahead of its transcript, and the reply owed to the new turn
// survives the Reset the Interrupt triggers downstream.
func TestSTTStage_TranscribedBargeInInterruptsAheadOfTheTranscript(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	got := blipTurn(t, h)
	require.NotEmpty(t, got)

	out := runStage(t, sttOf(h, "that's that"), got, 2*time.Second)

	require.GreaterOrEqual(t, len(out), 2)
	assert.True(t, out[0].Interrupt, "the Interrupt goes ahead of the transcript")
	require.NotNil(t, out[1].Text)
	assert.Equal(t, "that's that", *out[1].Text)
	assert.False(t, out[1].Meta.BargeInIfTranscribed, "the transcript does not carry the barge-in on")
	assert.True(t, h.WasInterrupted())
	h.Reset() // the TTS stage handles the Interrupt
	assert.True(t, h.IsBotSpeaking(), "the reply to the interrupting turn is still owed")
}

// A turn carrying the barge-in whose earlier reply finished while it was being
// transcribed interrupts nothing: no Interrupt reaches the provider stage or
// the caller.
func TestSTTStage_BargeInOnAReplyThatFinishedFiresNothing(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse() // the reply in progress
	got := blipTurn(t, h)
	require.NotEmpty(t, got)
	require.True(t, got[0].Meta.BargeInIfTranscribed)
	h.EndResponse() // the reply ended and played out before STT answered

	out := runStage(t, sttOf(h, "thanks"), got, 2*time.Second)

	require.NotEmpty(t, out)
	for _, e := range out {
		assert.False(t, e.Interrupt, "there was nothing left to interrupt")
	}
	require.NotNil(t, out[0].Text)
	assert.Equal(t, "thanks", *out[0].Text)
	assert.False(t, h.WasInterrupted())
}
