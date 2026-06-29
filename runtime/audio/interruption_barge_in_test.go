package audio

import (
	"testing"
	"time"
)

// TestInterruptionHandler_InterruptFiresChannel verifies the out-of-band channel
// signal used by the realtime barge-in path: Interrupt() closes the channel so a
// selecting consumer wakes immediately, and Reset() re-arms it for the next turn.
func TestInterruptionHandler_InterruptFiresChannel(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)

	ch := h.Interrupted()
	select {
	case <-ch:
		t.Fatal("channel fired before any interruption")
	default:
	}

	h.Interrupt()
	if !h.WasInterrupted() {
		t.Fatal("WasInterrupted should be true after Interrupt()")
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Interrupted() channel did not fire after Interrupt()")
	}

	// Idempotent within a turn: a second Interrupt() must not panic (double close).
	h.Interrupt()

	// Reset re-arms: a freshly fetched channel is open again.
	h.Reset()
	if h.WasInterrupted() {
		t.Fatal("WasInterrupted should be false after Reset")
	}
	ch2 := h.Interrupted()
	select {
	case <-ch2:
		t.Fatal("channel should be re-armed (open) after Reset")
	default:
	}
}

// TestInterruptionHandler_VADAlsoFiresChannel verifies the existing VAD-driven
// Immediate path also signals the channel (so both detection paths converge).
func TestInterruptionHandler_VADAlsoFiresChannel(t *testing.T) {
	h := NewInterruptionHandler(InterruptionImmediate, nil)
	h.SetBotSpeaking(true)
	ch := h.Interrupted()

	if fired, _ := h.ProcessVADState(nil, VADStateSpeaking); !fired {
		t.Fatal("expected Immediate VAD to report interruption")
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("VAD-driven interruption did not fire the channel")
	}
}
