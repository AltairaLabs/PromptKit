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

	for i := range state.Messages {
		msg := &state.Messages[i]
		if msg.CostInfo == nil {
			continue
		}
		totalCost.InputTokens += msg.CostInfo.InputTokens
		totalCost.OutputTokens += msg.CostInfo.OutputTokens
		totalCost.CachedTokens += msg.CostInfo.CachedTokens
		totalCost.InputCostUSD += msg.CostInfo.InputCostUSD
		totalCost.OutputCostUSD += msg.CostInfo.OutputCostUSD
		totalCost.CachedCostUSD += msg.CostInfo.CachedCostUSD
		totalCost.TotalCost += msg.CostInfo.TotalCost
		totalCost.Breakdown = append(totalCost.Breakdown, breakdownItemsForMessage(msg.CostInfo)...)
	}

	return totalCost
}

// breakdownItemsForMessage converts a single message's CostInfo into the line
// items it represents. Prefers Quantities (the unified path) when populated;
// otherwise falls back to deriving items from the legacy token fields.
func breakdownItemsForMessage(c *types.CostInfo) []types.CostLineItem {
	if c == nil {
		return nil
	}
	provider := c.ProviderName
	capability := c.Capability

	if len(c.Quantities) > 0 {
		items := make([]types.CostLineItem, 0, len(c.Quantities))
		var qtySum float64
		for _, q := range c.Quantities {
			qtySum += q
		}
		for unit, qty := range c.Quantities {
			usd := 0.0
			if qtySum > 0 {
				usd = c.TotalCost * (qty / qtySum)
			}
			items = append(items, types.CostLineItem{
				Provider:   provider,
				Capability: capability,
				Unit:       unit,
				Quantity:   qty,
				USD:        usd,
				Dimensions: c.DimensionMatch,
			})
		}
		return items
	}

	var items []types.CostLineItem
	if c.InputTokens > 0 {
		items = append(items, types.CostLineItem{
			Provider:   provider,
			Capability: capability,
			Unit:       "input_token",
			Quantity:   float64(c.InputTokens),
			USD:        c.InputCostUSD,
		})
	}
	if c.OutputTokens > 0 {
		items = append(items, types.CostLineItem{
			Provider:   provider,
			Capability: capability,
			Unit:       "output_token",
			Quantity:   float64(c.OutputTokens),
			USD:        c.OutputCostUSD,
		})
	}
	if c.CachedTokens > 0 {
		items = append(items, types.CostLineItem{
			Provider:   provider,
			Capability: capability,
			Unit:       "cached_token",
			Quantity:   float64(c.CachedTokens),
			USD:        c.CachedCostUSD,
		})
	}
	return items
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
