package middleware

import (
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// dynamicValidatorMiddleware creates middleware that dynamically instantiates
// validators from configurations stored in ExecutionContext.
type dynamicValidatorMiddleware struct {
	registry                     *validators.Registry
	SuppressValidationExceptions bool // When true, validation failures don't throw errors (test mode)
}

// DynamicValidatorMiddleware creates middleware that dynamically instantiates
// validators from configurations stored in ExecutionContext.
// It uses the validator registry to create validators on-demand and passes
// their params from the config.
func DynamicValidatorMiddleware(registry *validators.Registry) pipeline.Middleware {
	return &dynamicValidatorMiddleware{
		registry:                     registry,
		SuppressValidationExceptions: false, // Default: production mode (throw on failures)
	}
}

// DynamicValidatorMiddlewareWithSuppression creates middleware with validation suppression enabled.
// This is primarily used by test frameworks (like Arena) to allow validation failures to be
// recorded without halting execution, enabling assertions on guardrail behavior.
func DynamicValidatorMiddlewareWithSuppression(
	registry *validators.Registry,
	suppressExceptions bool,
) pipeline.Middleware {
	return &dynamicValidatorMiddleware{
		registry:                     registry,
		SuppressValidationExceptions: suppressExceptions,
	}
}

func (m *dynamicValidatorMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Get validator configs from metadata (populated by PromptAssemblyMiddleware)
	validatorList, validatorParams, _, shouldReturn := m.getValidators(execCtx)
	if shouldReturn {
		// No validators configured, just continue to next middleware
		return next()
	}

	validatorConfigs, ok := execCtx.Metadata["validator_configs"].([]validators.ValidatorConfig)
	if !ok || len(validatorConfigs) == 0 {
		// No validators configured, just continue
		return next()
	}

	logger.Debug("Validators ready for processing", "count", len(validatorList))

	// Validate the response and attach results to the message
	// The provider has already run and created the assistant message before we got here

	err := m.validateAndAttach(execCtx, validatorList, validatorParams)

	// Continue to next middleware (StateStore) which will persist the validation results
	nextErr := next()

	// Return validation error if present, otherwise return next error
	if err != nil {
		logger.Debug("Validation threw an error", "err", err)
		return err
	}

	return nextErr
}

// validateAndAttach validates the response and attaches results to the last assistant message
func (m *dynamicValidatorMiddleware) validateAndAttach(
	execCtx *pipeline.ExecutionContext,
	validatorList []validators.Validator,
	validatorParams []map[string]interface{},
) error {
	// Find the last assistant message
	lastAssistantIdx := m.findLastAssistantMessage(execCtx)
	if lastAssistantIdx == -1 {
		return nil
	}

	contentToValidate := execCtx.Messages[lastAssistantIdx].Content
	if contentToValidate == "" {
		logger.Debug("Skipping validation for empty content")
		return nil
	}

	logger.Debug("Validating response", "validators", len(validatorList), "content_length", len(contentToValidate))

	// Collect streaming failures and run non-streaming validators
	validationResults, failedValidations := m.runValidations(
		execCtx, validatorList, validatorParams, contentToValidate,
	)

	// Always attach all validation results to message (regardless of whether we throw)
	execCtx.Messages[lastAssistantIdx].Validations = validationResults

	logger.Debug("Validation complete", "total_validators", len(validationResults), "failed", len(failedValidations))

	// Handle validation failures based on suppression flag
	return m.handleValidationFailures(failedValidations)
}

// findLastAssistantMessage finds the index of the last assistant message
func (m *dynamicValidatorMiddleware) findLastAssistantMessage(
	execCtx *pipeline.ExecutionContext,
) int {
	for i := len(execCtx.Messages) - 1; i >= 0; i-- {
		if execCtx.Messages[i].Role == "assistant" {
			return i
		}
	}
	return -1
}

// runValidations executes all validators and collects results
func (m *dynamicValidatorMiddleware) runValidations(
	execCtx *pipeline.ExecutionContext,
	validatorList []validators.Validator,
	validatorParams []map[string]interface{},
	contentToValidate string,
) ([]types.ValidationResult, []types.ValidationResult) {
	// Get streaming validation results from metadata (if any)
	streamingResults, _ := execCtx.Metadata["_streaming_validation_results"].([]types.ValidationResult)
	validationFailed, _ := execCtx.Metadata["_streaming_validation_failed"].(bool)

	// Start with streaming results
	validationResults := make([]types.ValidationResult, len(streamingResults))
	copy(validationResults, streamingResults)

	// Track all failed validations for aggregation
	var failedValidations []types.ValidationResult

	// If streaming validation already failed, collect those failures
	if validationFailed {
		failedValidations = m.collectStreamingFailures(streamingResults)
	}

	// Run non-streaming validators on the complete content
	for i, validator := range validatorList {
		if m.shouldSkipValidator(execCtx, validator) {
			continue
		}

		result := m.executeValidator(validator, contentToValidate, validatorParams[i])
		validationResults = append(validationResults, result)

		if !result.Passed {
			logger.Warn("Validation failed", "validator", result.ValidatorType, "details", result.Details)
			failedValidations = append(failedValidations, result)
		}
	}

	return validationResults, failedValidations
}

// collectStreamingFailures extracts failed validations from streaming results
func (m *dynamicValidatorMiddleware) collectStreamingFailures(
	streamingResults []types.ValidationResult,
) []types.ValidationResult {
	var failures []types.ValidationResult
	for _, result := range streamingResults {
		if !result.Passed {
			failures = append(failures, result)
		}
	}
	return failures
}

// shouldSkipValidator determines if a validator should be skipped
func (m *dynamicValidatorMiddleware) shouldSkipValidator(
	execCtx *pipeline.ExecutionContext,
	validator validators.Validator,
) bool {
	// Skip streaming validators ONLY if we're in streaming mode and they already ran
	if !execCtx.StreamMode {
		return false
	}

	sv, ok := validator.(validators.StreamingValidator)
	return ok && sv.SupportsStreaming()
}

// executeValidator runs a single validator and returns the result
func (m *dynamicValidatorMiddleware) executeValidator(
	validator validators.Validator,
	content string,
	params map[string]interface{},
) types.ValidationResult {
	result := validator.Validate(content, params)

	// Convert details to map[string]interface{}
	var details map[string]interface{}
	if d, ok := result.Details.(map[string]interface{}); ok {
		details = d
	} else if result.Details != nil {
		// Wrap non-map details
		details = map[string]interface{}{"value": result.Details}
	}

	return types.ValidationResult{
		ValidatorType: fmt.Sprintf("%T", validator),
		Passed:        result.Passed,
		Details:       details,
		Timestamp:     time.Now(),
	}
}

// handleValidationFailures decides whether to throw error based on suppression flag
func (m *dynamicValidatorMiddleware) handleValidationFailures(
	failedValidations []types.ValidationResult,
) error {
	if len(failedValidations) == 0 {
		return nil
	}

	if m.SuppressValidationExceptions {
		// Test mode: log but don't throw
		logger.Info("Validation failures detected but SuppressValidationExceptions=true, continuing execution",
			"failed_count", len(failedValidations))
		return nil
	}

	// Production mode: throw error with all failures
	return &pipeline.ValidationError{
		Type:     "validation_failed",
		Details:  fmt.Sprintf("validation failed: %d validator(s) failed", len(failedValidations)),
		Failures: failedValidations,
	}
}

func (m *dynamicValidatorMiddleware) StreamChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	// Get validator configs from metadata (set by PromptAssemblyMiddleware)
	validatorList, validatorParams, err, shouldReturn := m.getValidators(execCtx)
	if shouldReturn {
		return err
	}

	// Get streaming state
	contentBuffer, validationResults := m.getStreamingState(execCtx)

	// Update accumulated content
	contentBuffer = m.updateContentBuffer(execCtx, chunk, contentBuffer)

	// Validate chunk with streaming validators
	validationResults, err = m.validateStreamingChunk(
		execCtx,
		chunk,
		validatorList,
		validatorParams,
		validationResults,
	)
	if err != nil {
		return err
	}

	// Record successful validations on final chunk
	if chunk.FinishReason != nil {
		m.recordSuccessfulValidations(execCtx, validatorList, contentBuffer, validationResults)
	}

	return nil
}

