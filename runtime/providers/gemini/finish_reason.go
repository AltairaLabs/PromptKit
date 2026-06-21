package gemini

import "github.com/AltairaLabs/PromptKit/runtime/types"

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
