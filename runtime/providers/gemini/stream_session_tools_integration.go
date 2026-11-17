package gemini

import (
	"context"
	"errors"
	"log/slog"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// ToolDefinition represents a function/tool that the model can call.
// This follows the Gemini function calling schema.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON Schema for parameters
}

// Ensure StreamSession implements ToolResponseSupport
var _ providers.ToolResponseSupport = (*StreamSession)(nil)

// SendToolResponse sends a single tool execution result back to Gemini.
// The toolCallID must match the ID from the FunctionCall.
// The result should be a JSON-serializable string (typically JSON).
func (s *StreamSession) SendToolResponse(ctx context.Context, toolCallID, result string) error {
	return s.SendToolResponses(ctx, []providers.ToolResponse{
		{
			ToolCallID: toolCallID,
			Result:     result,
		},
	})
}

// SendToolResponses sends multiple tool execution results back to Gemini.
// This is used when the model makes parallel tool calls.
// After receiving the tool responses, Gemini will continue generating.
// A result's media is sent in its function response's parts where the Live
// API takes it, and described in its response where it does not
// (live_tool_media.go).
func (s *StreamSession) SendToolResponses(ctx context.Context, responses []providers.ToolResponse) error {
	// Load the media and build the message first: it can take a while, and the
	// socket to send on is read under the lock only once it is ready.
	msg := s.toolResponseWire(ctx, responses)
	logToolResponses(ctx, responses)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	ws := s.ws // the current socket: a reconnect replaces it
	s.mu.Unlock()

	return ws.Send(msg)
}

// logToolResponses logs, at debug, which calls a round answers and how many
// parts each result carries; never their content.
func logToolResponses(ctx context.Context, responses []providers.ToolResponse) {
	if !logger.GetLogger().Enabled(ctx, slog.LevelDebug) {
		return
	}
	ids := make([]string, len(responses))
	parts := make([]int, len(responses))
	for i := range responses {
		ids[i] = responses[i].ToolCallID
		parts[i] = len(responses[i].Parts)
	}
	logger.Debug("Gemini sending tool response", "call_ids", ids, "parts", parts)
}
