package stage_test

import (
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// TestAudioTurnStage_BoundsRetainedSilenceWhenNoSpeechDetected covers unbounded
// memory growth on the no-speech path.
//
// The stage buffers every chunk, and shouldCompleteTurn returns early while
// speechDetected is false, so the MaxTurnDuration bound never applies. A
// participant who stays quiet — listening while the other party talks, a hot mic
// below the VAD threshold, a VAD misconfigured for the sample rate — grows the
// buffer for as long as they stay quiet. Benchmarked at ~2.2 MB/min with a flat
// per-minute rate across a 60x range: linear, no ceiling.
//
// Retention must be bounded by MaxTurnDuration, which is already the stage's
// declared maximum extent for any turn.
func TestAudioTurnStage_BoundsRetainedSilenceWhenNoSpeechDetected(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig() // MaxTurnDuration 30s, 16 kHz
	const chunkSamples = 1600             // 100 ms
	const minutes = 5

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range minutes * 600 {
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected buffered audio to be forwarded at EndOfStream, got no turns")
	}

	// Every emitted turn must respect the bound, not just the first.
	for i, turn := range turns {
		got := turnDuration(turn)
		if got > cfg.MaxTurnDuration {
			t.Errorf("turn %d retained %v of silence, exceeds MaxTurnDuration %v; "+
				"buffering on the no-speech path is unbounded (fed %d minutes)",
				i, got, cfg.MaxTurnDuration, minutes)
		}
	}
}

// TestAudioTurnStage_BoundsRetentionForSubSecondMaxTurnDuration covers the
// retention bound silently not applying at all.
//
// Computing the window from MaxTurnDuration.Seconds() as an int truncates: any
// duration below one second yields zero bytes, which reads as "no bound
// configured" and disables the cap entirely. Sub-second values are reachable —
// existing tests use 100ms and the SDK exposes the field — so the leak would
// still be present wherever it is set that low.
func TestAudioTurnStage_BoundsRetentionForSubSecondMaxTurnDuration(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.MaxTurnDuration = 500 * time.Millisecond

	const chunkSamples = 1600 // 100 ms
	const fedSeconds = 60

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range fedSeconds * 10 {
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected a turn at EndOfStream, got none")
	}

	for i, turn := range turns {
		if got := turnDuration(turn); got > cfg.MaxTurnDuration {
			t.Errorf("turn %d retained %v against a %v bound (fed %ds); "+
				"sub-second MaxTurnDuration truncates to zero and disables the bound",
				i, got, cfg.MaxTurnDuration, fedSeconds)
		}
	}
}

// TestAudioTurnStage_RetentionBoundKeepsFractionalSeconds guards the window
// against losing the fractional part of MaxTurnDuration.
//
// Truncating 1.5s to 1.0s bounds tighter than configured, discarding a third
// more audio than the contract promises. Feeding well past the bound and
// measuring what survives detects the narrower window.
func TestAudioTurnStage_RetentionBoundKeepsFractionalSeconds(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.MaxTurnDuration = 1500 * time.Millisecond

	const chunkSamples = 1600 // 100 ms

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range 300 { // 30s, far past the bound
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected a turn at EndOfStream, got none")
	}

	got := turnDuration(turns[len(turns)-1])
	// Allow a chunk of slack: the window is enforced per chunk, so the retained
	// span lands within one chunk of the bound rather than exactly on it.
	const slack = 100 * time.Millisecond
	if got < cfg.MaxTurnDuration-slack {
		t.Errorf("retained %v against a %v bound; the fractional second was "+
			"truncated away, bounding tighter than configured", got, cfg.MaxTurnDuration)
	}
	if got > cfg.MaxTurnDuration+slack {
		t.Errorf("retained %v, exceeds the %v bound", got, cfg.MaxTurnDuration)
	}
}

// TestAudioTurnStage_DoesNotSplitUtteranceAfterLongSilence covers an utterance
// being cut in two when it follows a long pause.
//
// turnSamples accumulates during silence, but shouldCompleteTurn measures it
// against MaxTurnDuration. After a quiet stretch longer than MaxTurnDuration the
// budget is already spent before anyone speaks, so the turn force-completes the
// moment MinSpeechDuration is satisfied — chopping the utterance into a ~1s
// fragment plus the remainder. That is two STT calls and a transcript broken
// mid-word.
//
// Silence that the retention bound has already discarded must not count against
// the turn's duration budget.
func TestAudioTurnStage_DoesNotSplitUtteranceAfterLongSilence(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig() // MaxTurnDuration 30s
	const chunkSamples = 1600             // 100 ms

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		// A long pause, well past MaxTurnDuration.
		for range 400 { // 40s
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
		// Then one continuous utterance, with no internal pause to split on.
		for range 50 { // 5s
			in <- makeAudioElement(vadSpeechPCM(chunkSamples), 16000)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected a turn, got none")
	}
	if len(turns) > 1 {
		durations := make([]time.Duration, len(turns))
		for i, turn := range turns {
			durations[i] = turnDuration(turn)
		}
		t.Fatalf("continuous 5s utterance after a 40s pause split into %d turns (%v); "+
			"silence consumed the turn's duration budget before speech began",
			len(turns), durations)
	}
}

// TestAudioTurnStage_RetainsNewestSilenceWithinBound pins WHICH audio survives
// the retention bound.
//
// Dropping the bound's worth from the wrong end would keep the oldest audio and
// discard whatever the participant said just before the stream ended — the exact
// audio most likely to matter. A duration-only assertion cannot tell the two
// apart.
func TestAudioTurnStage_RetainsNewestSilenceWithinBound(t *testing.T) {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.MaxTurnDuration = time.Second // keep the test small and the window obvious

	const chunkSamples = 1600 // 100 ms, so the bound holds 10 chunks
	const totalChunks = 100

	turns := runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for i := range totalChunks {
			// Silence, but tagged: every sample carries the chunk index, so the
			// retained window identifies itself. Values stay small enough to
			// remain below the VAD threshold.
			chunk := make([]byte, chunkSamples*2)
			for j := range chunk {
				chunk[j] = byte(i % 8)
			}
			in <- makeAudioElement(chunk, 16000)
		}
	})

	if len(turns) == 0 {
		t.Fatalf("expected a turn at EndOfStream, got none")
	}

	last := turns[len(turns)-1]
	if len(last) == 0 {
		t.Fatalf("emitted turn is empty")
	}

	// The final byte must come from the last chunk fed. If the bound dropped
	// from the wrong end, the tail would carry an older index.
	wantTail := byte((totalChunks - 1) % 8)
	if got := last[len(last)-1]; got != wantTail {
		t.Errorf("retained window ends with chunk tag %d, want %d; "+
			"the retention bound is discarding the newest audio instead of the oldest",
			got, wantTail)
	}
}
