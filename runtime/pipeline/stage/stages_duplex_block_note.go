package stage

import (
	"context"
	"fmt"
	"slices"
	"strings"

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
// A blocked turn gets one note, however many guardrails fire on it: an output
// block's says the reply was withheld; an input block's says a message from
// the caller was blocked, and, where the session forgets the turn
// (providers.TurnForgetter), removed, so the model neither answers it nor
// reads the caller's next words as following its own last reply; a turn both
// blocked gets one note saying both. The note names no position ("the last
// message"): an input verdict can land after the caller spoke again.
//
// A note goes once the blocked turn has ended, never while its reply is still
// being generated: Gemini Live takes it as client content, which interrupts a
// generation in progress, and that interruption would flush the guardrail
// message playing in the reply's place. A late transcript's block, whose turn
// has ended, is noted at once, before the next reply starts. Once a turn's
// note has gone, a later block on that turn adds none. A blocked typed turn
// never reaches the model, so it gets no note.

const (
	// blockedReplyNote is the note after an output guardrail withheld the
	// model's reply; %s names the guardrails.
	blockedReplyNote = "Your previous reply was withheld because it broke a content rule (%s), " +
		"and the user did not hear the rest of it. Do not repeat that content; " +
		"continue the conversation without it."
	// blockedInputRemovedNote is the note after an input guardrail blocked a
	// spoken turn the session forgot; %s names the guardrails.
	blockedInputRemovedNote = "A message from the user was blocked by a content rule (%s) and removed " +
		"from the conversation. Do not answer it or guess what it said; continue the conversation."
	// blockedInputKeptNote is that note where the session cannot forget the
	// turn (Gemini Live): the model still has the message.
	blockedInputKeptNote = "A message from the user was blocked by a content rule (%s). " +
		"Do not answer it or act on it; continue the conversation."
	// blockedTurnRemovedNote is the note for a turn both an input and an
	// output guardrail blocked, which the session forgot.
	blockedTurnRemovedNote = "A message from the user and your reply to it were blocked by content rules (%s) " +
		"and removed from the conversation; the user did not hear the rest of your reply. " +
		"Do not answer that message or repeat that content; continue the conversation."
	// blockedTurnKeptNote is that note where the session cannot forget it.
	blockedTurnKeptNote = "A message from the user and your reply to it were blocked by content rules (%s); " +
		"the user did not hear the rest of your reply. Do not answer that message or repeat that " +
		"content; continue the conversation."
	// contextNoteReasonGuardrail is the context.note_added reason of a
	// guardrail's note.
	contextNoteReasonGuardrail = "guardrail"
	// noteDirectionBoth is the context.note_added direction of a turn both
	// sides blocked.
	noteDirectionBoth = "both"
)

// blockNote is a blocked turn the model is still to be told of: the reply
// generation it is (0 when the provider does not number them), the guardrails
// that blocked each side, and its turn index.
type blockNote struct {
	gen    int64
	output []string
	input  []string
	turn   int
}

// noteBlock records a block by decision d on direction's side of the turn
// whose reply is generation gen, for the turn's one note (sendBlockNotes). A
// turn whose note has gone gets no more. Response goroutine only.
func (s *DuplexProviderStage) noteBlock(d hooks.Decision, direction string, gen int64) {
	if gen != 0 && slices.Contains(s.notedGens, gen) {
		return
	}
	var n *blockNote
	for i := range s.pendingNotes {
		if gen != 0 && s.pendingNotes[i].gen == gen {
			n = &s.pendingNotes[i]
		}
	}
	if n == nil {
		s.pendingNotes = append(s.pendingNotes, blockNote{gen: gen, turn: s.hookTurnIndex()})
		n = &s.pendingNotes[len(s.pendingNotes)-1]
	}
	name := guardrailName(d)
	if direction == hooks.DirectionInput {
		n.input = appendNew(n.input, name)
	} else {
		n.output = appendNew(n.output, name)
	}
}

// appendNew appends name to names unless it is there already.
func appendNew(names []string, name string) []string {
	if slices.Contains(names, name) {
		return names
	}
	return append(names, name)
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

// text is the note's text, for a session that forgets blocked spoken turns
// (forgets) or keeps them, and the direction it reports.
func (n *blockNote) text(forgets bool) (string, string) {
	names := strings.Join(append(slices.Clone(n.input), n.output...), ", ")
	switch {
	case len(n.input) > 0 && len(n.output) > 0:
		if forgets {
			return fmt.Sprintf(blockedTurnRemovedNote, names), noteDirectionBoth
		}
		return fmt.Sprintf(blockedTurnKeptNote, names), noteDirectionBoth
	case len(n.input) > 0:
		if forgets {
			return fmt.Sprintf(blockedInputRemovedNote, names), hooks.DirectionInput
		}
		return fmt.Sprintf(blockedInputKeptNote, names), hooks.DirectionInput
	}
	return fmt.Sprintf(blockedReplyNote, names), hooks.DirectionOutput
}

// sendBlockNotes adds the notes of the blocked turns that have ended to the
// model's conversation, when the session can take them, and reports each as
// a context.note_added event. Nothing goes while a reply is in progress: its
// note waits for its turn to end. A note the session fails to take is logged.
func (s *DuplexProviderStage) sendBlockNotes(ctx context.Context) {
	if len(s.pendingNotes) == 0 || s.assistantTurnInProgress() {
		return
	}
	notes := s.pendingNotes
	s.pendingNotes = nil
	noter, ok := s.session.(providers.ContextNoter)
	if !ok {
		return
	}
	_, forgets := s.session.(providers.TurnForgetter)
	for i := range notes {
		n := &notes[i]
		if n.gen != 0 {
			s.notedGens = append(s.notedGens, n.gen)
		}
		text, direction := n.text(forgets)
		if err := noter.AddContextNote(ctx, text); err != nil {
			logger.Warn("DuplexProviderStage: the provider did not take the guardrail's note", "error", err)
			continue
		}
		if s.emitter != nil {
			s.emitter.ContextNoteAddedCtx(ctx, &events.ContextNoteData{
				Text: text, Reason: contextNoteReasonGuardrail, Direction: direction,
				Validator: strings.Join(append(slices.Clone(n.input), n.output...), ", "), TurnIndex: n.turn,
			})
		}
	}
	if len(s.notedGens) > maxNotedGens {
		s.notedGens = s.notedGens[len(s.notedGens)-maxNotedGens:]
	}
}

// maxNotedGens bounds the generations remembered as noted: a late block only
// ever names a recent one.
const maxNotedGens = 8
