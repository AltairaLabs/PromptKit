package engine

import "github.com/AltairaLabs/PromptKit/runtime/types"

// selfPlayCostMetaKey is the Meta key under which duplex's
// streamSelfPlayUserAudio records the persona LLM call's CostInfo.
// Stored as map[string]any rather than CostInfo because the message
// passes through JSON persistence between writer and reader.
const selfPlayCostMetaKey = "self_play_cost"

// addSelfPlayCostFromMeta folds a turn's recorded self_play_cost into
// the running total. The persona-side LLM call cost is captured into
// message metadata at the duplex turn boundary (see
// duplex_executor_turns_integration.go) but lives outside
// message.CostInfo, which is reserved for the assistant turn. Without
// this fold, ConversationResult.Cost (and therefore the headline
// cost shown in the dashboard) silently undercounts a duplex run by
// roughly the persona side's spend.
//
// Numeric values may arrive as float64 (post-JSON round-trip) or as
// int / int64 (in-memory). Both forms are accepted; unknown types
// contribute zero.
func addSelfPlayCostFromMeta(total *types.CostInfo, meta map[string]interface{}) {
	if total == nil || meta == nil {
		return
	}
	raw, ok := meta[selfPlayCostMetaKey]
	if !ok {
		return
	}
	sc, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	total.InputTokens += metaInt(sc["input_tokens"])
	total.OutputTokens += metaInt(sc["output_tokens"])
	total.CachedTokens += metaInt(sc["cached_tokens"])
	total.InputCostUSD += metaFloat(sc["input_cost_usd"])
	total.OutputCostUSD += metaFloat(sc["output_cost_usd"])
	total.TotalCost += metaFloat(sc["total_cost_usd"])
}

func metaInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func metaFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
