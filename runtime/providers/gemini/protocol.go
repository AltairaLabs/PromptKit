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
		Generation:   s.gen,
	}
	// The tool calls end the turn. They are held for the turnComplete that
	// can follow with the generation's usage (live_tool_turn.go), which goes
	// on them with the usage held so far.
	s.turnOpen = false
	if err := s.holdToolCalls(response); err != nil {
		return err
	}
	logger.Debug("Gemini tool calls held for their generation's end", "count", len(toolCalls))
	return nil
}
