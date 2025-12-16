package audio

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewSilenceDetector(t *testing.T) {
	d := NewSilenceDetector(500 * time.Millisecond)
	if d == nil {
		t.Fatal("NewSilenceDetector() returned nil")
	}
	if d.Threshold != 500*time.Millisecond {
		t.Errorf("Threshold = %v, want 500ms", d.Threshold)
	}
}

func TestSilenceDetector_Name(t *testing.T) {
	d := NewSilenceDetector(500 * time.Millisecond)
	if got := d.Name(); got != "silence" {
		t.Errorf("Name() = %v, want silence", got)
	}
}

func TestSilenceDetector_ProcessVADState(t *testing.T) {
	t.Run("detects turn end after silence threshold", func(t *testing.T) {
		d := NewSilenceDetector(50 * time.Millisecond)

		// Simulate speech
		d.ProcessVADState(context.Background(), VADStateSpeaking)
		if !d.IsUserSpeaking() {
			t.Error("should be speaking after VADStateSpeaking")
		}

		// Simulate stop
		d.ProcessVADState(context.Background(), VADStateStopping)

		// Wait for threshold
		time.Sleep(60 * time.Millisecond)

		// Process quiet state - should detect turn end
		endOfTurn, err := d.ProcessVADState(context.Background(), VADStateQuiet)
		if err != nil {
			t.Fatalf("ProcessVADState() error = %v", err)
		}
		if !endOfTurn {
			t.Error("expected end of turn after silence threshold")
		}
	})

	t.Run("no turn end without speech", func(t *testing.T) {
		d := NewSilenceDetector(50 * time.Millisecond)

		// Go directly to quiet without speech
		endOfTurn, _ := d.ProcessVADState(context.Background(), VADStateQuiet)
		if endOfTurn {
			t.Error("should not detect turn end without prior speech")
		}
	})

	t.Run("speech resumes before threshold", func(t *testing.T) {
		d := NewSilenceDetector(100 * time.Millisecond)

		// Start speaking
		d.ProcessVADState(context.Background(), VADStateSpeaking)

		// Brief pause
		d.ProcessVADState(context.Background(), VADStateStopping)
		time.Sleep(20 * time.Millisecond)

		// Resume speaking before threshold
		d.ProcessVADState(context.Background(), VADStateSpeaking)

		// Wait longer than original threshold
		time.Sleep(150 * time.Millisecond)

		// Still speaking, should not trigger turn end
		endOfTurn, _ := d.ProcessVADState(context.Background(), VADStateSpeaking)
		if endOfTurn {
			t.Error("should not detect turn end while still speaking")
		}
	})
}

func TestSilenceDetector_OnTurnComplete(t *testing.T) {
	d := NewSilenceDetector(20 * time.Millisecond)

	var callbackCalled bool
	var receivedAudio []byte
	var wg sync.WaitGroup
	wg.Add(1)

	d.OnTurnComplete(func(audio []byte, transcript string) {
		callbackCalled = true
		receivedAudio = audio
		wg.Done()
	})

	// Accumulate some audio
	testAudio := []byte{1, 2, 3, 4}
	d.ProcessVADState(context.Background(), VADStateSpeaking)
	d.ProcessAudio(context.Background(), testAudio)

	// Trigger turn end
	d.ProcessVADState(context.Background(), VADStateStopping)
	time.Sleep(30 * time.Millisecond)
	d.ProcessVADState(context.Background(), VADStateQuiet)

	// Wait for callback with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if !callbackCalled {
			t.Error("callback should have been called")
		}
		if len(receivedAudio) != len(testAudio) {
			t.Errorf("received audio length = %d, want %d", len(receivedAudio), len(testAudio))
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("callback was not called in time")
	}
}

func TestSilenceDetector_GetAccumulatedAudio(t *testing.T) {
	d := NewSilenceDetector(500 * time.Millisecond)

	// Initially empty
	if got := d.GetAccumulatedAudio(); got != nil {
		t.Errorf("GetAccumulatedAudio() = %v, want nil", got)
	}

	// Start speaking and accumulate
	d.ProcessVADState(context.Background(), VADStateSpeaking)
	d.ProcessAudio(context.Background(), []byte{1, 2, 3})
	d.ProcessAudio(context.Background(), []byte{4, 5})

	audio := d.GetAccumulatedAudio()
	if len(audio) != 5 {
		t.Errorf("GetAccumulatedAudio() length = %d, want 5", len(audio))
	}
}

func TestSilenceDetector_SetTranscript(t *testing.T) {
	d := NewSilenceDetector(20 * time.Millisecond)

	var receivedTranscript string
	var wg sync.WaitGroup
	wg.Add(1)

	d.OnTurnComplete(func(audio []byte, transcript string) {
		receivedTranscript = transcript
		wg.Done()
	})

	// Set transcript and trigger turn
	d.SetTranscript("hello world")
	d.ProcessVADState(context.Background(), VADStateSpeaking)
	d.ProcessAudio(context.Background(), []byte{1})
	d.ProcessVADState(context.Background(), VADStateStopping)
	time.Sleep(30 * time.Millisecond)
	d.ProcessVADState(context.Background(), VADStateQuiet)

	// Wait for callback
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if receivedTranscript != "hello world" {
			t.Errorf("transcript = %v, want hello world", receivedTranscript)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("callback was not called in time")
	}
}

func TestSilenceDetector_Reset(t *testing.T) {
	d := NewSilenceDetector(500 * time.Millisecond)

	// Accumulate state
	d.ProcessVADState(context.Background(), VADStateSpeaking)
	d.ProcessAudio(context.Background(), []byte{1, 2, 3})
	d.SetTranscript("test")

	// Reset
	d.Reset()

	if d.IsUserSpeaking() {
		t.Error("IsUserSpeaking() should be false after reset")
	}
	if d.GetAccumulatedAudio() != nil {
		t.Error("GetAccumulatedAudio() should be nil after reset")
	}
}
