package stage_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// quietSpeechPCM returns sine at ~0.04 RMS — quiet but unambiguous conversational
// speech, well above a room-noise floor (~0.005) and far above digital silence.
func quietSpeechPCM(samples int) []byte {
	const targetRMS = 0.04
	amp := targetRMS * math.Sqrt2
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(amp * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// TestAudioTurnStage_DefaultVADDetectsQuietSpeech covers the stage's default VAD
// failing to recognize quiet speech as speech at all.
//
// The default was SimpleVAD, which maps RMS to probability linearly between
// MinVolume (0.01) and maxExpectedRMS (0.1) and compares against a 0.5
// Confidence threshold — so nothing below ~0.055 RMS is ever speech.
// Conversational speech averages ~0.10 RMS but ranges well under that: a
// measured 0.04 RMS passage scores 0.33 and is classified as silence.
//
// Measured across conditions, AdaptiveVAD detects this where SimpleVAD does not,
// detects normal speech sooner (300ms vs 500ms), and holds through quiet tails,
// while false-positive behavior on silence and room noise is identical.
//
// Observed through a turn detector, which receives every VAD state the stage
// computes. A turn is emitted either way at EndOfStream (the degrade-open path),
// so segmentation alone would not reveal whether the VAD ever saw speech.
func TestAudioTurnStage_DefaultVADDetectsQuietSpeech(t *testing.T) {
	spy := &spyTurnDetector{}
	cfg := stage.DefaultAudioTurnConfig()
	cfg.TurnDetector = spy // leave cfg.VAD nil so the stage picks its default

	const chunkSamples = 1600 // 100 ms

	runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range 20 { // 2s of quiet speech
			in <- makeAudioElement(quietSpeechPCM(chunkSamples), 16000)
		}
	})

	spy.mu.Lock()
	defer spy.mu.Unlock()

	sawSpeaking := false
	for _, s := range spy.states {
		if s == audio.VADStateSpeaking {
			sawSpeaking = true
			break
		}
	}

	if !sawSpeaking {
		t.Errorf("the stage's default VAD never reported speech across 2s of 0.04 RMS speech; "+
			"a fixed-threshold default classifies quiet conversational speech as silence "+
			"(states seen: %v)", spy.states)
	}
}

// TestAudioTurnStage_DefaultVADIgnoresRoomNoise is the counterpart: a more
// sensitive default must not start treating a quiet mic's noise floor as speech.
func TestAudioTurnStage_DefaultVADIgnoresRoomNoise(t *testing.T) {
	spy := &spyTurnDetector{}
	cfg := stage.DefaultAudioTurnConfig()
	cfg.TurnDetector = spy

	const chunkSamples = 1600

	runTurnStage(t, cfg, func(in chan<- stage.StreamElement) {
		for range 20 { // 2s of digital silence
			in <- makeAudioElement(vadSilencePCM(chunkSamples), 16000)
		}
	})

	spy.mu.Lock()
	defer spy.mu.Unlock()

	for _, s := range spy.states {
		if s == audio.VADStateSpeaking {
			t.Fatalf("the default VAD reported speech over silence (states: %v)", spy.states)
		}
	}
}
