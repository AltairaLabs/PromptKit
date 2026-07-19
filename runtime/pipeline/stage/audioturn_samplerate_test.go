package stage_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// ratePCM returns `samples` of speech-level sine, independent of sample rate.
func ratePCM(samples int) []byte {
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(0.15 * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// chunksToSpeaking feeds 100ms chunks at the configured rate and returns how
// many were needed before the turn detector saw a Speaking state, or -1.
func chunksToSpeaking(t *testing.T, sampleRate int) int {
	t.Helper()

	spy := &spyTurnDetector{}
	cfg := stage.DefaultAudioTurnConfig()
	cfg.SampleRate = sampleRate
	cfg.TurnDetector = spy

	chunkSamples := sampleRate / 10 // 100 ms at this rate

	runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range 30 {
			in <- makeAudioElement(ratePCM(chunkSamples), sampleRate)
		}
	})

	spy.mu.Lock()
	defer spy.mu.Unlock()
	for i, s := range spy.states {
		if s == audio.VADStateSpeaking {
			return i + 1
		}
	}
	return -1
}

// TestAudioTurnStage_VADHonorsConfiguredSampleRate covers the stage building its
// VAD at a hardcoded rate while timing its own turn math at the configured one.
//
// NewAudioTurnStage resolves config.SampleRate for its turn math, then creates
// the default VAD with DefaultVADParams() — always 16 kHz. That was harmless
// while the VAD state machine ignored SampleRate, but transitions are now scaled
// by it, so the VAD and the stage disagree about how long a chunk lasts whenever
// the configured rate is not 16 kHz.
//
// At 8 kHz telephony the VAD sees every duration as half its true length, so
// StartSecs 0.2 behaves as 0.4 and StopSecs 0.8 as 1.6 — the VAD lags turn-end
// by an extra ~800ms while the stage's own silence math is on schedule. At
// 48 kHz it fires three times early.
//
// Measured in chunks-to-Speaking, which should be the same count at any rate
// because each chunk is 100ms of audio regardless.
func TestAudioTurnStage_VADHonorsConfiguredSampleRate(t *testing.T) {
	base := chunksToSpeaking(t, 16000)
	if base < 0 {
		t.Fatal("VAD never reached Speaking at 16kHz; the baseline is broken")
	}

	for _, rate := range []int{8000, 48000} {
		got := chunksToSpeaking(t, rate)
		if got < 0 {
			t.Errorf("at %dHz the VAD never reached Speaking within 30 chunks (3s of audio); "+
				"the stage builds its VAD at a hardcoded 16kHz, so durations are scaled wrong", rate)
			continue
		}
		// Each chunk is 100ms of audio at every rate, so the count must match.
		// One chunk of slack absorbs rounding.
		if got < base-1 || got > base+1 {
			t.Errorf("at %dHz the VAD reached Speaking after %d chunks, but after %d at 16kHz; "+
				"each chunk is 100ms at both rates, so the stage is building its VAD with "+
				"DefaultVADParams (hardcoded 16kHz) instead of the configured rate",
				rate, got, base)
		}
	}
}
