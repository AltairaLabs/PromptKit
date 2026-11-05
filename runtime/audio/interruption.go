package audio

import (
	"context"
	"sync"
	"time"
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
//
// User speech is a barge-in while the bot is engaged in a reply, which is any
// of: SetBotSpeaking(true) (e.g. TTS synthesizing), a reply begun with
// BeginResponse that EndResponse has not ended (the model generating it), or
// delivered audio still playing (ExtendPlayback).
type InterruptionHandler struct {
	strategy InterruptionStrategy
	vad      VADAnalyzer

	mu          sync.RWMutex
	botSpeaking bool
	// responding is set from the end of the user's turn until the reply's turn
	// ends (BeginResponse/EndResponse), covering the generation that precedes
	// any audio.
	responding bool
	// respondAfterReset records a reply begun while an interruption was still
	// being handled, so the Reset that closes that interruption keeps it.
	respondAfterReset bool
	// speakingUntil is when the audio delivered so far would finish playing.
	speakingUntil time.Time
	// now is the clock for speakingUntil; nil means time.Now.
	now             func() time.Time
	interrupted     bool
	onInterrupt     InterruptionCallback
	deferredPending bool
	// interruptCh is closed when an interruption fires and re-armed by Reset.
	// It lets a stage that is blocked/sleeping (e.g. the audio pacing stage
	// draining a buffered realtime response) abort immediately via select,
	// which the callback/poll API (onInterrupt / WasInterrupted) cannot do.
	interruptCh chan struct{}
}

// NewInterruptionHandler creates an InterruptionHandler with the given strategy and VAD.
func NewInterruptionHandler(strategy InterruptionStrategy, vad VADAnalyzer) *InterruptionHandler {
	return &InterruptionHandler{
		strategy:    strategy,
		vad:         vad,
		interruptCh: make(chan struct{}),
	}
}

// interruptChLocked returns the current interrupt channel, lazily creating it so
// a struct-literal-constructed handler still works. Caller must hold h.mu.
func (h *InterruptionHandler) interruptChLocked() chan struct{} {
	if h.interruptCh == nil {
		h.interruptCh = make(chan struct{})
	}
	return h.interruptCh
}

// fireLocked closes the current interrupt channel exactly once (idempotent).
// Caller must hold h.mu.
func (h *InterruptionHandler) fireLocked() {
	ch := h.interruptChLocked()
	select {
	case <-ch: // already fired this turn
	default:
		close(ch)
	}
}

// Interrupted returns a channel closed when an interruption fires for the
// current turn. Reset re-arms it, so callers should re-fetch after a Reset.
func (h *InterruptionHandler) Interrupted() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.interruptChLocked()
}

// Interrupt externally signals an interruption. Use this on the realtime/ASM
// path where the provider's server-side VAD detects barge-in rather than a
// local VAD. No ProcessVADState call drives the interruption there, so the
// provider stage signals it directly. Idempotent within a turn; cleared by Reset.
func (h *InterruptionHandler) Interrupt() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.interrupted {
		return
	}
	h.interrupted = true
	h.fireLocked()
	if h.onInterrupt != nil {
		go h.onInterrupt()
	}
}

// SetBotSpeaking sets whether the bot is outputting audio (e.g. while TTS is
// synthesizing). See BeginResponse and ExtendPlayback for the rest of the
// window in which user speech interrupts the bot.
func (h *InterruptionHandler) SetBotSpeaking(speaking bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.botSpeaking = speaking

	// If bot stops speaking and there's a deferred interruption, trigger it
	if !speaking {
		h.fireDeferredLocked()
	}
}

// fireDeferredLocked delivers a pending deferred interruption. Caller must hold
// h.mu.
func (h *InterruptionHandler) fireDeferredLocked() {
	if !h.deferredPending {
		return
	}
	h.deferredPending = false
	if h.onInterrupt != nil {
		go h.onInterrupt()
	}
}

// BeginResponse marks that the bot owes a reply: call it when the user's turn
// ends. Until EndResponse, user speech interrupts the reply even though no
// audio has been produced yet (the model is still generating it).
//
// A reply begun while an interruption is still being handled (its Reset has not
// happened yet) survives that Reset: it is the reply to the interrupting turn.
func (h *InterruptionHandler) BeginResponse() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.responding = true
	if h.interrupted {
		h.respondAfterReset = true
	}
}

// EndResponse marks the end of the reply begun by BeginResponse: its turn is
// over, or no reply will follow. Audio still playing (see ExtendPlayback) keeps
// the bot speaking until it would have finished. A deferred interruption
// pending on this reply fires now.
func (h *InterruptionHandler) EndResponse() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.responding = false
	h.respondAfterReset = false
	h.fireDeferredLocked()
}

// ExtendPlayback records that d more audio was handed out for playback. Audio
// queues behind whatever is still playing, so the bot counts as speaking until
// max(now, previous end) + d. Reset clears it. Non-positive d is ignored.
func (h *InterruptionHandler) ExtendPlayback(d time.Duration) {
	if d <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	start := h.clockLocked()
	if h.speakingUntil.After(start) {
		start = h.speakingUntil
	}
	h.speakingUntil = start.Add(d)
}

// clockLocked returns the current time. Caller must hold h.mu.
func (h *InterruptionHandler) clockLocked() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// engagedLocked reports whether user speech now counts as a barge-in: the bot
// is synthesizing, owes a reply, or its audio is still playing. Caller must
// hold h.mu (read or write).
func (h *InterruptionHandler) engagedLocked() bool {
	return h.botSpeaking || h.responding || h.clockLocked().Before(h.speakingUntil)
}

// IsBotSpeaking reports whether the bot is engaged in a reply, so that user
// speech would interrupt it: SetBotSpeaking(true), a reply begun and not ended,
// or delivered audio still playing.
func (h *InterruptionHandler) IsBotSpeaking() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.engagedLocked()
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
	botSpeaking := h.engagedLocked()
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
	botSpeaking := h.engagedLocked()
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
		h.fireLocked()
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

// Reset clears interruption state for a new turn: the interrupted reply's
// speaking, responding and playback state is dropped with it.
func (h *InterruptionHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.botSpeaking = false
	// A reply begun after the interruption fired belongs to the interrupting
	// turn, so it outlives the Reset that closes the interrupted one.
	h.responding = h.respondAfterReset
	h.respondAfterReset = false
	h.speakingUntil = time.Time{}
	h.interrupted = false
	h.deferredPending = false
	// Re-arm the interrupt channel for the next turn. Waiters from the previous
	// turn already observed the closed channel; new waiters get a fresh one.
	h.interruptCh = make(chan struct{})
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
