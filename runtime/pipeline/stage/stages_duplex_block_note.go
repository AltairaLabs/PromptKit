package stage

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A guardrail's message is spoken outside the model's conversation (a cue),
// so a realtime model never learns that its reply was withheld: asked "which
// one?" after "Sorry, I can't say that word.", it says the word again, and is
// cut again. After a block the stage adds a note to the model's conversation
// (providers.ContextNoter) that the model reads with the turns that follow and
// does not answer. The caller neither hears nor sees it; it is not part of the
// transcript, and is reported as a context.note_added event.
//
// An output block's note says the reply was withheld. An input block's turn
// is forgotten where the session can (providers.TurnForgetter), which leaves
// the model with a conversation that skips a turn it took part in: its note
// says the caller's message was removed, so the model neither answers it nor
// treats the caller's next words as following its own last reply. Where the
// turn cannot be forgotten (Gemini Live), the same note tells the model not to
// answer what it heard. A blocked typed turn never reaches the model, so it
// gets no note.

const (
	// blockedReplyNote is the note after an output guardrail withheld the
	// model's reply; %s names the guardrail.
	blockedReplyNote = "Your previous reply was withheld because it broke a content rule (%s), " +
		"and the user did not hear the rest of it. Do not repeat that content; " +
		"continue the conversation without it."
	// blockedInputNote is the note after an input guardrail blocked the
	// caller's spoken turn; %s names the guardrail.
	blockedInputNote = "The user's last message was blocked by a content rule (%s) and removed " +
		"from the conversation. Do not answer it or guess what it said; continue the conversation."
	// contextNoteReasonGuardrail is the context.note_added reason of a
	// guardrail's note.
	contextNoteReasonGuardrail = "guardrail"
)

// blockNote is a guardrail block the model is still to be told of.
type blockNote struct {
	direction string
	validator string
	turn      int
}

// noteBlock queues the note for a block by decision d on direction's side;
// the next cutReply adds it (sendBlockNotes). Response goroutine only.
func (s *DuplexProviderStage) noteBlock(d hooks.Decision, direction string) {
	s.pendingNotes = append(s.pendingNotes,
		blockNote{direction: direction, validator: guardrailName(d), turn: s.hookTurnIndex()})
}

// guardrailName names the guardrail behind d: its pack ID, else its type,
// else "unnamed".
func guardrailName(d hooks.Decision) string {
	if decl, ok := d.Metadata[hooks.MetadataKeyValidatorDeclaration].(*packspec.Validator); ok && decl != nil &&
		decl.ID != "" {
		return decl.ID
	}
	if t, _ := d.Metadata["validator_type"].(string); t != "" {
		return t
	}
	return "unnamed"
}

// sendBlockNotes adds the queued notes to the model's conversation, when the
// session can take them, and reports each as a context.note_added event. A
// note the session fails to take is logged.
func (s *DuplexProviderStage) sendBlockNotes(ctx context.Context) {
	notes := s.pendingNotes
	s.pendingNotes = nil
	noter, ok := s.session.(providers.ContextNoter)
	if !ok {
		return
	}
	for _, n := range notes {
		format := blockedReplyNote
		if n.direction == hooks.DirectionInput {
			format = blockedInputNote
		}
		text := fmt.Sprintf(format, n.validator)
		if err := noter.AddContextNote(ctx, text); err != nil {
			logger.Warn("DuplexProviderStage: the provider did not take the guardrail's note", "error", err)
			continue
		}
		if s.emitter != nil {
			s.emitter.ContextNoteAddedCtx(ctx, &events.ContextNoteData{
				Text: text, Reason: contextNoteReasonGuardrail, Direction: n.direction,
				Validator: n.validator, TurnIndex: n.turn,
			})
		}
	}
}
