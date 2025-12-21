// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// Ensure RealtimeSession implements ToolResponseSupport
var _ providers.ToolResponseSupport = (*RealtimeSession)(nil)

// SendToolResponse sends the result of a tool execution back to the model.
func (s *RealtimeSession) SendToolResponse(ctx context.Context, toolCallID, result string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	// Create a function_call_output conversation item
	item := ConversationItem{
		Type:   "function_call_output",
		CallID: toolCallID,
		Output: result,
	}

	event := ConversationItemCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "conversation.item.create",
		},
		Item: item,
	}

	if err := s.ws.Send(event); err != nil {
		return fmt.Errorf("failed to send tool response: %w", err)
	}

	// Trigger a response to continue the conversation
	responseEvent := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "response.create",
		},
	}

	return s.ws.Send(responseEvent)
}

// SendToolResponses sends multiple tool results at once (for parallel tool calls).
func (s *RealtimeSession) SendToolResponses(ctx context.Context, responses []providers.ToolResponse) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	s.mu.Unlock()

	// Send each tool response as a separate conversation item
	for _, resp := range responses {
		item := ConversationItem{
			Type:   "function_call_output",
			CallID: resp.ToolCallID,
			Output: resp.Result,
		}

		event := ConversationItemCreateEvent{
			ClientEvent: ClientEvent{
				EventID: s.nextEventID(),
				Type:    "conversation.item.create",
			},
			Item: item,
		}

		if err := s.ws.Send(event); err != nil {
			return fmt.Errorf("failed to send tool response for %s: %w", resp.ToolCallID, err)
		}
	}

	// Trigger a response after all tool results are sent
	responseEvent := ResponseCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    "response.create",
		},
	}

	return s.ws.Send(responseEvent)
}
