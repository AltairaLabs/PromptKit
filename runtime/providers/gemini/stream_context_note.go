package gemini

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// AddContextNote implements providers.ContextNoter: Gemini Live has no
// system turn mid-session, so the note goes as context that does not
// complete the turn (SendSystemContext), and the model does not answer it.
// Client content interrupts a generation in progress, so the pipeline sends
// a note only once the turn it reports has ended (turnComplete).
func (s *StreamSession) AddContextNote(ctx context.Context, text string) error {
	return s.SendSystemContext(ctx, text)
}

var _ providers.ContextNoter = (*StreamSession)(nil)
