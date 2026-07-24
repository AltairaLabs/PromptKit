package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TranscriptReorderStage guarantees that, within a turn, the user's input
// transcript is emitted before that turn's assistant text — even when the
// provider delivers the transcript late (e.g. OpenAI Realtime, whose Whisper
// transcription lands after the assistant response has already started).
//
// It buffers assistant TEXT elements until the user turn element for that turn
// appears, then flushes them in order and streams the rest live. AUDIO elements
// pass through immediately, so playback stays realtime — only the transcript log
// is reordered. If a turn ends with no transcript, a configurable placeholder
// user turn is emitted so the reply is never shown with no prompt.
//
// The mechanism is provider-agnostic; whether it is wired is decided upstream
// (providers that emit the transcript first don't need it). It is a Transform
// stage: state is per-turn and reset on each turn boundary, so a single instance
// handles one continuous duplex conversation.
type TranscriptReorderStage struct {
	BaseStage

	// placeholder is the user-turn content synthesized when a turn ends without a
	// transcript. Empty means "emit nothing" — the assistant text is flushed with
	// no user turn.
	placeholder string

	// buffered holds assistant text elements seen before the current turn's user
	// element. Flushed (in order) once the user element arrives or the turn ends.
	buffered []StreamElement

	// userEmitted is true once the user turn element for the current turn has been
	// forwarded; subsequent assistant text for the turn streams live.
	userEmitted bool
}

// NewTranscriptReorderStage creates a reorder stage with the given missing-turn
// placeholder text (empty to omit the user turn when a transcript never arrives).
func NewTranscriptReorderStage(placeholder string) *TranscriptReorderStage {
	return &TranscriptReorderStage{
		BaseStage:   NewBaseStage("transcript-reorder", StageTypeTransform),
		placeholder: placeholder,
	}
}

// Process implements the Stage interface.
func (s *TranscriptReorderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if err := s.handle(ctx, elem, output); err != nil {
			return err
		}
	}
	// Input closed mid-turn (no trailing boundary): flush whatever is buffered so
	// nothing is silently dropped.
	return s.flushBuffer(ctx, output)
}

// handle routes a single element per the reorder rules.
func (s *TranscriptReorderStage) handle(ctx context.Context, elem StreamElement, output chan<- StreamElement) error {
	switch {
	case isUserTurnElement(&elem):
		// The user turn arrived — emit it, then release everything buffered for
		// this turn so the transcript reads user-then-assistant.
		if err := s.send(ctx, output, elem); err != nil {
			return err
		}
		s.userEmitted = true
		return s.flushBuffer(ctx, output)

	case elem.Audio != nil:
		// Audio is realtime — never buffered.
		return s.send(ctx, output, elem)

	case elem.EndOfStream || elem.Meta.Interrupted:
		// Turn boundary. If no user turn was emitted, synthesize the placeholder so
		// the reply isn't shown with no prompt. Then flush and forward the boundary.
		if !s.userEmitted && s.placeholder != "" {
			ph := StreamElement{Message: &types.Message{Role: roleUser, Content: s.placeholder}}
			if err := s.send(ctx, output, ph); err != nil {
				return err
			}
		}
		if err := s.flushBuffer(ctx, output); err != nil {
			return err
		}
		if err := s.send(ctx, output, elem); err != nil {
			return err
		}
		s.userEmitted = false
		s.buffered = s.buffered[:0]
		return nil

	case elem.Text != nil && !s.userEmitted:
		// Assistant text ahead of the transcript — hold it.
		s.buffered = append(s.buffered, elem)
		return nil

	default:
		// Assistant text after the user turn, reasoning, errors, control — forward.
		return s.send(ctx, output, elem)
	}
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
