package audio

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"
)

func TestNewSimpleVAD(t *testing.T) {
	t.Run("valid params", func(t *testing.T) {
		vad, err := NewSimpleVAD(DefaultVADParams())
		if err != nil {
			t.Fatalf("NewSimpleVAD() error = %v", err)
		}
		if vad == nil {
			t.Fatal("NewSimpleVAD() returned nil")
		}
		if vad.Name() != "simple-rms" {
			t.Errorf("Name() = %v, want simple-rms", vad.Name())
		}
	})

	t.Run("invalid params", func(t *testing.T) {
		params := VADParams{Confidence: -1} // Invalid
		_, err := NewSimpleVAD(params)
		if err == nil {
			t.Error("NewSimpleVAD() should error on invalid params")
		}
	})
}

func TestSimpleVAD_Name(t *testing.T) {
	vad, _ := NewSimpleVAD(DefaultVADParams())
	if got := vad.Name(); got != "simple-rms" {
		t.Errorf("Name() = %v, want simple-rms", got)
	}
}

// generatePCMAudio creates 16-bit PCM audio data with the given amplitude.
// amplitude should be 0.0 to 1.0
func generatePCMAudio(samples int, amplitude float64) []byte {
	data := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		// Generate a sine wave
		sample := int16(amplitude * 32767 * math.Sin(float64(i)*0.1))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	return data
}

// generateSilence creates silent 16-bit PCM audio data.
func generateSilence(samples int) []byte {
	return make([]byte, samples*2) // All zeros
}

func TestSimpleVAD_Analyze_Silence(t *testing.T) {
	vad, _ := NewSimpleVAD(DefaultVADParams())

	// Analyze silence
	silence := generateSilence(1600) // 100ms at 16kHz
	prob, err := vad.Analyze(context.Background(), silence)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if prob != 0 {
		t.Errorf("Analyze(silence) probability = %v, want 0", prob)
	}

	if vad.State() != VADStateQuiet {
		t.Errorf("State() = %v, want VADStateQuiet", vad.State())
	}
}

func TestSimpleVAD_Analyze_Speech(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.01 // Lower threshold for faster test
	vad, _ := NewSimpleVAD(params)

	// Generate loud audio (should trigger voice detection)
	loudAudio := generatePCMAudio(1600, 0.5) // High amplitude

	// Analyze multiple times to exceed start threshold
	for i := 0; i < 10; i++ {
		prob, err := vad.Analyze(context.Background(), loudAudio)
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}

		if prob <= 0 {
			t.Errorf("Analyze(loud audio) probability = %v, want > 0", prob)
		}
	}

	// Should eventually transition to speaking
	state := vad.State()
	if state != VADStateSpeaking && state != VADStateStarting {
		t.Errorf("State() = %v, want Speaking or Starting", state)
	}
}

func TestSimpleVAD_StateTransitions(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.001 // Very short for testing
	params.StopSecs = 0.001
	vad, _ := NewSimpleVAD(params)

	// Start quiet
	if vad.State() != VADStateQuiet {
		t.Errorf("initial State() = %v, want VADStateQuiet", vad.State())
	}

	// Transition to speaking with loud audio (need time to pass for StartSecs)
	loudAudio := generatePCMAudio(1600, 0.5)
	for i := 0; i < 50; i++ {
		vad.Analyze(context.Background(), loudAudio)
		time.Sleep(time.Millisecond) // Allow time for StartSecs threshold
	}

	state := vad.State()
	if state != VADStateSpeaking && state != VADStateStarting {
		t.Errorf("after loud audio State() = %v, want VADStateSpeaking or VADStateStarting", vad.State())
	}

	// Transition back to quiet with silence
	silence := generateSilence(1600)
	for i := 0; i < 50; i++ {
		vad.Analyze(context.Background(), silence)
		time.Sleep(time.Millisecond)
	}

	state = vad.State()
	if state != VADStateQuiet && state != VADStateStopping {
		t.Errorf("after silence State() = %v, want VADStateQuiet or VADStateStopping", vad.State())
	}
}

func TestSimpleVAD_OnStateChange(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.001
	params.StopSecs = 0.001
	vad, _ := NewSimpleVAD(params)

	events := vad.OnStateChange()

	// Generate events by transitioning states
	loudAudio := generatePCMAudio(1600, 0.5)
	for i := 0; i < 50; i++ {
		vad.Analyze(context.Background(), loudAudio)
		time.Sleep(time.Millisecond)
	}

	// Check for state change event (could be starting or speaking)
	select {
	case event := <-events:
		if event.State != VADStateSpeaking && event.State != VADStateStarting {
			t.Errorf("event.State = %v, want VADStateSpeaking or VADStateStarting", event.State)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected state change event")
	}
}

func TestSimpleVAD_Reset(t *testing.T) {
	params := DefaultVADParams()
	params.StartSecs = 0.01
	vad, _ := NewSimpleVAD(params)

	// Get to speaking state
	loudAudio := generatePCMAudio(1600, 0.5)
	for i := 0; i < 10; i++ {
		vad.Analyze(context.Background(), loudAudio)
	}

	// Reset
	vad.Reset()

	if vad.State() != VADStateQuiet {
		t.Errorf("after Reset() State() = %v, want VADStateQuiet", vad.State())
	}
}

func TestSimpleVAD_EmptyAudio(t *testing.T) {
	vad, _ := NewSimpleVAD(DefaultVADParams())

	prob, err := vad.Analyze(context.Background(), []byte{})
	if err != nil {
		t.Fatalf("Analyze(empty) error = %v", err)
	}
	if prob != 0 {
		t.Errorf("Analyze(empty) = %v, want 0", prob)
	}

	prob, err = vad.Analyze(context.Background(), nil)
	if err != nil {
		t.Fatalf("Analyze(nil) error = %v", err)
	}
	if prob != 0 {
		t.Errorf("Analyze(nil) = %v, want 0", prob)
	}
}
