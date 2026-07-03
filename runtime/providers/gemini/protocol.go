package gemini

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

	finishReason := "tool_calls"
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

// buildCostInfo creates cost information from usage metadata.
func (s *StreamSession) buildCostInfo(usage *UsageMetadata) *types.CostInfo {
	if usage == nil {
		return nil
	}

	inputTokens := usage.PromptTokenCount
	outputTokens := usage.ResponseTokenCount

	var inputCostUSD, outputCostUSD, totalCost float64
	if s.inputCostPer1K > 0 && s.outputCostPer1K > 0 {
		inputCostUSD = float64(inputTokens) / tokensPerThousand * s.inputCostPer1K
		outputCostUSD = float64(outputTokens) / tokensPerThousand * s.outputCostPer1K
		totalCost = inputCostUSD + outputCostUSD
	}

	return &types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  inputCostUSD,
		OutputCostUSD: outputCostUSD,
		TotalCost:     totalCost,
	}
}