// getStreamingState retrieves streaming validation state from metadata
func (m *dynamicValidatorMiddleware) getStreamingState(
	execCtx *pipeline.ExecutionContext,
) (string, []types.ValidationResult) {
	contentBuffer, _ := execCtx.Metadata["_streaming_content_buffer"].(string)
	validationResults, _ := execCtx.Metadata["_streaming_validation_results"].([]types.ValidationResult)
	return contentBuffer, validationResults
}

// updateContentBuffer updates the accumulated content buffer
func (m *dynamicValidatorMiddleware) updateContentBuffer(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
	contentBuffer string,
) string {
	if chunk.Content != "" {
		contentBuffer = chunk.Content
		execCtx.Metadata["_streaming_content_buffer"] = contentBuffer
	}
	return contentBuffer
}

// validateStreamingChunk validates the chunk with all streaming validators
func (m *dynamicValidatorMiddleware) validateStreamingChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
	validatorList []validators.Validator,
	validatorParams []map[string]interface{},
	validationResults []types.ValidationResult,
) ([]types.ValidationResult, error) {
	for i, validator := range validatorList {
		// Only process streaming validators
		sv, ok := validator.(validators.StreamingValidator)
		if !ok || !sv.SupportsStreaming() {
			continue
		}

		params := validatorParams[i]

		// Real-time streaming validation
		if err := sv.ValidateChunk(*chunk, params); err != nil {
			validationResults = m.recordStreamingFailure(execCtx, sv, err, chunk, validationResults)

			// Only throw error if suppression is disabled
			if !m.SuppressValidationExceptions {
				return validationResults, fmt.Errorf("streaming validation failed: %w", err)
			}

			// With suppression, record failure but don't throw - stream is still interrupted
			return validationResults, nil
		}
	}

	return validationResults, nil
}

