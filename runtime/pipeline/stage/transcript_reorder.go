package stage

import (
	"context"
	"time"

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
// its own.
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
			held, err := s.handle(ctx, elem, output)
			if err != nil {
				return err
			}
			s.syncHoldTimer(&hold, held)

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

// syncHoldTimer manages the hold timer to match the holding state: each
// turn-end handle starts holding (re)arms it, so the window runs from the
// latest one, and it stops once nothing is held.
func (s *TranscriptReorderStage) syncHoldTimer(hold *holdTimer, held bool) {
	switch {
	case held:
		hold.stop()
		hold.start(s.holdTimeout)
	case !s.holding:
		hold.stop()
	}
}

// resolveOnInputClosed resolves any held turn (placeholder if still no
// transcript) and flushes, once the input channel has closed.
func (s *TranscriptReorderStage) resolveOnInputClosed(ctx context.Context, output chan<- StreamElement) error {
	if len(s.buffered) > 0 { // a held turn-end is buffered too
		return s.resolvePending(ctx, output)
	}
	return nil
}

// handle routes one element. It returns held=true when it started holding a
// turn-end awaiting a late transcript.
func (s *TranscriptReorderStage) handle(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) (held bool, err error) {
	switch {
	case isUserTurnElement(&elem):
		return false, s.handleUserTurn(ctx, elem, output)
	case elem.Audio != nil:
		return false, s.send(ctx, output, elem) // realtime — never buffered or held
	case elem.EndOfStream || elem.Meta.Interrupted:
		return s.handleTurnEnd(ctx, elem, output)
	case elem.Text != nil:
		return false, s.handleText(ctx, elem, output)
	case elem.Message != nil && s.holding:
		// A message the turn's tool round adds (a tool result) belongs after
		// the held turn-end that made the call.
		s.buffered = append(s.buffered, elem)
		return false, nil
	default:
		return false, s.send(ctx, output, elem)
	}
}

// handleUserTurn emits the user turn, then everything held for it, in order.
// The turn ends there unless the last held turn-end made tool calls.
func (s *TranscriptReorderStage) handleUserTurn(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if err := s.send(ctx, output, elem); err != nil {
		return err
	}
	s.userEmitted = true
	return s.releaseHeld(ctx, output)
}

// handleTurnEnd forwards the turn-end when the user turn was already shown, or
// holds it (held=true) to wait for a late transcript.
func (s *TranscriptReorderStage) handleTurnEnd(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) (held bool, err error) {
	continues := makesToolCalls(&elem)
	if s.userEmitted {
		if err := s.flushBuffer(ctx, output); err != nil {
			return false, err
		}
		if err := s.send(ctx, output, elem); err != nil {
			return false, err
		}
		if !continues {
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
	return true, nil
}

// handleText buffers assistant text before the user turn (resolving any prior
// finished turn first), or forwards it once the user turn has been shown.
func (s *TranscriptReorderStage) handleText(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if err := s.resolveFinishedTurn(ctx, output); err != nil {
		return err
	}
	if !s.userEmitted {
		s.buffered = append(s.buffered, elem)
		return nil
	}
	return s.send(ctx, output, elem)
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
