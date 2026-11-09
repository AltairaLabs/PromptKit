package gemini

import (
	"context"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// SeedHistory adds the prior conversation as one client_content message whose
// turns are the user and model messages with text, with turn_complete false so
// the model does not answer it.
func (s *StreamSession) SeedHistory(_ context.Context, messages []types.Message) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	ws := s.ws
	s.mu.Unlock()

	history := providers.HistoryTurns(messages)
	if len(history) == 0 {
		return nil
	}
	turns := make([]map[string]interface{}, 0, len(history))
	for _, turn := range history {
		role := roleUser
		if turn.Role == "assistant" {
			role = roleModel
		}
		turns = append(turns, map[string]interface{}{
			wireKeyRole:  role,
			wireKeyParts: []interface{}{map[string]interface{}{deltaTypeText: turn.Text}},
		})
	}
	return ws.Send(map[string]interface{}{
		liveKeyClientContent: map[string]interface{}{
			liveKeyTurns:        turns,
			liveKeyTurnComplete: false,
		},
	})
}

// SupportsStreamHistory reports that Gemini Live sessions take prior
// conversation (see StreamSession.SeedHistory).
func (p *Provider) SupportsStreamHistory() bool { return true }

var (
	_ providers.StreamHistorySession = (*StreamSession)(nil)
	_ providers.StreamHistorySupport = (*Provider)(nil)
)
