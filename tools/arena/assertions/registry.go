package assertions

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// NewArenaAssertionRegistry creates a new registry with arena-specific assertion validators
func NewArenaAssertionRegistry() *runtimeValidators.Registry {
	registry := runtimeValidators.NewRegistry()

	// Register arena-specific assertion validators
	registry.Register("tools_called", NewToolsCalledValidator)
	registry.Register("tools_not_called", NewToolsNotCalledValidator)
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

	return registry
}

// conversationAdapter adapts a ConversationValidator to the turn-level Validator interface.
// It expects _metadata and _execution_context_messages to be present in params.
type conversationAdapter struct {
	cv ConversationValidator
}

func (a conversationAdapter) Validate(content string, params map[string]interface{}) runtimeValidators.ValidationResult {
	// Build a minimal ConversationContext from provided messages
	msgs, _ := params["_execution_context_messages"].([]types.Message)
	convCtx := &ConversationContext{
		AllTurns: msgs,
	}
	res := a.cv.ValidateConversation(context.Background(), convCtx, params)
	return runtimeValidators.ValidationResult{
		Passed: res.Passed,
		Details: map[string]interface{}{
			"message": res.Message,
			"details": res.Details,
		},
	}
}
