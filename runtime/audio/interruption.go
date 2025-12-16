package audio

import (
	"context"
	"sync"
)

// InterruptionStrategy determines how to handle user interrupting bot.
type InterruptionStrategy int

const (
	// InterruptionIgnore ignores user speech during bot output.
	InterruptionIgnore InterruptionStrategy = iota
	// InterruptionImmediate immediately stops bot and starts listening.
	InterruptionImmediate
	// InterruptionDeferred waits for bot's current sentence, then switches.
	InterruptionDeferred
)

// String returns a human-readable representation of the interruption strategy.
func (s InterruptionStrategy) String() string {
	switch s {
	case InterruptionIgnore:
		return "ignore"
	case InterruptionImmediate:
		return "immediate"
	case InterruptionDeferred:
		return "deferred"
	default:
		return "unknown"
	}
}

// InterruptionCallback is called when user interrupts the bot.
type InterruptionCallback func()

// InterruptionHandler manages user interruption logic during bot output.
type InterruptionHandler struct {
	strategy InterruptionStrategy
	vad      VADAnalyzer

	mu              sync.RWMutex
	botSpeaking     bool
	interrupted     bool
	onInterrupt     InterruptionCallback
	deferredPending bool
}

// NewInterruptionHandler creates an InterruptionHandler with the given strategy and VAD.
func NewInterruptionHandler(strategy InterruptionStrategy, vad VADAnalyzer) *InterruptionHandler {
	return &InterruptionHandler{
		strategy: strategy,
		vad:      vad,
	}
}

// SetBotSpeaking sets whether the bot is currently outputting audio.
func (h *InterruptionHandler) SetBotSpeaking(speaking bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.botSpeaking = speaking

	// If bot stops speaking and there's a deferred interruption, trigger it
	if !speaking && h.deferredPending {
		h.deferredPending = false
		if h.onInterrupt != nil {
			go h.onInterrupt()
		}
	}
}

// IsBotSpeaking returns true if the bot is currently outputting audio.
func (h *InterruptionHandler) IsBotSpeaking() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.botSpeaking
}

// OnInterrupt registers a callback for when interruption occurs.
func (h *InterruptionHandler) OnInterrupt(callback InterruptionCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onInterrupt = callback
}

// ProcessAudio processes audio and detects user interruption.
// Returns true if an interruption was detected and should be acted upon.
func (h *InterruptionHandler) ProcessAudio(ctx context.Context, audio []byte) (bool, error) {
	h.mu.RLock()
	botSpeaking := h.botSpeaking
	strategy := h.strategy
	h.mu.RUnlock()

	// No interruption possible when bot is quiet
	if !botSpeaking {
		return false, nil
	}

	// Ignore strategy - never interrupt
	if strategy == InterruptionIgnore {
		return false, nil
	}

	// Run audio through VAD
	_, err := h.vad.Analyze(ctx, audio)
	if err != nil {
		return false, err
	}

	// Check if user started speaking
	if h.vad.State() == VADStateSpeaking {
		return h.handleInterruption(), nil
	}

	return false, nil
}

// ProcessVADState processes a VAD state update for interruption detection.
// Returns true if an interruption was detected and should be acted upon.
func (h *InterruptionHandler) ProcessVADState(ctx context.Context, state VADState) (bool, error) {
	h.mu.RLock()
	botSpeaking := h.botSpeaking
	strategy := h.strategy
	h.mu.RUnlock()

	// No interruption possible when bot is quiet or with ignore strategy
	if !botSpeaking || strategy == InterruptionIgnore {
		return false, nil
	}

	// Check if user started speaking
	if state == VADStateSpeaking {
		return h.handleInterruption(), nil
	}

	return false, nil
}

// handleInterruption processes an interruption based on strategy.
// Returns true if the interruption should be acted upon immediately.
func (h *InterruptionHandler) handleInterruption() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.interrupted {
		return false // Already handled
	}

	switch h.strategy {
	case InterruptionIgnore:
		return false // Ignore strategy should not reach here, but handle it

	case InterruptionImmediate:
		h.interrupted = true
		if h.onInterrupt != nil {
			go h.onInterrupt()
		}
		return true

	case InterruptionDeferred:
		h.deferredPending = true
		return false // Wait for sentence boundary

	default:
		return false
	}
}

// WasInterrupted returns true if an interruption occurred.
func (h *InterruptionHandler) WasInterrupted() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.interrupted
}

// Reset clears interruption state for a new turn.
func (h *InterruptionHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.botSpeaking = false
	h.interrupted = false
	h.deferredPending = false
}

// NotifySentenceBoundary notifies the handler of a sentence boundary.
// For deferred interruption strategy, this may trigger the pending interruption.
func (h *InterruptionHandler) NotifySentenceBoundary() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.deferredPending && h.onInterrupt != nil {
		h.deferredPending = false
		go h.onInterrupt()
	}
}
