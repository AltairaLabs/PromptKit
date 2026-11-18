package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// AddContextNote implements providers.ContextNoter: it adds text to the
// conversation as a system message (conversation.item.create), which the
// model reads with the turns that follow. It asks for no response, so the
// model says nothing to it.
func (s *RealtimeSession) AddContextNote(_ context.Context, text string) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New(errSessionClosed)
	}
	event := ConversationItemCreateEvent{
		ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventItemCreate},
		Item: ConversationItem{
			Type:    typeMessage,
			Role:    roleSystem,
			Content: []ConversationContent{{Type: contentInputText, Text: text}},
		},
	}
	if err := s.ws.Send(event); err != nil {
		return fmt.Errorf("add a note to the conversation: %w", err)
	}
	return nil
}

// AddContextNote forwards to the underlying session if it implements
// providers.ContextNoter — hidden, a blocked turn would leave the model
// unaware that its reply, or the caller's input, was withheld.
func (s *realtimeSessionBookkeeping) AddContextNote(ctx context.Context, text string) error {
	if n, ok := s.StreamInputSession.(providers.ContextNoter); ok {
		return n.AddContextNote(ctx, text)
	}
	return errors.New("realtime session cannot add a note to the conversation")
}

var _ providers.ContextNoter = (*RealtimeSession)(nil)
