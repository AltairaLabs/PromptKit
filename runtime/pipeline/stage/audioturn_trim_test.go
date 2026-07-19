package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

const trimTestSampleRate = 16000

// turnDuration reports the wall-clock-equivalent duration of a PCM16-mono turn.
func turnDuration(samples []byte) time.Duration {
	return time.Duration(len(samples)/2) * time.Second / time.Duration(trimTestSampleRate)
}

// runTurnStage feeds the supplied chunks through an AudioTurnStage and returns
// the emitted turns.
func runTurnStage(t *testing.T, cfg stage.AudioTurnConfig, feed func(in chan<- stage.StreamElement)) [][]byte {
	t.Helper()

	s, err := stage.NewAudioTurnStage(cfg)
	if err != nil {
		t.Fatalf("NewAudioTurnStage: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() { _ = s.Process(ctx, input, output) }()

	go func() {
		feed(input)
		input <- stage.StreamElement{EndOfStream: true}
		close(input)
	}()

	return collectTurns(output)
}

// TestAudioTurnStage_TrimsLeadingSilenceBeforeSpeech covers the STT cost and
// accuracy problem: between turns the stage buffers every silent chunk, so a
// caller who is quiet for several seconds prepends all of that silence to the
// next turn. That silence is billed per-second by STT providers and, with
// Whisper in particular, invites hallucinated text on near-silent input.
//
// The emitted turn must start shortly before speech onset, not at the start of
// the preceding silence.
func TestAudioTurnStage_TrimsLeadingSilenceBeforeSpeech(t *testing.T) {
	const chunkSamples = 1600 // 100 ms @ 16 kHz

	turns := runTurnStage(t, stage.DefaultAudioTurnConfig(), func(in chan<- stage.StreamElement) {
		feed := func(gen func(int) []byte, chunks int) {
			for range chunks {
				in <- makeAudioElement(gen(chunkSamples), trimTestSampleRate)
			}
		}
		feed(vadSilencePCM, 30) // 3.0 s of dead air before the caller speaks
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s trailing silence completes the turn
	})

	if len(turns) == 0 {
		t.Fatalf("expected at least one turn, got none")
	}

	got := turnDuration(turns[0])

	// Untrimmed this is ~5.2 s. Trimmed it should be the 1.0 s of speech plus
	// trailing silence and a small pre-roll — comfortably under 3 s.
	if got >= 3*time.Second {
		t.Errorf("leading silence not trimmed: turn is %v, expected well under 3s "+
			"(3.0s of dead air is still being sent to STT)", got)
	}

	// The speech itself must survive. Anything under 1 s means the onset was clipped.
	if got < time.Second {
		t.Errorf("turn is %v, shorter than the 1.0s of speech — onset was clipped", got)
	}
}

// TestAudioTurnStage_PreservesPreRollBeforeSpeechOnset guards against trimming
// exactly at the VAD's speech-start marker. VAD triggers a beat late, so cutting
// precisely at onset clips the leading consonant. A short pre-roll must be kept.
func TestAudioTurnStage_PreservesPreRollBeforeSpeechOnset(t *testing.T) {
	const chunkSamples = 1600 // 100 ms @ 16 kHz

	turns := runTurnStage(t, stage.DefaultAudioTurnConfig(), func(in chan<- stage.StreamElement) {
		feed := func(gen func(int) []byte, chunks int) {
			for range chunks {
				in <- makeAudioElement(gen(chunkSamples), trimTestSampleRate)
			}
		}
		feed(vadSilencePCM, 20) // 2.0 s dead air
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s trailing silence
	})

	if len(turns) == 0 {
		t.Fatalf("expected at least one turn, got none")
	}

	got := turnDuration(turns[0])

	// speech (1.0s) + trailing silence (1.2s) = 2.2s of content the turn must keep.
	// A pre-roll adds a little more, but only a little: the bounds must exclude
	// BOTH trimming flush at onset (<=2.2s, clips the leading consonant) and not
	// trimming at all (the untrimmed 4.2s would otherwise satisfy a lower bound).
	const content = 2200 * time.Millisecond
	const maxPreRoll = 3 * time.Second
	if got <= content {
		t.Errorf("turn is %v with no pre-roll before speech onset; expected >%v "+
			"so a late VAD trigger cannot clip the leading consonant", got, content)
	}
	if got >= maxPreRoll {
		t.Errorf("turn is %v, beyond %v: pre-roll should be a short margin, "+
			"not the whole preceding silence", got, maxPreRoll)
	}
}

// TestAudioTurnStage_ForwardsUntrimmedWhenNoSpeechDetected pins the safety
// property that makes this stage recoverable when VAD is wrong.
//
// The stage buffers unconditionally so that a VAD misread mis-cuts a turn rather
// than deleting audio — that is why a SimpleVAD misclassification was a
// segmentation bug and not silent data loss. Trimming must not weaken it: when
// VAD never reports speech at all, the buffer has to be forwarded whole, because
// the audio may well contain speech the VAD failed to see.
func TestAudioTurnStage_ForwardsUntrimmedWhenNoSpeechDetected(t *testing.T) {
	const chunkSamples = 1600 // 100 ms @ 16 kHz
	const chunks = 20         // 2.0 s

	turns := runTurnStage(t, stage.DefaultAudioTurnConfig(), func(in chan<- stage.StreamElement) {
		for range chunks {
			in <- makeAudioElement(vadSilencePCM(chunkSamples), trimTestSampleRate)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected the buffered audio to be forwarded at EndOfStream, got no turns")
	}

	got := turnDuration(turns[0])
	want := 2 * time.Second
	if got != want {
		t.Errorf("VAD never detected speech: expected the full %v forwarded untrimmed, got %v "+
			"— trimming must not discard audio the VAD may have misread", want, got)
	}
}
