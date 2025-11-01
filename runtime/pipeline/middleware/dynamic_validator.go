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
	registry *validators.Registry
}

// DynamicValidatorMiddleware creates middleware that dynamically instantiates
// validators from configurations stored in ExecutionContext.
// It uses the validator registry to create validators on-demand and passes
// their params from the config.
func DynamicValidatorMiddleware(registry *validators.Registry) pipeline.Middleware {
	return &dynamicValidatorMiddleware{registry: registry}
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
	next_err := next()

	// Return validation error if present, otherwise return next error
	if err != nil {
		logger.Debug("Validation threw an error", "err", err)
		return err
	}

	return next_err
}

// validateAndAttach validates the response and attaches results to the last assistant message
func (m *dynamicValidatorMiddleware) validateAndAttach(execCtx *pipeline.ExecutionContext, validatorList []validators.Validator, validatorParams []map[string]interface{}) error {
	// Find the last assistant message
	var lastAssistantIdx = -1
	for i := len(execCtx.Messages) - 1; i >= 0; i-- {
		if execCtx.Messages[i].Role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}

	if lastAssistantIdx == -1 {
		// No assistant message to validate
		return nil
	}

	// Get content to validate from the message
	contentToValidate := execCtx.Messages[lastAssistantIdx].Content

	// Skip validation for empty content
	if contentToValidate == "" {
		logger.Debug("Skipping validation for empty content")
		return nil
	}

	logger.Debug("Validating response", "validators", len(validatorList), "content_length", len(contentToValidate))

	// Get streaming validation results from metadata (if any)
	streamingResults, _ := execCtx.Metadata["_streaming_validation_results"].([]types.ValidationResult)
	validationFailed, _ := execCtx.Metadata["_streaming_validation_failed"].(bool)

	// Start with streaming results
	validationResults := make([]types.ValidationResult, len(streamingResults))
	copy(validationResults, streamingResults)

	// If streaming validation already failed, we still need to attach results
	// but we should return the error
	var validationError error
	if validationFailed {
		// Find the failed validation in streaming results to return as error
		for _, result := range streamingResults {
			if !result.Passed {
				validationError = &pipeline.ValidationError{
					Type:    result.ValidatorType,
					Details: fmt.Sprintf("%v", result.Details),
				}
				break
			}
		}
	}

	// Run non-streaming validators on the complete content
	// (Streaming validators already ran during StreamChunk in streaming mode)
	for i, validator := range validatorList {
		// Skip streaming validators ONLY if we're in streaming mode and they already ran
		if execCtx.StreamMode {
			if sv, ok := validator.(validators.StreamingValidator); ok && sv.SupportsStreaming() {
				// This validator already ran during streaming, skip it
				continue
			}
		}

		params := validatorParams[i]
		result := validator.Validate(contentToValidate, params)

		// Convert details to map[string]interface{}
		var details map[string]interface{}
		if d, ok := result.Details.(map[string]interface{}); ok {
			details = d
		} else if result.Details != nil {
			// Wrap non-map details
			details = map[string]interface{}{"value": result.Details}
		}

		// Record validation result
		validationResult := types.ValidationResult{
			ValidatorType: fmt.Sprintf("%T", validator),
			Passed:        result.OK,
			Details:       details,
			Timestamp:     time.Now(),
		}
		validationResults = append(validationResults, validationResult)

		if !result.OK {
			// Non-streaming validation failed
			logger.Warn("Validation failed", "validator", fmt.Sprintf("%T", validator), "details", details)

			// Attach validation results to message before returning error
			execCtx.Messages[lastAssistantIdx].Validations = validationResults

			return &pipeline.ValidationError{
				Type:    fmt.Sprintf("%T", validator),
				Details: fmt.Sprintf("%v", result.Details),
			}
		}
	}

	// Attach all validation results to message
	execCtx.Messages[lastAssistantIdx].Validations = validationResults

	logger.Debug("Validation complete", "total_validators", len(validationResults), "passed", validationError == nil)

	// Return streaming validation error if it occurred
	return validationError
}

func (m *dynamicValidatorMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Get validator configs from metadata (set by PromptAssemblyMiddleware)
	validatorList, validatorParams, err, shouldReturn := m.getValidators(execCtx)
	if shouldReturn {
		return err
	}

	// Get streaming state
	contentBuffer, _ := execCtx.Metadata["_streaming_content_buffer"].(string)
	validationResults, _ := execCtx.Metadata["_streaming_validation_results"].([]types.ValidationResult)

	// Update accumulated content (chunks are cumulative)
	if chunk.Content != "" {
		contentBuffer = chunk.Content
		execCtx.Metadata["_streaming_content_buffer"] = contentBuffer
	}

	// Validate chunk with streaming validators only
	for i, validator := range validatorList {
		// Only process streaming validators
		sv, ok := validator.(validators.StreamingValidator)
		if !ok || !sv.SupportsStreaming() {
			continue
		}

		params := validatorParams[i]

		// Real-time streaming validation
		if err := sv.ValidateChunk(*chunk, params); err != nil {
			// Streaming validation failed - interrupt the stream
			reason := fmt.Sprintf("Streaming validation failed: %v", err)
			execCtx.InterruptStream(reason)

			// Record failed validation in metadata
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

			return fmt.Errorf("streaming validation failed: %w", err)
		}
	}

	// If we get here, all streaming validators passed for this chunk
	// Record successful validations on final chunk
	if chunk.FinishReason != nil {
		// Final chunk - record successful streaming validations
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

	return nil
}

func (m *dynamicValidatorMiddleware) getValidators(execCtx *pipeline.ExecutionContext) ([]validators.Validator, []map[string]interface{}, error, bool) {
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
