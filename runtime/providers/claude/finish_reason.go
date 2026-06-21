package claude

import "github.com/AltairaLabs/PromptKit/runtime/types"

// stopReasonToolUse is the Anthropic wire stop_reason indicating the model
// stopped to call a tool. Declared here to keep the literal out of the switch
// (the same string is used for the content-block type elsewhere in the package).
const stopReasonToolUse = "tool_use"

// normalizeFinishReason maps an Anthropic stop_reason onto the canonical
// vocabulary in runtime/types. Unknown/empty values pass through verbatim.
func normalizeFinishReason(raw string) string {
	switch raw {
	case "end_turn", "stop_sequence", "pause_turn":
		return types.FinishReasonStop
	case "max_tokens":
		return types.FinishReasonMaxOutputTokens
	case stopReasonToolUse:
		return types.FinishReasonToolUse
	case "refusal":
		return types.FinishReasonRefusal
	default:
		return raw
	}
}
