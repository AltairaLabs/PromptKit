package stage_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// TestAudioTurnStage_DoesNotEmitSpeechlessRemainderAfterATurn covers trailing
// silence being emitted as a turn of its own.
//
// When a turn completes mid-stream the buffer resets, and whatever silence
// arrives afterwards accumulates until EndOfStream. emitRemainingAudio then
// forwards it on size alone, so a stream that ends with more silence than the
// turn threshold produces a second, speechless "turn".
//
// Downstream that is an STT call on pure silence: billed, slow, and a standing
// invitation for Whisper to hallucinate text that then flows on as if the
// speaker had said it. Observed as a phantom duplicate transcript in the
// voice-sales-assist example, where one utterance produced two.
func TestAudioTurnStage_DoesNotEmitSpeechlessRemainderAfterATurn(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.SilenceDuration = 500 * time.Millisecond

	const chunkSamples = 1600 // 100 ms

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		feed := func(gen func(int) []byte, chunks int) {
			for range chunks {
				in <- makeAudioElement(gen(chunkSamples), 16000)
			}
		}
		feed(vadSpeechPCM, 10)  // 1.0s speech
		feed(vadSilencePCM, 20) // 2.0s silence: completes the turn, leaves a tail
	})

	if len(turns) == 0 {
		t.Fatal("expected the spoken turn to be emitted, got none")
	}
	if len(turns) > 1 {
		durations := make([]string, len(turns))
		for i, turn := range turns {
			durations[i] = turnDuration(turn).String()
		}
		t.Errorf("one utterance produced %d turns (%v); the trailing silence after the "+
			"turn completed was emitted as a speechless turn of its own and would be "+
			"sent to STT", len(turns), durations)
	}
}

// TestAudioTurnStage_StillForwardsAudioWhenVADNeverFires guards the degrade-open
// property that the fix must not break.
//
// The stage forwards a buffer at EndOfStream even without detected speech, so a
// pre-recorded file whose VAD never triggers is not silently dropped. Suppressing
// speechless remainders must remain conditional on having already emitted a turn
// — if nothing has been emitted, the audio still has to come out.
func TestAudioTurnStage_StillForwardsAudioWhenVADNeverFires(t *testing.T) {
	const chunkSamples = 1600
	const chunks = 20 // 2.0s, comfortably over defaultMinAudioBytes

	turns := runTurnStage(t, stage.DefaultAudioTurnConfig(), func(in chan<- stage.StreamElement) {
		for range chunks {
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
	})

	if len(turns) == 0 {
		t.Error("no turn emitted when VAD never detected speech; audio the VAD may have " +
			"misread must still be forwarded")
	}
}
