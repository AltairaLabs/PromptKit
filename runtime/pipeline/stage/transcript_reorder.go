package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// defaultTranscriptHoldTimeout is how long the reorder stage holds a completed
// turn waiting for a late input transcript before falling back to the
// placeholder. Providers like OpenAI Realtime deliver Whisper transcripts a
// beat after a short reply finishes; this covers that gap without stalling a
// genuinely transcript-less turn for long.
const defaultTranscriptHoldTimeout = 3 * time.Second

// TranscriptReorderStage guarantees that, within a turn, the user's input
// transcript is emitted before that turn's assistant text — even when the
// provider delivers the transcript late (e.g. OpenAI Realtime, whose Whisper
// transcription can land after the assistant reply, or even after the whole
// turn, has finished).
//
// It buffers assistant TEXT elements and HOLDS the turn-end until the user turn
// for that turn arrives (or a short timeout elapses), then emits user-then-text
// in order. AUDIO passes through immediately, so playback stays realtime — only
// the transcript log is reordered. Crucially the turn-end (EndOfStream) is held
// too, so a late transcript is never emitted after the turn boundary (which both
// mis-orders the log and can confuse downstream turn-scoped stages). If the
// transcript never arrives, a configurable placeholder user turn is emitted.
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

	// buffered holds assistant text elements seen before the current turn's user
	// element. Flushed (in order) once the user element arrives or the turn resolves.
	buffered []StreamElement

	// userEmitted is true once the user turn element for the current turn has been
	// forwarded; subsequent assistant text for the turn streams live.
	userEmitted bool

	// pendingEnd holds the turn-end element while awaiting a late transcript. Nil
	// when not holding.
	pendingEnd *StreamElement
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

	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer != nil {
			timer.Stop()
			timer = nil
			timerC = nil
		}
	}
	defer stopTimer()

	for {
		select {
		case elem, ok := <-input:
			if !ok {
				// Input closed: resolve any held turn (placeholder if still no
				// transcript) and flush.
				if s.pendingEnd != nil || len(s.buffered) > 0 {
					return s.resolvePending(ctx, output, true)
				}
				return nil
			}
			held, err := s.handle(ctx, elem, output)
			if err != nil {
				return err
			}
			// Manage the hold timer to match pendingEnd state.
			switch {
			case held && timer == nil:
				timer = time.NewTimer(s.holdTimeout)
				timerC = timer.C
			case !held && s.pendingEnd == nil:
				stopTimer()
			}

		case <-timerC:
			timer = nil
			timerC = nil
			// Transcript never arrived — fall back to the placeholder.
			if err := s.resolvePending(ctx, output, true); err != nil {
				return err
			}

		case <-ctx.Done():
			return ctx.Err()
		}
	}
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
	default:
		return false, s.send(ctx, output, elem)
	}
}

// handleUserTurn emits the user turn, flushes buffered assistant text, and
// releases a held turn-end (if any) in order.
func (s *TranscriptReorderStage) handleUserTurn(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if err := s.send(ctx, output, elem); err != nil {
		return err
	}
	s.userEmitted = true
	if err := s.flushBuffer(ctx, output); err != nil {
		return err
	}
	if s.pendingEnd != nil {
		if err := s.send(ctx, output, *s.pendingEnd); err != nil {
			return err
		}
		s.resetTurn()
	}
	return nil
}

// handleTurnEnd forwards the turn-end when the user turn was already shown, or
// holds it (held=true) to wait for a late transcript.
func (s *TranscriptReorderStage) handleTurnEnd(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) (held bool, err error) {
	if s.userEmitted {
		if err := s.flushBuffer(ctx, output); err != nil {
			return false, err
		}
		if err := s.send(ctx, output, elem); err != nil {
			return false, err
		}
		s.resetTurn()
		return false, nil
	}
	e := elem
	s.pendingEnd = &e
	return true, nil
}

// handleText buffers assistant text before the user turn (resolving any prior
// held turn first), or forwards it once the user turn has been shown.
func (s *TranscriptReorderStage) handleText(
	ctx context.Context, elem StreamElement, output chan<- StreamElement,
) error {
	if s.pendingEnd != nil {
		// New turn's text while awaiting the previous turn's transcript — it isn't
		// coming. Resolve the previous turn (placeholder), then buffer this one.
		if err := s.resolvePending(ctx, output, true); err != nil {
			return err
		}
		s.buffered = append(s.buffered, elem)
		return nil
	}
	if !s.userEmitted {
		s.buffered = append(s.buffered, elem)
		return nil
	}
	return s.send(ctx, output, elem)
}

// resolvePending finishes a held turn: emits the placeholder (when requested and
// no user turn was shown), flushes buffered text, forwards the held turn-end, and
// resets per-turn state.
func (s *TranscriptReorderStage) resolvePending(
	ctx context.Context, output chan<- StreamElement, usePlaceholder bool,
) error {
	if usePlaceholder && !s.userEmitted && s.placeholder != "" {
		ph := StreamElement{Message: &types.Message{Role: roleUser, Content: s.placeholder}}
		if err := s.send(ctx, output, ph); err != nil {
			return err
		}
	}
	if err := s.flushBuffer(ctx, output); err != nil {
		return err
	}
	if s.pendingEnd != nil {
		if err := s.send(ctx, output, *s.pendingEnd); err != nil {
			return err
		}
	}
	s.resetTurn()
	return nil
}

// resetTurn clears per-turn state for the next turn.
func (s *TranscriptReorderStage) resetTurn() {
	s.userEmitted = false
	s.buffered = s.buffered[:0]
	s.pendingEnd = nil
}

// flushBuffer forwards and clears any buffered assistant text elements.
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

// isUserTurnElement reports whether an element is the materialized user turn (a
// user-role Message). The turn-boundary EndOfStream element carries an
// assistant Message and is handled separately.
func isUserTurnElement(elem *StreamElement) bool {
	return elem.Message != nil && elem.Message.Role == roleUser && !elem.EndOfStream
}
