package assertions

import runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"

// NewArenaAssertionRegistry creates a new registry with arena-specific assertion validators
func NewArenaAssertionRegistry() *runtimeValidators.Registry {
	registry := runtimeValidators.NewRegistry()

	// Register arena-specific assertion validators
	registry.Register("tools_called", NewToolsCalledValidator)
	registry.Register("tools_not_called", NewToolsNotCalledValidator)
	registry.Register("tool_calls_with_args", NewToolCallsWithArgsValidator)
	registry.Register("content_includes", NewContentIncludesValidator)
	registry.Register("content_matches", NewContentMatchesValidator)
	registry.Register("guardrail_triggered", NewGuardrailTriggeredValidator)

	// Register JSON validation assertions
	registry.Register("is_valid_json", NewIsValidJSONValidator)
	registry.Register("json_schema", NewJSONSchemaValidator)
	registry.Register("json_path", NewJSONPathValidator)

	// Register media assertion validators
	registry.Register("image_format", NewImageFormatValidator)
	registry.Register("image_dimensions", NewImageDimensionsValidator)
	registry.Register("audio_duration", NewAudioDurationValidator)
	registry.Register("audio_format", NewAudioFormatValidator)
	registry.Register("video_duration", NewVideoDurationValidator)
	registry.Register("video_resolution", NewVideoResolutionValidator)

	// LLM judge validators
	registry.Register("llm_judge", NewLLMJudgeValidator)
	// Note: conversation-level validator registered in conversation registry

	// Register multi-agent assertion validators
	registry.Register("agent_invoked", NewAgentInvokedValidator)
	registry.Register("agent_not_invoked", NewAgentNotInvokedValidator)
	registry.Register("agent_response_contains", NewAgentResponseContainsValidator)

	// Register turn-level tool trace validators
	registry.Register("tool_call_sequence", NewToolCallSequenceValidator)
	registry.Register("tool_call_count", NewToolCallCountValidator)
	registry.Register("tool_result_includes", NewToolResultIncludesValidator)
	registry.Register("tool_result_matches", NewToolResultMatchesValidator)
	registry.Register("no_tool_errors", NewNoToolErrorsValidator)
	registry.Register("tool_call_chain", NewToolCallChainValidator)

	return registry
}
