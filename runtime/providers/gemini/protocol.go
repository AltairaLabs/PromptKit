package gemini

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// handleToolCalls processes tool calls from the server.
func (s *StreamSession) handleToolCalls(toolCall *ToolCallMsg) error {
	toolCalls := make([]types.MessageToolCall, len(toolCall.FunctionCalls))
	for i, fc := range toolCall.FunctionCalls {
		argsJSON, err := json.Marshal(fc.Args)
		if err != nil {
			argsJSON = []byte("{}")
		}
		toolCalls[i] = types.MessageToolCall{
			ID:   fc.ID,
			Name: fc.Name,
			Args: argsJSON,
		}
	}

	finishReason := types.FinishReasonToolUse
	response := providers.StreamChunk{
		ToolCalls:    toolCalls,
		FinishReason: &finishReason,
	}
	// The tool calls end the turn. Gemini 3.x reports its usage on a
	// turnComplete that follows (carryToolCallUsage).
	response.CostInfo = s.takeTurnCost()
	s.turnOpen = false

	if err := s.sendChunk(&response); err != nil {
		return err
	}
	logger.Debug("Gemini tool calls emitted", "count", len(toolCalls))
	return nil
}
