package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Realtime content types for prior conversation: what the user said is
// input_text, what the model said is output_text.
const (
	contentInputText  = "input_text"
	contentOutputText = "output_text"
)

// SeedHistory adds the prior conversation as conversation.item.create events,
// one per user or assistant message with text, without response.create.
func (s *RealtimeSession) SeedHistory(_ context.Context, messages []types.Message) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New(errSessionClosed)
	}
	for _, turn := range providers.HistoryTurns(messages) {
		contentType := contentInputText
		if turn.Role == "assistant" {
			contentType = contentOutputText
		}
		event := ConversationItemCreateEvent{
			ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventItemCreate},
			Item: ConversationItem{
				Type:    typeMessage,
				Role:    turn.Role,
				Content: []ConversationContent{{Type: contentType, Text: turn.Text}},
			},
		}
		if err := s.ws.Send(event); err != nil {
			return fmt.Errorf("failed to seed history: %w", err)
		}
	}
	return nil
}

// SupportsStreamHistory reports that OpenAI Realtime sessions take prior
// conversation (see RealtimeSession.SeedHistory).
func (p *Provider) SupportsStreamHistory() bool { return true }

var (
	_ providers.StreamHistorySession = (*RealtimeSession)(nil)
	_ providers.StreamHistorySupport = (*Provider)(nil)
)
