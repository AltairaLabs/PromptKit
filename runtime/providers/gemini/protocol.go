package gemini

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
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

	if err := s.sendChunk(&response); err != nil {
		return err
	}
	logger.Debug("Gemini tool calls emitted", "count", len(toolCalls))
	return nil
}

// buildCostInfo prices usage metadata through the shared pricing engine, by
// modality, against the descriptor resolved when the session was created,
// together with any usage carried from a tool-call turn.
func (s *StreamSession) buildCostInfo(usage *UsageMetadata) *types.CostInfo {
	if usage == nil {
		return nil
	}
	tokens := usage.tokens()
	if s.carriedUsage != nil { // a tool-call turn's usage (carryToolCallUsage)
		tokens = addTokenUsage(tokens, *s.carriedUsage)
		s.carriedUsage = nil
	}
	ci := base.PriceUsage(s.pricing, s.providerID, base.ProviderTypeInference, tokens, nil, 0)
	return &ci
}
