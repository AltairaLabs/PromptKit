package engine

import "github.com/AltairaLabs/PromptKit/runtime/types"

// selfPlayCostMetaKey is the Meta key under which duplex's
// streamSelfPlayUserAudio records the persona LLM call's CostInfo.
// Stored as map[string]any rather than CostInfo because the message
// passes through JSON persistence between writer and reader.
const selfPlayCostMetaKey = "self_play_cost"

// ttsCostMetaKey is the Meta key under which TTS synthesis cost is recorded.
// Set by stampTTSCostInMeta (duplex persona path) and by TTSStage.processElement
// (pipeline path) after a successful Synthesize call.
const ttsCostMetaKey = "tts_cost"

// ancillaryCostMetaKeys lists every Message.Meta key that holds an
// ancillary cost contribution to fold into the total. Ordered
// alphabetically.
var ancillaryCostMetaKeys = []string{
	selfPlayCostMetaKey,
	ttsCostMetaKey,
}

// addAncillaryCostFromMeta folds every recognized ancillary cost stored
// in a message's Meta into the running total. The persona-side LLM call
// cost (self_play_cost) is the original case — see #1105. Future
// migrations (TTS, STT, embedding) extend the same pattern by adding to
// ancillaryCostMetaKeys.
//
// Numeric values may arrive as float64 (post-JSON round-trip) or as
// int / int64 (in-memory). Both forms are accepted; unknown types
// contribute zero.
func addAncillaryCostFromMeta(total *types.CostInfo, meta map[string]interface{}) {
	if total == nil || meta == nil {
		return
	}
	for _, key := range ancillaryCostMetaKeys {
		addCostFromMetaKey(total, meta, key)
	}
}

func addCostFromMetaKey(total *types.CostInfo, meta map[string]interface{}, key string) {
	raw, ok := meta[key]
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

// addSelfPlayCostFromMeta is a back-compat alias for addAncillaryCostFromMeta.
//
// Deprecated: use addAncillaryCostFromMeta directly; this wrapper exists so
// existing call sites continue to compile.
func addSelfPlayCostFromMeta(total *types.CostInfo, meta map[string]interface{}) {
	addAncillaryCostFromMeta(total, meta)
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
