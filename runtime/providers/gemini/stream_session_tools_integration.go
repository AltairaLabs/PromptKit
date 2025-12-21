package gemini

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// ToolDefinition represents a function/tool that the model can call.
// This follows the Gemini function calling schema.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"` // JSON Schema for parameters
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
func (s *StreamSession) SendToolResponses(ctx context.Context, responses []providers.ToolResponse) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New(ErrSessionClosed)
	}
	s.mu.Unlock()

	// Build Gemini's BidiGenerateContentToolResponse format
	// Per docs: toolResponse.functionResponses[].{id, name, response}
	functionResponses := make([]map[string]interface{}, len(responses))
	for i, resp := range responses {
		// Parse result as JSON if possible, otherwise wrap as string
		var resultObj interface{}
		if err := json.Unmarshal([]byte(resp.Result), &resultObj); err != nil {
			// Result is not valid JSON, wrap it
			resultObj = map[string]interface{}{"result": resp.Result}
		}

		funcResp := map[string]interface{}{
			"id":       resp.ToolCallID,
			"response": resultObj,
		}

		// Add error flag if the tool execution failed
		if resp.IsError {
			funcResp["error"] = true
		}

		functionResponses[i] = funcResp
	}

	msg := map[string]interface{}{
		"toolResponse": map[string]interface{}{
			"functionResponses": functionResponses,
		},
	}

	// Log tool response for debugging
	if logger.DefaultLogger != nil {
		if msgJSON, err := json.MarshalIndent(msg, "", "  "); err == nil {
			logger.DefaultLogger.Debug("Gemini sending tool response", "message", string(msgJSON))
		}
	}

	return s.ws.Send(msg)
}
