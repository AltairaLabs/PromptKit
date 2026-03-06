package statestore

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// extractValidations extracts all validations from messages with turn indices
func (s *ArenaStateStore) extractValidations(state *ArenaConversationState) []ValidationResult {
	var validations []ValidationResult

	userTurns := 0
	for i := range state.Messages {
		if state.Messages[i].Role == "user" {
			userTurns++
		}
		msg := &state.Messages[i]
		if len(msg.Validations) > 0 {
			// Turn index is based on the number of user messages seen so far,
			// which is robust against tool-call or system messages mid-conversation.
			turnIndex := userTurns

			for _, v := range msg.Validations {
				validations = append(validations, ValidationResult{
					TurnIndex:     turnIndex,
					Timestamp:     v.Timestamp,
					ValidatorType: v.ValidatorType,
					Passed:        v.Passed,
					Details:       v.Details,
				})
			}
		}
	}

	return validations
}

// computeTotalCost aggregates cost info from all messages
func (s *ArenaStateStore) computeTotalCost(state *ArenaConversationState) types.CostInfo {
	var totalCost types.CostInfo

	for _, msg := range state.Messages {
		if msg.CostInfo != nil {
			totalCost.InputTokens += msg.CostInfo.InputTokens
			totalCost.OutputTokens += msg.CostInfo.OutputTokens
			totalCost.CachedTokens += msg.CostInfo.CachedTokens
			totalCost.InputCostUSD += msg.CostInfo.InputCostUSD
			totalCost.OutputCostUSD += msg.CostInfo.OutputCostUSD
			totalCost.CachedCostUSD += msg.CostInfo.CachedCostUSD
			totalCost.TotalCost += msg.CostInfo.TotalCost
		}
	}

	return totalCost
}

// computeToolStats aggregates tool usage from all messages
func (s *ArenaStateStore) computeToolStats(state *ArenaConversationState) *types.ToolStats {
	byTool := make(map[string]int)
	totalCalls := 0

	for _, msg := range state.Messages {
		for _, toolCall := range msg.ToolCalls {
			byTool[toolCall.Name]++
			totalCalls++
		}
	}

	if totalCalls == 0 {
		return nil
	}

	return &types.ToolStats{
		TotalCalls: totalCalls,
		ByTool:     byTool,
	}
}

// extractViolationsFlat returns violations as a flat list (for RunResult compatibility)
func (s *ArenaStateStore) extractViolationsFlat(state *ArenaConversationState) []types.ValidationError {
	var violations []types.ValidationError

	for _, msg := range state.Messages {
		for _, v := range msg.Validations {
			if !v.Passed {
				violations = append(violations, types.ValidationError{
					Type:   v.ValidatorType,
					Detail: fmt.Sprintf("%v", v.Details),
				})
			}
		}
	}

	return violations
}
