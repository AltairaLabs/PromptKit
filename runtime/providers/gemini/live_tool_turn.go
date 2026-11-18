package gemini

import (
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Gemini Live ends a tool-call generation in one of two ways, told apart by
// the messages that follow the toolCall, never by the model's name:
//
//   - toolCall, generationComplete, then a turnComplete carrying that
//     generation's usage, sent before the server has the tool response
//     (observed live on gemini-3.8-live: all three read within 1ms);
//   - toolCall and nothing more: the reply to the tool response follows, and
//     one turnComplete carries the usage of the whole server turn (observed
//     live on gemini-2.5-flash-native-audio).
//
// So a toolCall's chunk is held (holdToolCalls) until the generation's end is
// known. A generationComplete while it is held means the generation's
// turnComplete follows: that turnComplete's usage goes on the tool calls'
// chunk, so the provider call that made them carries its own cost. Anything
// else, or the wait running out (toolTurnEndWait with no generationComplete,
// toolTurnEndMaxWait after one), sends the chunk as it is. No tool response
// can be sent before the chunk is, so a turnComplete read while it is held is
// the tool-call generation's, never the reply's.
const (
	// toolTurnEndWait is how long a toolCall's chunk is held for a
	// generationComplete.
	toolTurnEndWait = 250 * time.Millisecond
	// toolTurnEndMaxWait is how long it is held, after a generationComplete,
	// for the turnComplete carrying its usage.
	toolTurnEndMaxWait = 2 * time.Second
)

// heldToolCalls is a toolCall's chunk held for its generation's end. The
// receive goroutine holds and releases it; the hold's timer can release it
// from its own goroutine, sending under mu so that it never sends once the
// receive loop has closed the hold.
type heldToolCalls struct {
	mu     sync.Mutex
	chunk  *providers.StreamChunk
	timer  *time.Timer
	closed bool
	// waits overrides toolTurnEndWait and toolTurnEndMaxWait (tests).
	wait, maxWait time.Duration
}

// holdToolCalls holds chunk, a toolCall's, until its generation's end is
// known or the wait runs out. Receive goroutine only.
func (s *StreamSession) holdToolCalls(chunk providers.StreamChunk) error {
	if err := s.releaseToolCalls(); err != nil { // never two held
		return err
	}
	s.toolGenEnding = false
	h := &s.held
	h.mu.Lock()
	defer h.mu.Unlock()
	h.chunk = &chunk
	h.timer = s.releaseAfter(h.chunk, durationOr(h.wait, toolTurnEndWait))
	return nil
}

// releaseAfter starts the timer that sends held once d passes, unless it was
// released before. Called with s.held.mu held.
func (s *StreamSession) releaseAfter(held *providers.StreamChunk, d time.Duration) *time.Timer {
	return time.AfterFunc(d, func() {
		h := &s.held
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.closed || h.chunk != held {
			return
		}
		h.chunk = nil
		logger.Debug("StreamSession: tool calls sent without their generation's end")
		if err := s.emitHeld(held); err != nil {
			logger.Debug("StreamSession: held tool calls not delivered", "error", err)
		}
	})
}

// extendHold gives the held chunk toolTurnEndMaxWait for its generation's
// turnComplete, once a generationComplete says it follows. A timer that has
// already fired has sent it. Receive goroutine only.
func (s *StreamSession) extendHold() {
	h := &s.held
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.chunk != nil && h.timer.Stop() {
		h.timer = s.releaseAfter(h.chunk, durationOr(h.maxWait, toolTurnEndMaxWait))
	}
}

// takeHeld returns the held chunk and stops holding it; nil when none is held.
func (s *StreamSession) takeHeld() *providers.StreamChunk {
	h := &s.held
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.takeLocked()
}

// takeLocked is takeHeld with h.mu held.
func (h *heldToolCalls) takeLocked() *providers.StreamChunk {
	held := h.chunk
	h.chunk = nil
	if h.timer != nil {
		h.timer.Stop()
	}
	return held
}

// holdingToolCalls reports whether a toolCall's chunk is held.
func (s *StreamSession) holdingToolCalls() bool {
	h := &s.held
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.chunk != nil
}

// releaseToolCalls sends the held chunk, carrying the usage held for its
// generation. Receive goroutine only.
func (s *StreamSession) releaseToolCalls() error {
	held := s.takeHeld()
	if held == nil {
		return nil
	}
	held.CostInfo = s.takeTurnCost()
	return s.emitHeld(held)
}

// closeHold sends the held chunk, if any, as the receive loop ends, without
// blocking, and stops the timer sending it later. Receive goroutine only.
func (s *StreamSession) closeHold() {
	h := &s.held
	h.mu.Lock()
	h.closed = true
	held := h.takeLocked()
	h.mu.Unlock()
	if held == nil {
		return
	}
	held.CostInfo = s.takeTurnCost()
	select {
	case s.emitCh <- *held:
	default:
		logger.Warn("StreamSession: tool calls not reported at session end: the response queue is full")
	}
}

// emitHeld sends a held chunk with the generation it was held with.
func (s *StreamSession) emitHeld(chunk *providers.StreamChunk) error {
	select {
	case s.emitCh <- *chunk:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// endHeldToolCallTurn handles msg while a toolCall's chunk is held, and
// reports whether msg was consumed: a generationComplete (its turnComplete
// follows), that turnComplete (its usage goes on the tool calls, which are
// sent), or usage on its own (the generation's). Anything else sends the held
// chunk first and is processed as usual. Receive goroutine only.
func (s *StreamSession) endHeldToolCallTurn(msg *ServerMessage) (bool, error) {
	if !s.holdingToolCalls() {
		return false, nil
	}
	sc := msg.ServerContent
	switch {
	case sc == nil:
		s.holdUsage(msg.UsageMetadata)
		return true, nil
	case isGenerationCompleteOnly(sc):
		s.toolGenEnding = true
		s.extendHold()
		return true, nil
	case sc.TurnComplete && !hasModelOutput(sc):
		s.holdUsage(msg.UsageMetadata)
		s.toolGenEnding = false
		s.toolCallTurn = false
		if err := s.releaseToolCalls(); err != nil {
			return true, err
		}
		s.ClearDrop()
		return true, s.handleInputTranscription(sc)
	default:
		return false, s.releaseToolCalls()
	}
}

// isGenerationCompleteOnly reports whether sc says only that the generation
// is complete.
func isGenerationCompleteOnly(sc *ServerContent) bool {
	return sc.GenerationComplete && !sc.TurnComplete && !sc.Interrupted && !hasModelOutput(sc) &&
		(sc.InputTranscription == nil || sc.InputTranscription.Text == "")
}

// hasModelOutput reports whether sc carries the model's output or an
// interruption of it.
func hasModelOutput(sc *ServerContent) bool {
	return sc.ModelTurn != nil || sc.Interrupted ||
		(sc.OutputTranscription != nil && sc.OutputTranscription.Text != "")
}

// durationOr is d, or def when d is zero.
func durationOr(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}
