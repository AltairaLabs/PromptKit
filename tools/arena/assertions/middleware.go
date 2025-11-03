package assertions

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
	assertions []AssertionConfig
}

// ArenaAssertionMiddleware creates middleware that validates assertions after LLM responses
func ArenaAssertionMiddleware(registry *runtimeValidators.Registry, assertions []AssertionConfig) pipeline.Middleware {
	return &assertionMiddleware{
		registry:   registry,
		assertions: assertions,
	}
}

// Process implements pipeline.Middleware.Process
func (m *assertionMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	// Skip validation if no assertions configured
	if len(m.assertions) == 0 {
		return next()
	}

	// Run assertions and collect any errors
	validationErrors := m.runAssertions(ctx)

	// Execute next middleware
	nextErr := next()

	// Return validation errors if any occurred
	if len(validationErrors) > 0 {
		return fmt.Errorf("validation failed: %v", validationErrors)
	}

	return nextErr
}

// runAssertions executes all configured assertions on the context
func (m *assertionMiddleware) runAssertions(ctx *pipeline.ExecutionContext) []error {
	// Find the last assistant message to validate
	lastAssistantMsg := m.findLastAssistantMessage(ctx)
	if lastAssistantMsg == nil {
		return nil // No assistant message to validate
	}

	// Prepare validation context
	turnMessages := m.extractTurnMessages(ctx)

	// Execute all assertions
	results, errors := m.executeAssertions(lastAssistantMsg, turnMessages, ctx.Messages)

	// Attach results to message metadata
	m.attachResultsToMessage(lastAssistantMsg, results)

	return errors
}

// findLastAssistantMessage finds the most recent assistant message in the context
func (m *assertionMiddleware) findLastAssistantMessage(ctx *pipeline.ExecutionContext) *types.Message {
	for i := len(ctx.Messages) - 1; i >= 0; i-- {
		if ctx.Messages[i].Role == "assistant" {
			return &ctx.Messages[i]
		}
	}
	return nil
}

// extractTurnMessages extracts messages from the current turn (not from StateStore)
func (m *assertionMiddleware) extractTurnMessages(ctx *pipeline.ExecutionContext) []types.Message {
	var turnMessages []types.Message
	for _, msg := range ctx.Messages {
		if msg.Source != "statestore" {
			turnMessages = append(turnMessages, msg)
		}
	}
	return turnMessages
}

// executeAssertions runs all configured assertions and returns results and errors
func (m *assertionMiddleware) executeAssertions(
	lastAssistantMsg *types.Message,
	turnMessages []types.Message,
	allMessages []types.Message,
) (map[string]interface{}, []error) {
	results := make(map[string]interface{})
	var validationErrors []error

	for _, assertionConfig := range m.assertions {
		result, err := m.runSingleAssertion(assertionConfig, lastAssistantMsg, turnMessages, allMessages)
		if err != nil {
			logger.Warn("unknown assertion validator type %q", assertionConfig.Type)
			continue
		}

		// Convert to AssertionResult with message from config
		assertionResult := FromValidationResult(result, assertionConfig.Message)
		results[assertionConfig.Type] = assertionResult

		// Collect validation failures
		if !assertionResult.Passed {
			validationErrors = append(validationErrors,
				fmt.Errorf("assertion %q failed with details: %v", assertionConfig.Type, assertionResult.Details))
		}
	}

	return results, validationErrors
}

// runSingleAssertion executes a single assertion configuration
func (m *assertionMiddleware) runSingleAssertion(
	assertionConfig AssertionConfig,
	lastAssistantMsg *types.Message,
	turnMessages []types.Message,
	allMessages []types.Message,
) (runtimeValidators.ValidationResult, error) {
	// Create validator
	factory, ok := m.registry.Get(assertionConfig.Type)
	if !ok {
		return runtimeValidators.ValidationResult{}, fmt.Errorf("unknown validator type")
	}

	// Prepare parameters with context
	params := m.buildValidatorParams(assertionConfig.Params, turnMessages, allMessages)
	validator := factory(params)

	// Execute validation
	return validator.Validate(lastAssistantMsg.Content, params), nil
}

// buildValidatorParams builds parameters for validator execution
func (m *assertionMiddleware) buildValidatorParams(
	configParams map[string]interface{},
	turnMessages []types.Message,
	allMessages []types.Message,
) map[string]interface{} {
	params := make(map[string]interface{})

	// Copy configuration parameters
	for k, v := range configParams {
		params[k] = v
	}

	// Add context parameters
	params["_turn_messages"] = deepCloneMessages(turnMessages)
	params["_execution_context_messages"] = deepCloneMessages(allMessages)

	return params
}

// attachResultsToMessage attaches validation results to the message metadata
func (m *assertionMiddleware) attachResultsToMessage(msg *types.Message, results map[string]interface{}) {
	if msg.Meta == nil {
		msg.Meta = make(map[string]interface{})
	}
	msg.Meta["assertions"] = results
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
