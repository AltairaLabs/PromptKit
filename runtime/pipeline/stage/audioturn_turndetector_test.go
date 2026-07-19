package stage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// TestAudioTurnStage_TurnDetectorDoesNotShatterContinuousSpeech covers a
// TurnDetector being fed audio but never the VAD states it needs.
//
// AudioTurnStage calls turnDetector.ProcessAudio and IsUserSpeaking, but never
// ProcessVADState — the only caller of that is the interruption handler. A
// SilenceDetector decides speaking/silence entirely inside ProcessVADState, so
// left undriven its IsUserSpeaking is permanently false, and shouldCompleteTurn's
// detector vote ("not speaking, so the turn is over") fires on every evaluation.
//
// A continuous utterance is then cut at MinSpeechDuration over and over: one
// sentence becomes many fragments, each its own STT call and transcript.
//
// This is the configuration the WithTurnDetector doc comment recommends.
func TestAudioTurnStage_TurnDetectorDoesNotShatterContinuousSpeech(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.TurnDetector = audio.NewSilenceDetector(800 * time.Millisecond)

	const chunkSamples = 1600 // 100 ms

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		// 5s of continuous speech with no pause to segment on.
		for range 50 {
			in <- makeAudioElement(vadSpeechPCM(chunkSamples), 16000)
		}
	})

	if len(turns) > 1 {
		durations := make([]time.Duration, len(turns))
		for i, turn := range turns {
			durations[i] = turnDuration(turn)
		}
		t.Fatalf("continuous 5s utterance split into %d turns (%v) with a TurnDetector configured; "+
			"the detector never receives VAD states, so it always reports not-speaking",
			len(turns), durations)
	}
}

// TestAudioTurnStage_TurnDetectorMatchesNoDetectorOnContinuousSpeech pins that
// adding a detector does not change segmentation of audio that has no turn
// boundary in it.
//
// A detector is meant to refine when a turn ends, not to invent boundaries in
// unbroken speech. Comparing against the no-detector baseline catches a detector
// that is being driven incorrectly rather than merely producing a different
// answer.
func TestAudioTurnStage_TurnDetectorMatchesNoDetectorOnContinuousSpeech(t *testing.T) {
	const chunkSamples = 1600

	feed := func(in chan<- stage.StreamElement) {
		for range 50 {
			in <- makeAudioElement(vadSpeechPCM(chunkSamples), 16000)
		}
	}

	withDetector := stage.DefaultAudioTurnConfig()
	withDetector.TurnDetector = audio.NewSilenceDetector(800 * time.Millisecond)

	got := len(runTurnStage(t, withDetector, feed))
	want := len(runTurnStage(t, stage.DefaultAudioTurnConfig(), feed))

	if got != want {
		t.Errorf("continuous speech produced %d turns with a TurnDetector but %d without; "+
			"a detector should not invent boundaries in unbroken speech", got, want)
	}
}

// spyTurnDetector records the VAD states delivered to it.
//
// Asserting on a real detector's IsUserSpeaking after a run cannot prove the
// wiring: EndOfStream resets the stage, which resets the detector, so the flag
// is legitimately false by the time the test reads it. Recording the delivered
// states observes the wiring itself rather than a state that is correctly
// cleared afterwards.
type spyTurnDetector struct {
	mu     sync.Mutex
	states []audio.VADState
	audioN int
}

func (d *spyTurnDetector) Name() string { return "spy" }

func (d *spyTurnDetector) ProcessAudio(_ context.Context, _ []byte) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.audioN++
	return false, nil
}

func (d *spyTurnDetector) ProcessVADState(_ context.Context, state audio.VADState) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.states = append(d.states, state)
	return false, nil
}

func (d *spyTurnDetector) IsUserSpeaking() bool { return true }

func (d *spyTurnDetector) Reset() {}

func (d *spyTurnDetector) sawSpeech() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.states {
		if s == audio.VADStateSpeaking || s == audio.VADStateStarting {
			return true
		}
	}
	return false
}

func (d *spyTurnDetector) counts() (states, audioChunks int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.states), d.audioN
}

// TestAudioTurnStage_TurnDetectorReceivesVADState pins the wiring itself.
//
// A detector splits its work: ProcessAudio carries the samples, ProcessVADState
// carries the speaking/silence signal that IsUserSpeaking — and therefore
// shouldCompleteTurn's vote — depends on. Delivering only the audio leaves the
// detector unable to answer the question the stage asks it.
func TestAudioTurnStage_TurnDetectorReceivesVADState(t *testing.T) {
	spy := &spyTurnDetector{}
	cfg := stage.DefaultAudioTurnConfig()
	cfg.TurnDetector = spy

	const chunkSamples = 1600

	runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range 20 { // 2s of speech
			in <- makeAudioElement(vadSpeechPCM(chunkSamples), 16000)
		}
	})

	states, audioChunks := spy.counts()
	if states == 0 {
		t.Fatalf("TurnDetector received %d audio chunks but 0 VAD states; "+
			"the stage is not driving ProcessVADState", audioChunks)
	}
	if states != audioChunks {
		t.Errorf("TurnDetector received %d VAD states for %d audio chunks; "+
			"both must be delivered per chunk to stay in step", states, audioChunks)
	}
	if !spy.sawSpeech() {
		t.Error("TurnDetector never received a speaking state across 2s of continuous speech")
	}
}
