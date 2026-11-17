package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// defaultTranscriptHoldTimeout is how long the reorder stage holds a completed
// turn waiting for a late input transcript before falling back to the
// placeholder. Providers like OpenAI Realtime deliver Whisper transcripts a
// beat after a short reply finishes; this covers that gap without stalling a
// genuinely transcript-less turn for long.
const defaultTranscriptHoldTimeout = 3 * time.Second

// TranscriptReorderStage guarantees that, within a turn, the user's input
// transcript is emitted before that turn's assistant output, even when the
// provider delivers the transcript late (e.g. OpenAI Realtime, whose Whisper
// transcription can land after the assistant reply, or even after the whole
// turn, has finished).
//
// Until the user turn for the current turn arrives it holds everything the
// model produced in answer: assistant TEXT, the turn-end, and the messages a
// tool round adds (the tool results that follow a tool-call turn-end). Once
// the user turn arrives (or a short timeout after a turn-end elapses) it emits
// the user turn, then what it held, in the order received. AUDIO passes
// through immediately, so playback stays realtime. Only the transcript log is
// reordered. If the transcript never arrives, a configurable placeholder user
// turn is emitted.
//
// A turn-end that makes tool calls does not end the user's turn: the reply
// after the tool round answers the same utterance, so it is held (or streamed,
// once the user turn is shown) with it rather than waiting for a transcript of
// its own. The round's other elements (results, calls held for the caller,
// errors) are held with it. While a round DuplexProviderStage runs
// (Meta.ToolCallsRunning) is still running, a held turn is not timed out. A
// round whose calls never get results does not swallow the next turn: the
// next reply closes it.
//
// A user transcript marked Meta.LateTranscript belongs to the turn whose reply
// already completed: it fills that turn while the stage still holds it, and is
// dropped once the stage has resolved the turn (with the placeholder) rather
// than becoming the next turn's user message. A typed user turn
// (TextInputTurnSource) and any non-user message for a later turn resolve a
// held finished turn first instead of joining it.
//
// The mechanism is provider-agnostic; whether it is wired is decided upstream. It
// is a Transform stage: state is per-turn and reset on each turn boundary, so a
// single instance handles one continuous duplex conversation.
type TranscriptReorderStage struct {
	BaseStage

	// placeholder is the user-turn content synthesized when a turn ends and no
	// transcript arrives within holdTimeout. Empty omits the user turn.
	placeholder string

	// holdTimeout bounds how long a completed turn waits for a late transcript.
	holdTimeout time.Duration

	// buffered holds the turn's output seen before its user element: assistant
	// text, turn-ends and tool-round messages. Flushed (in order) once the user
	// element arrives or the turn resolves.
	buffered []StreamElement

	// userEmitted is true once the user turn element for the current turn has been
	// forwarded (or resolved with the placeholder); the turn's output then
	// streams live.
	userEmitted bool

	// holding is set while buffered holds a turn-end awaiting a late transcript;
	// the hold timer runs while it is set.
	holding bool

	// heldToolTurn is set when the last held turn-end made tool calls: the
	// user's turn continues past it.
	heldToolTurn bool

	// awaiting are the current tool round's calls with no result yet; running
	// are those DuplexProviderStage is still running itself (not yet answered
	// or handed to the caller as pending).
	awaiting map[string]bool
	running  map[string]bool
}

// NewTranscriptReorderStage creates a reorder stage with the given missing-turn
// placeholder text (empty to omit the user turn) and the default hold timeout.
func NewTranscriptReorderStage(placeholder string) *TranscriptReorderStage {
	return NewTranscriptReorderStageWithTimeout(placeholder, defaultTranscriptHoldTimeout)
}

// NewTranscriptReorderStageWithTimeout is like NewTranscriptReorderStage but with
// an explicit hold timeout (tests use a short value).
func NewTranscriptReorderStageWithTimeout(placeholder string, holdTimeout time.Duration) *TranscriptReorderStage {
	return &TranscriptReorderStage{
		BaseStage:   NewBaseStage("transcript-reorder", StageTypeTransform),
		placeholder: placeholder,
		holdTimeout: holdTimeout,
	}
}

