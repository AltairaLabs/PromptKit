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

// ancillaryMetaCostKeys is the list of Message.Meta keys that carry an
// ancillary cost contribution (TTS, STT, embedding, image_gen, persona-side
// LLM via self-play). Mirrors the engine-side list in
// tools/arena/engine/cost_aggregation.go — kept in sync there.
var ancillaryMetaCostKeys = []string{
	"self_play_cost",
	"tts_cost",
	"stt_cost",
	"embedding_cost",
	"image_gen_cost",
}

// computeTotalCost aggregates cost info from all messages
func (s *ArenaStateStore) computeTotalCost(state *ArenaConversationState) types.CostInfo {
	var totalCost types.CostInfo

	for i := range state.Messages {
		msg := &state.Messages[i]
		if msg.CostInfo != nil {
			totalCost.InputTokens += msg.CostInfo.InputTokens
			totalCost.OutputTokens += msg.CostInfo.OutputTokens
			totalCost.CachedTokens += msg.CostInfo.CachedTokens
			totalCost.InputCostUSD += msg.CostInfo.InputCostUSD
			totalCost.OutputCostUSD += msg.CostInfo.OutputCostUSD
			totalCost.CachedCostUSD += msg.CostInfo.CachedCostUSD
			totalCost.TotalCost += msg.CostInfo.TotalCost
			totalCost.Breakdown = append(totalCost.Breakdown, breakdownItemsForMessage(msg.CostInfo)...)
		}
		foldAncillaryMetaCosts(&totalCost, msg.Meta)
	}

	return totalCost
}

// foldAncillaryMetaCosts adds any ancillary cost stored in the message's
// Meta (TTS, STT, embedding, etc.) to the running total and emits a
// corresponding Breakdown line item. Mirrors the addAncillaryCostFromMeta
// path used by the engine-side aggregator.
func foldAncillaryMetaCosts(total *types.CostInfo, meta map[string]any) {
	if total == nil || meta == nil {
		return
	}
	for _, key := range ancillaryMetaCostKeys {
		raw, ok := meta[key]
		if !ok {
			continue
		}
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		c := costInfoFromMeta(entry)
		total.InputTokens += c.InputTokens
		total.OutputTokens += c.OutputTokens
		total.CachedTokens += c.CachedTokens
		total.InputCostUSD += c.InputCostUSD
		total.OutputCostUSD += c.OutputCostUSD
		total.CachedCostUSD += c.CachedCostUSD
		total.TotalCost += c.TotalCost
		total.Breakdown = append(total.Breakdown, breakdownItemsForMessage(&c)...)
	}
}

// costInfoFromMeta reconstructs a CostInfo from the map[string]any shape
// used to round-trip through Meta storage.
func costInfoFromMeta(m map[string]any) types.CostInfo {
	c := types.CostInfo{
		InputTokens:   metaInt(m["input_tokens"]),
		OutputTokens:  metaInt(m["output_tokens"]),
		CachedTokens:  metaInt(m["cached_tokens"]),
		InputCostUSD:  metaFloat(m["input_cost_usd"]),
		OutputCostUSD: metaFloat(m["output_cost_usd"]),
		CachedCostUSD: metaFloat(m["cached_cost_usd"]),
		TotalCost:     metaFloat(m["total_cost_usd"]),
	}
	if s, ok := m["provider_name"].(string); ok {
		c.ProviderName = s
	}
	if s, ok := m["capability"].(string); ok {
		c.Capability = s
	}
	if q, ok := m["quantities"].(map[string]any); ok {
		c.Quantities = make(map[string]float64, len(q))
		for k, v := range q {
			c.Quantities[k] = metaFloat(v)
		}
	}
	if d, ok := m["dimension_match"].(map[string]any); ok {
		c.DimensionMatch = make(map[string]string, len(d))
		for k, v := range d {
			if s, ok := v.(string); ok {
				c.DimensionMatch[k] = s
			}
		}
	}
	return c
}

func metaInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func metaFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
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
