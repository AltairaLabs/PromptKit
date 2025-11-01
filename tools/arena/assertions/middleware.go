package validators

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	runtimeValidators "github.com/AltairaLabs/PromptKit/runtime/validators"
)

// assertionMiddleware implements pipeline.Middleware for turn-level assertions
type assertionMiddleware struct {
	registry   *runtimeValidators.Registry
	assertions []runtimeValidators.ValidatorConfig
}

// ArenaAssertionMiddleware creates middleware that validates assertions after LLM responses
func ArenaAssertionMiddleware(registry *runtimeValidators.Registry, assertions []runtimeValidators.ValidatorConfig) pipeline.Middleware {
	return &assertionMiddleware{
		registry:   registry,
		assertions: assertions,
	}
}

// Process implements pipeline.Middleware.Process
func (m *assertionMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	var validation_errors []error

	// If no assertions configured, nothing to validate
	if len(m.assertions) != 0 {

		// Find the last assistant message to validate
		lastAssistantMsgIdx := -1
		for i := len(ctx.Messages) - 1; i >= 0; i-- {
			if ctx.Messages[i].Role == "assistant" {
				lastAssistantMsgIdx = i
				break
			}
		}

		if lastAssistantMsgIdx == -1 {
			// No assistant message to validate, skip assertions
		} else {

			// Get pointer to the actual message in the slice
			lastAssistantMsg := &ctx.Messages[lastAssistantMsgIdx]

			// Prepare context for validators
			// Collect messages from THIS TURN ONLY (not from entire conversation history)
			// Use Source field to identify new messages (not loaded from StateStore)
			var turnMessages []types.Message
			for _, msg := range ctx.Messages {
				// Include messages not from statestore (current turn)
				if msg.Source != "statestore" {
					turnMessages = append(turnMessages, msg)
				}
			}

			// Run all assertions
			results := make(map[string]interface{})
			for _, assertionConfig := range m.assertions {
				// Inject context into validator params
				params := make(map[string]interface{})
				for k, v := range assertionConfig.Params {
					params[k] = v
				}
				// Pass deep clone of turn messages to validators
				params["_turn_messages"] = deepCloneMessages(turnMessages)
				params["_execution_context_messages"] = deepCloneMessages(ctx.Messages)

				// Create validator
				factory, ok := m.registry.Get(assertionConfig.Type)
				if !ok {
					logger.Warn("unknown assertion validator type %q", assertionConfig.Type)
					continue
				}

				validator := factory(params)

				// Validate
				result := validator.Validate(lastAssistantMsg.Content, params)
				results[assertionConfig.Type] = result

				// Fail fast on assertion failure
				if !result.OK {
					validation_errors = append(validation_errors, fmt.Errorf("assertion %q failed with details: %v", assertionConfig.Type, result.Details))
				}
			}

			// Attach results to message metadata
			if lastAssistantMsg.Meta == nil {
				lastAssistantMsg.Meta = make(map[string]interface{})
			}

			lastAssistantMsg.Meta["assertions"] = results
		}

	}
	// Proceed to next middleware/handler. Generally, this calls teh state store middleware to save the results
	next_err := next()

	if len(validation_errors) > 0 {
		return fmt.Errorf("validation failed: %v", validation_errors)
	}

	return next_err
}

// StreamChunk implements pipeline.Middleware.StreamChunk
// Assertions are not validated during streaming, only after completion
func (m *assertionMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

// deepCloneMessages creates a deep copy of messages to prevent validators from mutating the original
func deepCloneMessages(messages []types.Message) []types.Message {
	if messages == nil {
		return nil
	}

	cloned := make([]types.Message, len(messages))
	for i, msg := range messages {
		cloned[i] = types.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
			LatencyMs: msg.LatencyMs,
			Source:    msg.Source, // Preserve Source field
		}

		// Deep clone ToolCalls
		if len(msg.ToolCalls) > 0 {
			cloned[i].ToolCalls = make([]types.MessageToolCall, len(msg.ToolCalls))
			copy(cloned[i].ToolCalls, msg.ToolCalls)
		}

		// Deep clone ToolResult
		if msg.ToolResult != nil {
			result := *msg.ToolResult
			cloned[i].ToolResult = &result
		}

		// Deep clone Meta
		if msg.Meta != nil {
			cloned[i].Meta = make(map[string]interface{})
			for k, v := range msg.Meta {
				cloned[i].Meta[k] = v
			}
		}

		// Clone CostInfo
		if msg.CostInfo != nil {
			costInfo := *msg.CostInfo
			cloned[i].CostInfo = &costInfo
		}

		// Clone Validations
		if len(msg.Validations) > 0 {
			cloned[i].Validations = make([]types.ValidationResult, len(msg.Validations))
			copy(cloned[i].Validations, msg.Validations)
		}
	}

	return cloned
}