// recordStreamingFailure records a streaming validation failure and interrupts the stream
func (m *dynamicValidatorMiddleware) recordStreamingFailure(
	execCtx *pipeline.ExecutionContext,
	sv validators.StreamingValidator,
	err error,
	chunk *providers.StreamChunk,
	validationResults []types.ValidationResult,
) []types.ValidationResult {
	// Interrupt the stream
	reason := fmt.Sprintf("Streaming validation failed: %v", err)
	execCtx.InterruptStream(reason)

	// Record failed validation
	validationResult := types.ValidationResult{
		ValidatorType: fmt.Sprintf("%T", sv),
		Passed:        false,
		Details:       map[string]interface{}{"error": err.Error(), "content_length": len(chunk.Content)},
		Timestamp:     time.Now(),
	}
	validationResults = append(validationResults, validationResult)
	execCtx.Metadata["_streaming_validation_results"] = validationResults
	execCtx.Metadata["_streaming_validation_failed"] = true

	logger.Warn("Streaming validation failed, interrupting stream",
		"validator", fmt.Sprintf("%T", sv),
		"error", err.Error())

	return validationResults
}

// recordSuccessfulValidations records successful streaming validations on final chunk
func (m *dynamicValidatorMiddleware) recordSuccessfulValidations(
	execCtx *pipeline.ExecutionContext,
	validatorList []validators.Validator,
	contentBuffer string,
	validationResults []types.ValidationResult,
) {
	for _, validator := range validatorList {
		sv, ok := validator.(validators.StreamingValidator)
		if !ok || !sv.SupportsStreaming() {
			continue
		}

		validationResult := types.ValidationResult{
			ValidatorType: fmt.Sprintf("%T", sv),
			Passed:        true,
			Details:       map[string]interface{}{"content_length": len(contentBuffer)},
			Timestamp:     time.Now(),
		}
		validationResults = append(validationResults, validationResult)
	}
	execCtx.Metadata["_streaming_validation_results"] = validationResults

	logger.Debug("All streaming validations passed", "validators", len(validationResults))
}

func (m *dynamicValidatorMiddleware) getValidators(
	execCtx *pipeline.ExecutionContext,
) ([]validators.Validator, []map[string]interface{}, error, bool) {
	validatorConfigs, ok := execCtx.Metadata["validator_configs"].([]validators.ValidatorConfig)
	if !ok || len(validatorConfigs) == 0 {
		logger.Debug("No validator configs found in metadata, skipping validation")
		// No validators configured
		return nil, nil, nil, true
	}

	// Check if we've already built the validator list (to avoid rebuilding on every chunk)
	var validatorList []validators.Validator
	var validatorParams []map[string]interface{}

	if cached, ok := execCtx.Metadata["_validators"].([]validators.Validator); ok {
		// Already built - reuse
		validatorList = cached
		validatorParams, _ = execCtx.Metadata["_validator_params"].([]map[string]interface{})
	} else {
		// First chunk - build validator list from configs
		for _, config := range validatorConfigs {
			factory, exists := m.registry.Get(config.Type)
			if !exists {
				logger.Warn("Unknown validator type, skipping", "type", config.Type)
				continue
			}

			validator := factory(config.Params)
			validatorList = append(validatorList, validator)
			validatorParams = append(validatorParams, config.Params)
		}

		// Cache for subsequent chunks
		execCtx.Metadata["_validators"] = validatorList
		execCtx.Metadata["_validator_params"] = validatorParams
		execCtx.Metadata["_streaming_validation_results"] = []types.ValidationResult{}
	}

	if len(validatorList) == 0 {
		return nil, nil, nil, true
	}
	return validatorList, validatorParams, nil, false
}
