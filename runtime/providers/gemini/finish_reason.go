package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// normalizeFinishReason maps a Gemini finishReason onto the canonical
// vocabulary in runtime/types. Unknown/empty values pass through verbatim.
func normalizeFinishReason(raw string) string {
	switch raw {
	case "STOP":
		return types.FinishReasonStop
	case finishReasonMaxTokens:
		return types.FinishReasonMaxOutputTokens
	case finishReasonSafety, finishReasonRecitation, "PROHIBITED_CONTENT", "SPII", "BLOCKLIST":
		return types.FinishReasonSafety
	case "MALFORMED_FUNCTION_CALL":
		return types.FinishReasonRefusal
	default:
		return raw
	}
}

// turnFinishReason is the canonical finish reason of a finished turn. Gemini
// ends a turn that calls functions with STOP, where every other provider (and
// Gemini's own Live and Interactions paths) reports tool_use, so a STOP turn
// that carries tool calls is reported as tool_use.
func turnFinishReason(raw string, hasToolCalls bool) string {
	normalized := normalizeFinishReason(raw)
	if hasToolCalls && normalized == types.FinishReasonStop {
		return types.FinishReasonToolUse
	}
	return normalized
}
