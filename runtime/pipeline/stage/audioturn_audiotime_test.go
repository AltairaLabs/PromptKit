package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// collectTurns drains output and returns one []byte per emitted audio turn.
func collectTurns(output <-chan stage.StreamElement) [][]byte {
	var turns [][]byte
	for e := range output {
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			turns = append(turns, e.Audio.Samples)
		}
	}
	return turns
}

// TestAudioTurnStage_SegmentsByAudioTimeNotWallClock feeds
// speech|silence|speech|silence INSTANTLY (no real-time pacing) and requires the
// stage to still segment into two turns.
//
// Turn boundaries must be decided from the AUDIO's own duration (sample count),
// not wall-clock elapsed time. In a live pipeline, a blocking STT call or GC
// pause drives wall-clock ahead of audio-time and mis-cuts turns — the defect
// that split a caller utterance and dropped its trailing word ("...Springfield")
// from a real call. With wall-clock timing an instant feed accumulates ~0s of
// silence and emits a single turn only at EndOfStream; audio-time timing yields
// two turns regardless of how fast the audio arrives.
func TestAudioTurnStage_SegmentsByAudioTimeNotWallClock(t *testing.T) {
	s, err := stage.NewAudioTurnStage(stage.DefaultAudioTurnConfig())
	if err != nil {
		t.Fatalf("NewAudioTurnStage: %v", err)
	}

	const chunkSamples = 1600 // 100 ms @ 16 kHz

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() { _ = s.Process(ctx, input, output) }()

	go func() {
		feed := func(gen func(int) []byte, chunks int) {
			for range chunks {
				input <- makeAudioElement(gen(chunkSamples), 16000)
			}
		}
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s silence  (> 0.8 s SilenceDuration in AUDIO time)
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s silence
		input <- stage.StreamElement{EndOfStream: true}
		close(input)
	}()

	turns := collectTurns(output)
	if len(turns) < 2 {
		t.Fatalf("expected >=2 audio-time-segmented turns from an instant feed, got %d; "+
			"turn segmentation depends on wall-clock, not audio duration", len(turns))
	}
}

// TestAudioTurnStage_ZeroSampleRateFallsBackToDefault guards the audio-time
// timebase. Callers may build an AudioTurnConfig by hand instead of via
// DefaultAudioTurnConfig, leaving SampleRate unset. Because turn boundaries are
// now measured in samples, a zero rate would collapse every duration to 0 —
// MinSpeechDuration would never be satisfied and the stage would accumulate
// forever, emitting only at EndOfStream. NewAudioTurnStage must substitute the
// default rate so segmentation still works.
func TestAudioTurnStage_ZeroSampleRateFallsBackToDefault(t *testing.T) {
	cfg := audioTurnConfigNoRate()
	s, err := stage.NewAudioTurnStage(cfg)
	if err != nil {
		t.Fatalf("NewAudioTurnStage: %v", err)
	}

	const chunkSamples = 1600 // 100 ms @ 16 kHz

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() { _ = s.Process(ctx, input, output) }()

	go func() {
		feed := func(gen func(int) []byte, chunks int) {
			for range chunks {
				input <- makeAudioElement(gen(chunkSamples), 16000)
			}
		}
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s silence
		feed(vadSpeechPCM, 10)  // 1.0 s speech
		feed(vadSilencePCM, 12) // 1.2 s silence
		input <- stage.StreamElement{EndOfStream: true}
		close(input)
	}()

	turns := collectTurns(output)
	if len(turns) < 2 {
		t.Fatalf("expected >=2 turns with SampleRate unset (default should apply), got %d; "+
			"a zero sample rate collapses audio-time durations to 0 and stalls segmentation", len(turns))
	}
}

// audioTurnConfigNoRate builds a config the way a caller would when they
// only care about the turn thresholds and never set SampleRate.
func audioTurnConfigNoRate() stage.AudioTurnConfig {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.SampleRate = 0
	return cfg
}
