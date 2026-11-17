package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// reasonMalformedFunctionCall is the finish reason, and the Live turn-complete
// reason, of a turn whose function call was invalid.
const reasonMalformedFunctionCall = "MALFORMED_FUNCTION_CALL"

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
	case reasonMalformedFunctionCall:
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

// liveTurnFinishReason maps a Gemini Live turnCompleteReason onto the
// canonical vocabulary. Every documented reason other than those listed is
// one of Gemini's safety filters ending the turn (prohibited input, a
// blocklist, generated content or audio failing a check), so an unlisted
// reason reads as safety rather than as a normal end.
func liveTurnFinishReason(reason string) string {
	switch reason {
	case "", "TURN_COMPLETE_REASON_UNSPECIFIED", "NEED_MORE_INPUT", "MAX_REGENERATION_REACHED":
		return finishReasonComplete
	case reasonMalformedFunctionCall, "RESPONSE_REJECTED":
		return types.FinishReasonRefusal
	default:
		return types.FinishReasonSafety
	}
}
