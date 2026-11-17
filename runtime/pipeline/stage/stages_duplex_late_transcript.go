package stage

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lateTranscriptState places a provider's final input transcript that
// arrives after the reply to it completed (OpenAI Realtime delivers the
// Whisper transcript of a short utterance after the reply). Such a transcript
// is the answered turn's user message: it is emitted marked
// Meta.LateTranscript, opens no tool turn, and goes into the hooks' history
// ahead of the reply it prompted. A final transcript repeating a user turn
// already materialized from incremental chunks is dropped.
//
// Response goroutine only.
type lateTranscriptState struct {
	// shown is set once the current turn's user message has been emitted.
	shown bool
	// owed is set when a reply completed with no user message for its turn,
	// and no new reply has started since: a final transcript now is that
	// turn's. owedToolTurn records that the reply made tool calls, so the
	// turn continues once its transcript is shown.
	owed, owedToolTurn bool
	// owedHistPos is where the owed turn's messages begin in the hooks'
	// history; turnHistStart is where the current turn's do.
	owedHistPos, turnHistStart int
	// lastText is the last user message emitted, and lastFromDeltas whether
	// it was assembled from incremental transcript chunks.
	lastText       string
	lastFromDeltas bool
}

// userShown records that the current turn's user message was emitted.
func (l *lateTranscriptState) userShown(text string, fromDeltas bool) {
	l.shown = true
	l.owed = false
	l.lastText, l.lastFromDeltas = text, fromDeltas
}

// repeats reports whether a final transcript repeats the last user turn shown
// from incremental chunks (it is the same text, or a shorter prefix of it).
func (l *lateTranscriptState) repeats(text string) bool {
	if !l.lastFromDeltas {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(l.lastText), strings.TrimSpace(text))
}

// placeFinalTranscript handles a final transcript on the streaming path that
// is not a new user turn: one owed to the turn just answered is emitted as
// its late user message, and a repeat of a turn already shown is dropped.
// handled reports that the chunk was consumed.
func (s *DuplexProviderStage) placeFinalTranscript(
	ctx context.Context, chunk *providers.StreamChunk, output chan<- StreamElement,
) (handled bool, err error) {
	if !s.isStreamingFinalTranscript(chunk) {
		return false, nil
	}
	text, _ := chunk.Metadata["transcription"].(string)
	if text == "" {
		return false, nil
	}
	switch {
	case s.late.owed:
		return true, s.emitLateUserTurn(ctx, text, output)
	case s.late.repeats(text):
		logger.Debug("DuplexProviderStage: dropping a final transcript of a user turn already shown")
		return true, nil
	}
	return false, nil
}

// emitLateUserTurn emits text as the answered turn's user message, marked
// late, and puts it in the hooks' history ahead of that turn's reply, then
// checks it with the input guardrails. It begins no tool turn: the turn it
// belongs to already has one.
func (s *DuplexProviderStage) emitLateUserTurn(ctx context.Context, text string, output chan<- StreamElement) error {
	user := &types.Message{Role: roleUser, Content: text,
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}}}
	toolTurn, pos := s.late.owedToolTurn, s.late.owedHistPos
	s.late.userShown(text, false)
	// A tool turn continues with its user message now shown; a finished turn
	// leaves the next one with none yet.
	s.late.shown = toolTurn
	if s.hooksActive() {
		s.history.insert(pos, user)
	}
	elem := StreamElement{Message: user}
	elem.Meta.LateTranscript = true
	if err := s.emit(ctx, elem, output); err != nil {
		return err
	}
	// It is judged as the input it is, though its reply follows it in the
	// history. A finished turn's reply completed: a block can only cut what
	// still plays. A tool turn goes on: a block stops the rest of it.
	s.guardSpokenInput(ctx, s.history.upTo(pos), !toolTurn)
	return nil
}

// noteReplyStarted records that a reply is streaming: a finished turn still
// owed its transcript will not get it now (one arriving is this reply's
// turn's). A tool turn's follow-up reply is the same turn, still owed.
func (s *DuplexProviderStage) noteReplyStarted() {
	if !s.late.owedToolTurn {
		s.late.owed = false
	}
}

// noteChunkTurnEnd records a turn that a chunk ended (a completed reply, or
// one cut short by barge-in) on the streaming path.
func (s *DuplexProviderStage) noteChunkTurnEnd(chunk *providers.StreamChunk, elem *StreamElement, turnDone bool) {
	switch {
	case turnDone:
		s.noteTurnEnd(elem.Message != nil && len(elem.Message.ToolCalls) > 0)
	case chunk.Interrupted && elem.Meta.Interrupted:
		s.noteTurnEnd(false)
	}
}

// noteTurnEnd records that a reply ended: its turn is owed a transcript when
// no user message was shown for it (on the stream or from the input side). A
// reply without tool calls ends the turn.
func (s *DuplexProviderStage) noteTurnEnd(toolCalls bool) {
	shown := s.late.shown || s.inputUserTurn.Swap(false)
	s.late.owed = !shown
	s.late.owedToolTurn = toolCalls
	if !shown {
		s.late.owedHistPos = s.late.turnHistStart
	}
	if toolCalls {
		s.late.shown = shown
		return
	}
	s.late.shown = false
	s.late.turnHistStart = s.history.length()
}
