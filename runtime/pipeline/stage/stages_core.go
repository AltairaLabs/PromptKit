package stage

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// PromptAssemblyStage loads and assembles prompts from the prompt registry.
// It enriches elements with system prompt, allowed tools, and variables.
type PromptAssemblyStage struct {
	BaseStage
	promptRegistry *prompt.Registry
	taskType       string
	baseVariables  map[string]string
}

// NewPromptAssemblyStage creates a new prompt assembly stage.
func NewPromptAssemblyStage(
	promptRegistry *prompt.Registry,
	taskType string,
	baseVariables map[string]string,
) *PromptAssemblyStage {
	return &PromptAssemblyStage{
		BaseStage:      NewBaseStage("prompt_assembly", StageTypeTransform),
		promptRegistry: promptRegistry,
		taskType:       taskType,
		baseVariables:  baseVariables,
	}
}

// Process loads and assembles the prompt, enriching elements with prompt data.
//
//nolint:lll // Channel signature cannot be shortened
func (s *PromptAssemblyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Load and assemble prompt
	assembled := s.assemblePrompt()

	// Forward all elements with enriched metadata
	for elem := range input {
		// Enrich element with prompt metadata
		if assembled != nil {
			if elem.Metadata == nil {
				elem.Metadata = make(map[string]interface{})
			}
			elem.Metadata["system_prompt"] = assembled.SystemPrompt
			elem.Metadata["allowed_tools"] = assembled.AllowedTools
			elem.Metadata["base_variables"] = s.baseVariables

			// Store validator configs for dynamic validator stage
			if len(assembled.Validators) > 0 {
				validatorConfigs := s.extractValidatorConfigs(assembled.Validators)
				if len(validatorConfigs) > 0 {
					elem.Metadata["validator_configs"] = validatorConfigs
				}
			}
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (s *PromptAssemblyStage) assemblePrompt() *prompt.AssembledPrompt {
	if s.promptRegistry == nil {
		logger.Warn("⚠️  Using default system prompt - no prompt registry configured", "task_type", s.taskType)
		return &prompt.AssembledPrompt{
			SystemPrompt: "You are a helpful AI assistant.",
			AllowedTools: nil,
			Validators:   nil,
		}
	}

	assembled := s.promptRegistry.LoadWithVars(s.taskType, s.baseVariables, "")
	if assembled == nil {
		logger.Warn("⚠️  Using default system prompt - no prompt found for task type", "task_type", s.taskType)
		return &prompt.AssembledPrompt{
			SystemPrompt: "You are a helpful AI assistant.",
			AllowedTools: nil,
			Validators:   nil,
		}
	}

	logger.Debug("Assembled prompt",
		"task_type", s.taskType,
		"length", len(assembled.SystemPrompt),
		"tools", len(assembled.AllowedTools),
		"base_vars", len(s.baseVariables))

	return assembled
}

func (s *PromptAssemblyStage) extractValidatorConfigs(promptValidators []prompt.ValidatorConfig) []validators.ValidatorConfig {
	validatorConfigs := make([]validators.ValidatorConfig, 0, len(promptValidators))
	for _, v := range promptValidators {
		// Skip disabled validators
		if v.Enabled != nil && !*v.Enabled {
			continue
		}
		// Extract the embedded validators.ValidatorConfig
		validatorConfigs = append(validatorConfigs, v.ValidatorConfig)
	}
	return validatorConfigs
}

// ValidationStage validates responses using configured validators.
type ValidationStage struct {
	BaseStage
	registry                     *validators.Registry
	suppressValidationExceptions bool
}

// NewValidationStage creates a new validation stage.
func NewValidationStage(registry *validators.Registry, suppressExceptions bool) *ValidationStage {
	return &ValidationStage{
		BaseStage:                    NewBaseStage("validation", StageTypeTransform),
		registry:                     registry,
		suppressValidationExceptions: suppressExceptions,
	}
}

// Process validates response elements and attaches results to metadata.
//
//nolint:lll // Channel signature cannot be shortened
func (s *ValidationStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Accumulate messages and metadata
	var elements []StreamElement
	var lastAssistantElem *StreamElement
	metadata := make(map[string]interface{})

	for elem := range input {
		elements = append(elements, elem)

		// Track last assistant message
		if elem.Message != nil && elem.Message.Role == "assistant" {
			lastAssistantElem = &elements[len(elements)-1]
		}

		// Accumulate metadata from all elements
		for k, v := range elem.Metadata {
			metadata[k] = v
		}
	}

	// Validate if we have an assistant message and validators configured
	if lastAssistantElem != nil && lastAssistantElem.Message != nil {
		// Merge accumulated metadata onto assistant element
		if lastAssistantElem.Metadata == nil {
			lastAssistantElem.Metadata = make(map[string]interface{})
		}
		for k, v := range metadata {
			lastAssistantElem.Metadata[k] = v
		}

		logger.Debug("ValidationStage found assistant message to validate",
			"has_content", lastAssistantElem.Message.Content != "",
			"has_metadata", lastAssistantElem.Metadata != nil,
			"has_validator_configs", lastAssistantElem.Metadata != nil && lastAssistantElem.Metadata["validator_configs"] != nil)

		if err := s.validateElement(ctx, lastAssistantElem); err != nil {
			// Emit error element if validation failed and exceptions not suppressed
			if !s.suppressValidationExceptions {
				output <- NewErrorElement(err)
				return err
			}
		}
	}

	// Forward all elements
	for _, elem := range elements {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (s *ValidationStage) validateElement(ctx context.Context, elem *StreamElement) error {
	if elem.Metadata == nil {
		return nil
	}

	// Get validator configs from metadata
	validatorConfigs, ok := elem.Metadata["validator_configs"].([]validators.ValidatorConfig)
	if !ok || len(validatorConfigs) == 0 {
		return nil
	}

	// Build validators from configs
	validatorList, validatorParams := s.buildValidators(validatorConfigs)
	if len(validatorList) == 0 {
		return nil
	}

	// Validate the message content
	contentToValidate := elem.Message.Content
	if contentToValidate == "" {
		logger.Debug("Skipping validation for empty content")
		return nil
	}

	logger.Debug("Validating response", "validators", len(validatorList), "content_length", len(contentToValidate))

	// Run validations
	results := make([]types.ValidationResult, 0, len(validatorList))
	for i, validator := range validatorList {
		result := validator.Validate(contentToValidate, validatorParams[i])

		// Convert to types.ValidationResult with ValidatorType
		var details map[string]interface{}
		if result.Details != nil {
			if detailsMap, ok := result.Details.(map[string]interface{}); ok {
				details = detailsMap
			}
		}

		validationResult := types.ValidationResult{
			Passed:        result.Passed,
			Details:       details,
			ValidatorType: validatorConfigs[i].Type,
		}
		results = append(results, validationResult)

		logger.Debug("Validation result",
			"validator_type", validatorConfigs[i].Type,
			"passed", result.Passed)
	}

	// Attach results to metadata
	elem.Metadata["validation_results"] = results

	// Also attach to message for arena assertions to access
	if elem.Message != nil {
		elem.Message.Validations = results
	}

	// Check for failures
	if !s.suppressValidationExceptions && hasValidationFailures(results) {
		return fmt.Errorf("validation failed: %d validators found issues", countFailures(results))
	}

	return nil
}

func (s *ValidationStage) buildValidators(configs []validators.ValidatorConfig) ([]validators.Validator, []map[string]interface{}) {
	validatorList := make([]validators.Validator, 0, len(configs))
	validatorParams := make([]map[string]interface{}, 0, len(configs))

	for _, config := range configs {
		factory, ok := s.registry.Get(config.Type)
		if !ok {
			logger.Warn("Validator not found in registry", "type", config.Type)
			continue
		}
		validator := factory(config.Params)
		validatorList = append(validatorList, validator)
		validatorParams = append(validatorParams, config.Params)
	}

	return validatorList, validatorParams
}

func hasValidationFailures(results []types.ValidationResult) bool {
	for _, result := range results {
		if !result.Passed {
			return true
		}
	}
	return false
}

func countFailures(results []types.ValidationResult) int {
	count := 0
	for _, result := range results {
		if !result.Passed {
			count++
		}
	}
	return count
}

// StateStoreLoadStage loads conversation history from state store.
type StateStoreLoadStage struct {
	BaseStage
	config *pipeline.StateStoreConfig
}

// NewStateStoreLoadStage creates a new state store load stage.
func NewStateStoreLoadStage(config *pipeline.StateStoreConfig) *StateStoreLoadStage {
	return &StateStoreLoadStage{
		BaseStage: NewBaseStage("statestore_load", StageTypeTransform),
		config:    config,
	}
}

// Process loads conversation history and emits it before current input.
//
//nolint:lll // Channel signature cannot be shortened
func (s *StateStoreLoadStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Load history if configured
	var historyMessages []types.Message
	if s.config != nil && s.config.Store != nil {
		store, ok := s.config.Store.(statestore.Store)
		if !ok {
			return fmt.Errorf("state store load: invalid store type")
		}

		state, err := store.Load(ctx, s.config.ConversationID)
		if err != nil && !errors.Is(err, statestore.ErrNotFound) {
			return fmt.Errorf("state store load: failed to load state: %w", err)
		}

		if state != nil && len(state.Messages) > 0 {
			historyMessages = state.Messages
			// Mark messages as loaded from statestore
			for i := range historyMessages {
				historyMessages[i].Source = "statestore"
			}
		}
	}

	// Emit history messages first
	for i := range historyMessages {
		elem := NewMessageElement(&historyMessages[i])
		elem.Metadata["from_history"] = true
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Then emit current input messages
	for elem := range input {
		// Enrich with conversation metadata
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		if s.config != nil {
			elem.Metadata["conversation_id"] = s.config.ConversationID
			if s.config.UserID != "" {
				elem.Metadata["user_id"] = s.config.UserID
			}
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// StateStoreSaveStage saves conversation state to state store.
type StateStoreSaveStage struct {
	BaseStage
	config *pipeline.StateStoreConfig
}

// NewStateStoreSaveStage creates a new state store save stage.
func NewStateStoreSaveStage(config *pipeline.StateStoreConfig) *StateStoreSaveStage {
	return &StateStoreSaveStage{
		BaseStage: NewBaseStage("statestore_save", StageTypeSink),
		config:    config,
	}
}

// Process collects all messages and saves them to state store.
//
//nolint:lll // Channel signature cannot be shortened
func (s *StateStoreSaveStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Skip if no config
	if s.config == nil || s.config.Store == nil {
		// Just forward elements
		for elem := range input {
			select {
			case output <- elem:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Collect all messages
	var messages []types.Message
	var metadata map[string]interface{}

	for elem := range input {
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
		if elem.Metadata != nil {
			if metadata == nil {
				metadata = make(map[string]interface{})
			}
			for k, v := range elem.Metadata {
				metadata[k] = v
			}
		}

		// Forward element
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Save to state store
	store, ok := s.config.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("state store save: invalid store type")
	}

	// Load current state or create new
	state, err := store.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("state store save: failed to load state: %w", err)
	}

	if state == nil {
		state = &statestore.ConversationState{
			ID:       s.config.ConversationID,
			UserID:   s.config.UserID,
			Messages: make([]types.Message, 0),
			Metadata: make(map[string]interface{}),
		}

		// Initialize with config metadata
		for k, v := range s.config.Metadata {
			state.Metadata[k] = v
		}
	}

	// Update with new messages
	state.Messages = make([]types.Message, len(messages))
	copy(state.Messages, messages)

	// Ensure metadata is initialized
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}

	// Merge execution metadata
	for k, v := range metadata {
		state.Metadata[k] = v
	}

	// Save to store
	return store.Save(ctx, state)
}
