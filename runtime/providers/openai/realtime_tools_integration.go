// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Ensure RealtimeSession implements ToolResponseSupport and StreamToolUpdater.
var (
	_ providers.ToolResponseSupport = (*RealtimeSession)(nil)
	_ providers.StreamToolUpdater   = (*RealtimeSession)(nil)
)

// sessionToolsUpdate is a session.update that changes only the session's
// tools and tool_choice. Tools is never omitted: an empty array clears them.
type sessionToolsUpdate struct {
	ClientEvent
	Session sessionToolsConfig `json:"session"`
}

// sessionToolsConfig is the session part of sessionToolsUpdate.
type sessionToolsConfig struct {
	Type       string            `json:"type"`
	Tools      []RealtimeToolDef `json:"tools"`
	ToolChoice interface{}       `json:"tool_choice,omitempty"`
}

// UpdateTools replaces the live session's tools with a session.update, keeping
// the session's tool_choice. Implements providers.StreamToolUpdater.
func (s *RealtimeSession) UpdateTools(_ context.Context, tools []providers.StreamingToolDefinition) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(errSessionClosed)
	}
	choice := s.config.ToolChoice
	s.mu.Unlock()

	event := sessionToolsUpdate{
		ClientEvent: ClientEvent{EventID: s.nextEventID(), Type: realtimeEventSessionUpdate},
		Session: sessionToolsConfig{
			Type:       sessionTypeRealtime,
			Tools:      realtimeToolDefs(tools),
			ToolChoice: realtimeToolChoice(choice),
		},
	}
	if err := s.ws.Send(event); err != nil {
		return fmt.Errorf("failed to update session tools: %w", err)
	}
	return nil
}

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
		Type:   typeFunctionCallOutput,
		CallID: toolCallID,
		Output: result,
	}

	event := ConversationItemCreateEvent{
		ClientEvent: ClientEvent{
			EventID: s.nextEventID(),
			Type:    realtimeEventItemCreate,
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
			Type:    realtimeEventResponseCreate,
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
			Type:   typeFunctionCallOutput,
			CallID: resp.ToolCallID,
			Output: resp.Result,
		}

		event := ConversationItemCreateEvent{
			ClientEvent: ClientEvent{
				EventID: s.nextEventID(),
				Type:    realtimeEventItemCreate,
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
			Type:    realtimeEventResponseCreate,
		},
	}

	return s.ws.Send(responseEvent)
}
