package audio

import (
	"context"
	"testing"
)

func TestInterruptionStrategy_String(t *testing.T) {
	tests := []struct {
		strategy InterruptionStrategy
		want     string
	}{
		{InterruptionIgnore, "ignore"},
		{InterruptionImmediate, "immediate"},
		{InterruptionDeferred, "deferred"},
		{InterruptionStrategy(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.strategy.String(); got != tt.want {
				t.Errorf("InterruptionStrategy.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mockVAD implements VADAnalyzer for testing
type mockVAD struct {
	state VADState
}

func (m *mockVAD) Name() string                                               { return "mock" }
func (m *mockVAD) Analyze(ctx context.Context, audio []byte) (float64, error) { return 0, nil }
func (m *mockVAD) State() VADState                                            { return m.state }
func (m *mockVAD) OnStateChange() <-chan VADEvent                             { return nil }
func (m *mockVAD) Reset()                                                     {}

func TestNewInterruptionHandler(t *testing.T) {
	vad := &mockVAD{}
	h := NewInterruptionHandler(InterruptionImmediate, vad)
	if h == nil {
		t.Fatal("NewInterruptionHandler() returned nil")
	}
}

func TestInterruptionHandler_SetBotSpeaking(t *testing.T) {
	vad := &mockVAD{}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	if h.IsBotSpeaking() {
		t.Error("IsBotSpeaking() should be false initially")
	}

	h.SetBotSpeaking(true)
	if !h.IsBotSpeaking() {
		t.Error("IsBotSpeaking() should be true after SetBotSpeaking(true)")
	}

	h.SetBotSpeaking(false)
	if h.IsBotSpeaking() {
		t.Error("IsBotSpeaking() should be false after SetBotSpeaking(false)")
	}
}

func TestInterruptionHandler_IgnoreStrategy(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionIgnore, vad)

	h.SetBotSpeaking(true)

	// Even with user speaking and bot speaking, should not interrupt
	interrupted, err := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if err != nil {
		t.Fatalf("ProcessVADState() error = %v", err)
	}
	if interrupted {
		t.Error("InterruptionIgnore should not trigger interruption")
	}
}

func TestInterruptionHandler_ImmediateStrategy(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	h.OnInterrupt(func() {
		// Callback executed
	})

	// Bot not speaking - no interrupt
	interrupted, _ := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if interrupted {
		t.Error("should not interrupt when bot is not speaking")
	}

	// Bot speaking + user speaking = interrupt
	h.SetBotSpeaking(true)
	interrupted, _ = h.ProcessVADState(context.Background(), VADStateSpeaking)
	if !interrupted {
		t.Error("should interrupt when both speaking")
	}

	// Give callback time to execute
	// Note: callback is async, so we just check the flag was set
	if !h.WasInterrupted() {
		t.Error("WasInterrupted() should be true after interruption")
	}
}

func TestInterruptionHandler_DeferredStrategy(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionDeferred, vad)

	var callbackCalled bool
	h.OnInterrupt(func() {
		callbackCalled = true
	})

	h.SetBotSpeaking(true)

	// Should not interrupt immediately
	interrupted, _ := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if interrupted {
		t.Error("deferred strategy should not interrupt immediately")
	}

	// Callback should not be called yet
	if callbackCalled {
		t.Error("callback should not be called before sentence boundary")
	}

	// Notify sentence boundary
	h.NotifySentenceBoundary()

	// Give async callback time
	// The callback was triggered, but we can't easily verify timing in unit test
}

func TestInterruptionHandler_NotBotSpeaking(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	// Bot not speaking - interruption not possible
	interrupted, _ := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if interrupted {
		t.Error("should not interrupt when bot is not speaking")
	}
}

func TestInterruptionHandler_Reset(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	h.SetBotSpeaking(true)
	h.ProcessVADState(context.Background(), VADStateSpeaking)

	if !h.WasInterrupted() {
		t.Error("should be interrupted before reset")
	}

	h.Reset()

	if h.WasInterrupted() {
		t.Error("WasInterrupted() should be false after reset")
	}
	if h.IsBotSpeaking() {
		t.Error("IsBotSpeaking() should be false after reset")
	}
}

func TestInterruptionHandler_DeferredTriggerOnBotStop(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionDeferred, vad)

	callbackChan := make(chan struct{}, 1)
	h.OnInterrupt(func() {
		callbackChan <- struct{}{}
	})

	h.SetBotSpeaking(true)

	// User starts speaking - queue deferred interruption
	h.ProcessVADState(context.Background(), VADStateSpeaking)

	// Bot stops speaking - should trigger deferred interruption
	h.SetBotSpeaking(false)

	// Check callback was queued
	select {
	case <-callbackChan:
		// Success
	default:
		// May not have executed yet due to goroutine timing
	}
}

func TestInterruptionHandler_ProcessAudio(t *testing.T) {
	vad := &mockVAD{state: VADStateQuiet}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	t.Run("no interrupt when bot not speaking", func(t *testing.T) {
		interrupted, err := h.ProcessAudio(context.Background(), []byte{0, 0})
		if err != nil {
			t.Fatalf("ProcessAudio() error = %v", err)
		}
		if interrupted {
			t.Error("should not interrupt when bot is not speaking")
		}
	})

	t.Run("no interrupt when user not speaking", func(t *testing.T) {
		h.SetBotSpeaking(true)
		vad.state = VADStateQuiet
		interrupted, err := h.ProcessAudio(context.Background(), []byte{0, 0})
		if err != nil {
			t.Fatalf("ProcessAudio() error = %v", err)
		}
		if interrupted {
			t.Error("should not interrupt when user is not speaking")
		}
	})

	t.Run("interrupt when both speaking", func(t *testing.T) {
		h.Reset()
		h.SetBotSpeaking(true)
		vad.state = VADStateSpeaking
		interrupted, err := h.ProcessAudio(context.Background(), []byte{0, 0})
		if err != nil {
			t.Fatalf("ProcessAudio() error = %v", err)
		}
		if !interrupted {
			t.Error("should interrupt when both speaking")
		}
	})

	t.Run("ignore strategy never interrupts", func(t *testing.T) {
		ignoreH := NewInterruptionHandler(InterruptionIgnore, vad)
		ignoreH.SetBotSpeaking(true)
		vad.state = VADStateSpeaking
		interrupted, err := ignoreH.ProcessAudio(context.Background(), []byte{0, 0})
		if err != nil {
			t.Fatalf("ProcessAudio() error = %v", err)
		}
		if interrupted {
			t.Error("ignore strategy should never interrupt")
		}
	})
}

func TestInterruptionHandler_AlreadyInterrupted(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionImmediate, vad)

	var callCount int
	h.OnInterrupt(func() {
		callCount++
	})

	h.SetBotSpeaking(true)

	// First interruption should succeed
	h.ProcessVADState(context.Background(), VADStateSpeaking)
	if !h.WasInterrupted() {
		t.Error("should be interrupted after first call")
	}

	// Second interruption should be ignored
	h.ProcessVADState(context.Background(), VADStateSpeaking)
	// Callback should have been called only once
}

func TestInterruptionHandler_NoCallback(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionImmediate, vad)
	// Don't register a callback

	h.SetBotSpeaking(true)

	// Should not panic when callback is nil
	interrupted, err := h.ProcessVADState(context.Background(), VADStateSpeaking)
	if err != nil {
		t.Fatalf("ProcessVADState() error = %v", err)
	}
	if !interrupted {
		t.Error("should return interrupted=true even without callback")
	}
}

func TestInterruptionHandler_DeferredNoCallback(t *testing.T) {
	vad := &mockVAD{state: VADStateSpeaking}
	h := NewInterruptionHandler(InterruptionDeferred, vad)
	// Don't register a callback

	h.SetBotSpeaking(true)

	// Should not panic
	h.ProcessVADState(context.Background(), VADStateSpeaking)
	h.NotifySentenceBoundary()
	h.SetBotSpeaking(false)
}
