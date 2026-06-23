package audio

import (
	"context"
	"testing"
	"time"
)

func TestNewAdaptiveVAD(t *testing.T) {
	t.Run("valid params", func(t *testing.T) {
		vad, err := NewAdaptiveVAD(DefaultVADParams())
		if err != nil {
			t.Fatalf("NewAdaptiveVAD() error = %v", err)
		}
		if vad == nil {
			t.Fatal("NewAdaptiveVAD() returned nil")
		}
		if vad.Name() != "adaptive-rms" {
			t.Errorf("Name() = %v, want adaptive-rms", vad.Name())
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		params := VADParams{Confidence: -1}
		_, err := NewAdaptiveVAD(params)
		if err == nil {
			t.Error("NewAdaptiveVAD() should error on invalid params")
		}
	})
}

// TestAdaptiveVAD_NoiseFloorAdaptation feeds quiet background followed by
// louder speech and asserts that the VAD detects the speech after the noise
// floor has settled.
func TestAdaptiveVAD_NoiseFloorAdaptation(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.05 // Speed up the test
	vad, err := NewAdaptiveVAD(params)
	if err != nil {
		t.Fatalf("NewAdaptiveVAD() error = %v", err)
	}

	ctx := context.Background()

	// Feed background noise at amplitude 0.003 (~0.002 RMS) — well below
	// the adaptiveFloorInit of 0.005, which lets the floor descend.
	background := generatePCMAudio(1600, 0.003)
	for i := 0; i < 30; i++ {
		_, err := vad.Analyze(ctx, background)
		if err != nil {
			t.Fatalf("Analyze(background) error = %v", err)
		}
	}

	// After background conditioning the floor has descended; now feed speech
	// at amplitude 0.04 (~0.028 RMS). This is below SimpleVAD's fixed MinVolume
	// of 0.01 only relative to maxExpectedRMS=0.1, but AdaptiveVAD's
	// speechThreshold = max(floor×3, 0.01) will be much lower.
	speech := generatePCMAudio(1600, 0.04)
	for i := 0; i < 20; i++ {
		_, err := vad.Analyze(ctx, speech)
		if err != nil {
			t.Fatalf("Analyze(speech) error = %v", err)
		}
		time.Sleep(2 * time.Millisecond) // Let StartSecs timer accumulate
	}

	state := vad.State()
	if state != VADStateSpeaking && state != VADStateStarting {
		t.Errorf("State() = %v after quiet background + louder speech, want Speaking or Starting", state)
	}
}

// TestAdaptiveVAD_QuietMicDetected verifies that AdaptiveVAD detects speech
// at amplitude 0.04 (~0.028 RMS) — a level that sits below SimpleVAD's fixed
// MinVolume-to-maxExpectedRMS mapping and thus would never trigger SimpleVAD.
func TestAdaptiveVAD_QuietMicDetected(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.001 // Minimal hold time for unit test speed
	vad, err := NewAdaptiveVAD(params)
	if err != nil {
		t.Fatalf("NewAdaptiveVAD() error = %v", err)
	}

	ctx := context.Background()

	// amplitude 0.04 → RMS ≈ 0.028, which is above adaptiveFloorInit (0.005)
	// and above speechThreshold = max(0.005×3, 0.01) = 0.015.
	quietSpeech := generatePCMAudio(1600, 0.04)

	var lastProb float64
	for i := 0; i < 15; i++ {
		prob, err := vad.Analyze(ctx, quietSpeech)
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		lastProb = prob
		time.Sleep(time.Millisecond)
	}

	if lastProb <= 0 {
		t.Errorf("probability = %v for amplitude=0.04 quiet speech, want > 0 (AdaptiveVAD must detect quiet mic)", lastProb)
	}

	state := vad.State()
	if state != VADStateSpeaking && state != VADStateStarting {
		t.Errorf("State() = %v, want Speaking or Starting for quiet-mic speech", state)
	}
}

// TestAdaptiveVAD_SilenceStaysQuiet verifies that true silence stays at
// VADStateQuiet and returns zero probability.
func TestAdaptiveVAD_SilenceStaysQuiet(t *testing.T) {
	vad, err := NewAdaptiveVAD(DefaultVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD() error = %v", err)
	}

	silence := generateSilence(1600)
	for i := 0; i < 10; i++ {
		prob, err := vad.Analyze(context.Background(), silence)
		if err != nil {
			t.Fatalf("Analyze(silence) error = %v", err)
		}
		if prob != 0 {
			t.Errorf("Analyze(silence) probability = %v, want 0", prob)
		}
	}

	if vad.State() != VADStateQuiet {
		t.Errorf("State() = %v after silence, want VADStateQuiet", vad.State())
	}
}

// TestAdaptiveVAD_Reset verifies that Reset returns the VAD to its initial state.
func TestAdaptiveVAD_Reset(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.001
	vad, err := NewAdaptiveVAD(params)
	if err != nil {
		t.Fatalf("NewAdaptiveVAD() error = %v", err)
	}

	// Drive into speaking state.
	speech := generatePCMAudio(1600, 0.1)
	for i := 0; i < 15; i++ {
		vad.Analyze(context.Background(), speech) //nolint:errcheck
		time.Sleep(time.Millisecond)
	}

	vad.Reset()

	if vad.State() != VADStateQuiet {
		t.Errorf("after Reset() State() = %v, want VADStateQuiet", vad.State())
	}
}