// Process implements the Stage interface.
func (s *TranscriptReorderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	var hold holdTimer
	defer hold.stop()

	for {
		select {
		case elem, ok := <-input:
			if !ok {
				return s.resolveOnInputClosed(ctx, output)
			}
			rearm, err := s.handle(ctx, elem, output)
			if err != nil {
				return err
			}
			s.syncHoldTimer(&hold, rearm)

		case <-hold.c:
			hold.clear()
			// Transcript never arrived — fall back to the placeholder.
			if err := s.resolvePending(ctx, output); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// holdTimer is the optional timer bounding how long a turn-end is held for a
// late transcript. A nil c never fires in a select, so an unstarted timer is
// inert.
type holdTimer struct {
	timer *time.Timer
	c     <-chan time.Time
}

// start arms the timer for d.
func (h *holdTimer) start(d time.Duration) {
	h.timer = time.NewTimer(d)
	h.c = h.timer.C
}

// stop stops a running timer and clears it.
func (h *holdTimer) stop() {
	if h.timer != nil {
		h.timer.Stop()
		h.clear()
	}
}

// clear forgets the timer without stopping it (used after it fired).
func (h *holdTimer) clear() {
	h.timer = nil
	h.c = nil
}

// syncHoldTimer keeps the hold timer in line with the holding state: it runs
// while a turn-end is held, except while the held tool round is still running,
// and restarts (so the window runs from the latest event) when rearm is set.
func (s *TranscriptReorderStage) syncHoldTimer(hold *holdTimer, rearm bool) {
	switch {
	case !s.timerWanted():
		hold.stop()
	case rearm || hold.timer == nil:
		hold.stop()
		hold.start(s.holdTimeout)
	}
}

// timerWanted reports whether a held turn is waiting on the hold window: one
// is held and no tool round of it is still running.
func (s *TranscriptReorderStage) timerWanted() bool {
	return s.holding && (!s.heldToolTurn || len(s.running) == 0)
}

// resolveOnInputClosed resolves any held turn (placeholder if still no
// transcript) and flushes, once the input channel has closed.
func (s *TranscriptReorderStage) resolveOnInputClosed(ctx context.Context, output chan<- StreamElement) error {
	if len(s.buffered) > 0 { // a held turn-end is buffered too
		return s.resolvePending(ctx, output)
	}
	return nil
}

// handle routes one element. It returns rearm=true when the hold window
// should restart: a turn-end was newly held.
func (s *TranscriptReorderStage) handle(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) (rearm bool, err error) {
	switch {
	case isUserTurnElement(&elem):
		return false, s.handleUserTurn(ctx, elem, output)
	case elem.Audio != nil:
		return false, s.send(ctx, output, elem) // realtime — never buffered or held
	case elem.EndOfStream || elem.Meta.Interrupted:
		return s.handleTurnEnd(ctx, elem, output)
	case elem.Text != nil:
		return false, s.handleText(ctx, elem, output)
	case elem.Message != nil && elem.Message.Role == roleTool:
		return false, s.handleToolResult(ctx, elem, output)
	default:
		return false, s.handleOther(ctx, elem, output)
	}
}

// handleUserTurn places a user element. A late transcript fills the held
// turn or is dropped; a typed turn, or one arriving while the current tool
// round is unanswered, starts a new turn; any other is the transcript for the
// turn in progress.
func (s *TranscriptReorderStage) handleUserTurn(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	switch {
	case elem.Meta.LateTranscript && !s.holding:
		logger.Debug("TranscriptReorderStage: dropping a late transcript for a turn already resolved")
		return nil
	case elem.Meta.LateTranscript:
		return s.takeTranscript(ctx, elem, output)
	case elem.Source == TextInputTurnSource || len(s.awaiting) > 0:
		if s.holding || len(s.awaiting) > 0 {
			if err := s.closeTurn(ctx, output); err != nil {
				return err
			}
		}
		if err := s.send(ctx, output, elem); err != nil {
			return err
		}
		s.userEmitted = true
		return nil
	default:
		return s.takeTranscript(ctx, elem, output)
	}
}

// takeTranscript emits the user turn, then everything held for it, in order.
// The turn ends there unless the last held turn-end made tool calls.
func (s *TranscriptReorderStage) takeTranscript(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if err := s.send(ctx, output, elem); err != nil {
		return err
	}
	s.userEmitted = true
	return s.releaseHeld(ctx, output)
}

// handleTurnEnd forwards the turn-end when the user turn was already shown, or
// holds it (rearm=true) to wait for a late transcript.
func (s *TranscriptReorderStage) handleTurnEnd(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) (rearm bool, err error) {
	if err := s.closeAbandonedRound(ctx, output); err != nil {
		return false, err
	}
	continues := makesToolCalls(&elem)
	if s.userEmitted {
		if err := s.flushBuffer(ctx, output); err != nil {
			return false, err
		}
		if err := s.send(ctx, output, elem); err != nil {
			return false, err
		}
		if continues {
			s.startRound(&elem)
		} else {
			s.resetTurn()
		}
		return false, nil
	}
	if err := s.resolveFinishedTurn(ctx, output); err != nil {
		return false, err
	}
	s.buffered = append(s.buffered, elem)
	s.holding = true
	s.heldToolTurn = continues
	if continues {
		s.startRound(&elem)
	}
	return true, nil
}

// handleText buffers assistant text before the user turn (resolving any prior
// finished turn first), or forwards it once the user turn has been shown.
// Text while the current tool round is unanswered is a new turn's reply.
func (s *TranscriptReorderStage) handleText(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if err := s.closeAbandonedRound(ctx, output); err != nil {
		return err
	}
	if err := s.resolveFinishedTurn(ctx, output); err != nil {
		return err
	}
	if !s.userEmitted {
		s.buffered = append(s.buffered, elem)
		return nil
	}
	return s.send(ctx, output, elem)
}

// handleToolResult answers a call of the current round: held after the
// tool-call turn-end while that is held, otherwise forwarded.
func (s *TranscriptReorderStage) handleToolResult(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if tr := elem.Message.ToolResult; tr != nil {
		delete(s.awaiting, tr.ID)
		s.stopRunning(tr.ID)
	}
	if s.holding && s.heldToolTurn {
		s.buffered = append(s.buffered, elem)
		return nil
	}
	if err := s.resolveFinishedTurn(ctx, output); err != nil {
		return err
	}
	return s.send(ctx, output, elem)
}

// handleOther places any other element. A held tool round keeps its elements
// (calls held for the caller, errors) in order; a message for a later turn
// resolves a held finished turn first.
func (s *TranscriptReorderStage) handleOther(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	for i := range elem.Meta.PendingTools {
		s.stopRunning(elem.Meta.PendingTools[i].CallID) // handed to the caller
	}
	if s.holding && s.heldToolTurn {
		s.buffered = append(s.buffered, elem)
		return nil
	}
	if elem.Message != nil {
		if err := s.resolveFinishedTurn(ctx, output); err != nil {
			return err
		}
	}
	return s.send(ctx, output, elem)
}

// startRound records the calls a tool-call turn-end makes: awaited until
// their results arrive, and running while the duplex stage runs them.
func (s *TranscriptReorderStage) startRound(elem *StreamElement) {
	s.awaiting = map[string]bool{}
	s.running = map[string]bool{}
	for _, c := range elem.Message.ToolCalls {
		s.awaiting[c.ID] = true
		if elem.Meta.ToolCallsRunning {
			s.running[c.ID] = true
		}
	}
}

// stopRunning marks a call no longer running. The hold timer starts once no
// call of a held round is running (syncHoldTimer).
func (s *TranscriptReorderStage) stopRunning(id string) {
	delete(s.running, id)
}

// closeAbandonedRound closes the current turn when its tool round still has
// unanswered calls: new model output means the round was abandoned (or is
// waiting on the caller) and a new turn has begun.
func (s *TranscriptReorderStage) closeAbandonedRound(ctx context.Context, output chan<- StreamElement) error {
	if len(s.awaiting) == 0 {
		return nil
	}
	return s.closeTurn(ctx, output)
}

// closeTurn ends the current turn: a held one is resolved with the
// placeholder, and the turn's state is reset whether or not it continued.
func (s *TranscriptReorderStage) closeTurn(ctx context.Context, output chan<- StreamElement) error {
	if s.holding {
		if err := s.resolvePending(ctx, output); err != nil {
			return err
		}
	}
	s.resetTurn()
	return nil
}

// resolveFinishedTurn resolves a held turn whose turn-end finished it (no tool
// calls): new output while still awaiting its transcript belongs to the next
// turn, so the transcript isn't coming. A held tool-call turn-end is left
// held: the output that follows it answers the same user turn.
func (s *TranscriptReorderStage) resolveFinishedTurn(ctx context.Context, output chan<- StreamElement) error {
	if !s.holding || s.heldToolTurn {
		return nil
	}
	return s.resolvePending(ctx, output)
}

// resolvePending finishes waiting for the transcript: emits the placeholder
// (when no user turn was shown), then everything held.
func (s *TranscriptReorderStage) resolvePending(ctx context.Context, output chan<- StreamElement) error {
	if !s.userEmitted && s.placeholder != "" {
		ph := StreamElement{Message: &types.Message{Role: roleUser, Content: s.placeholder}}
		if err := s.send(ctx, output, ph); err != nil {
			return err
		}
	}
	s.userEmitted = true
	return s.releaseHeld(ctx, output)
}

// releaseHeld flushes what was held once the user turn is shown. A held
// turn-end that made tool calls leaves the turn open, its output streaming
// live; any other ends the turn.
func (s *TranscriptReorderStage) releaseHeld(ctx context.Context, output chan<- StreamElement) error {
	if err := s.flushBuffer(ctx, output); err != nil {
		return err
	}
	wasHolding, continues := s.holding, s.heldToolTurn
	s.holding, s.heldToolTurn = false, false
	if wasHolding && !continues {
		s.resetTurn()
	}
	return nil
}

// resetTurn clears per-turn state for the next turn.
func (s *TranscriptReorderStage) resetTurn() {
	s.userEmitted = false
	s.buffered = s.buffered[:0]
	s.holding = false
	s.heldToolTurn = false
	s.awaiting = nil
	s.running = nil
}

// flushBuffer forwards and clears any buffered elements.
func (s *TranscriptReorderStage) flushBuffer(ctx context.Context, output chan<- StreamElement) error {
	for i := range s.buffered {
		if err := s.send(ctx, output, s.buffered[i]); err != nil {
			return err
		}
	}
	s.buffered = s.buffered[:0]
	return nil
}

// send forwards one element, honoring context cancellation.
func (s *TranscriptReorderStage) send(ctx context.Context, output chan<- StreamElement, elem StreamElement) error {
	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// makesToolCalls reports whether a turn-end element closes a model response
// that made tool calls, so the user's turn continues past it. An interrupted
// response does not continue it.
func makesToolCalls(elem *StreamElement) bool {
	return elem.EndOfStream && !elem.Meta.Interrupted && elem.Message != nil && len(elem.Message.ToolCalls) > 0
}

// isUserTurnElement reports whether an element is the materialized user turn (a
// user-role Message). The turn-boundary EndOfStream element carries an
// assistant Message and is handled separately.
func isUserTurnElement(elem *StreamElement) bool {
	return elem.Message != nil && elem.Message.Role == roleUser && !elem.EndOfStream
}
