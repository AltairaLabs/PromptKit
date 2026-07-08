package providers

import "github.com/AltairaLabs/PromptKit/runtime/types"

// NormalizeOpenAIFinishReason maps an OpenAI-wire finish_reason onto the
// canonical vocabulary in runtime/types. Unknown (and empty) values pass
// through verbatim so a new provider reason is never silently swallowed.
// Shared by the OpenAI, vLLM, and Ollama providers, which speak this vocabulary.
func NormalizeOpenAIFinishReason(raw string) string {
	switch raw {
	case "stop", "eos_token":
		// "eos_token" is vLLM's native (non-OpenAI-compat) stop reason,
		// surfaced verbatim by some vLLM deployments/engines instead of "stop".
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonMaxOutputTokens
	case "tool_calls", "function_call":
		return types.FinishReasonToolUse
	case "content_filter":
		return types.FinishReasonSafety
	default:
		return raw
	}
}
