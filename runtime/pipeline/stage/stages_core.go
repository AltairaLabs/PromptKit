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
			// Set variables for TemplateStage to use for substitution
			// VariableProviderStage (if present) can override/extend these
			elem.Metadata["variables"] = s.baseVariables

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

// validationState holds accumulated state during validation processing.
type validationState struct {
	elements          []StreamElement
	lastAssistantElem *StreamElement
	metadata          map[string]interface{}
}

// Process validates response elements and attaches results to metadata.
//
//nolint:lll // Channel signature cannot be shortened
func (s *ValidationStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	state := s.accumulateElements(input)

	if err := s.runValidation(ctx, state, output); err != nil {
		return err
	}

	return s.forwardAllElements(ctx, state.elements, output)
}

// accumulateElements collects elements and tracks the last assistant message.
func (s *ValidationStage) accumulateElements(input <-chan StreamElement) *validationState {
	state := &validationState{
		metadata: make(map[string]interface{}),
	}

	for elem := range input {
		state.elements = append(state.elements, elem)
		if elem.Message != nil && elem.Message.Role == "assistant" {
			state.lastAssistantElem = &state.elements[len(state.elements)-1]
		}
		for k, v := range elem.Metadata {
			state.metadata[k] = v
		}
	}

	return state
}

// runValidation performs validation on the assistant message if present.
func (s *ValidationStage) runValidation(
	ctx context.Context,
	state *validationState,
	output chan<- StreamElement,
) error {
	if state.lastAssistantElem == nil || state.lastAssistantElem.Message == nil {
		return nil
	}

	s.mergeMetadataToAssistant(state)

	logger.Debug("ValidationStage found assistant message to validate",
		"has_content", state.lastAssistantElem.Message.Content != "",
		"has_metadata", state.lastAssistantElem.Metadata != nil,
		"has_validator_configs", state.lastAssistantElem.Metadata["validator_configs"] != nil)

	if err := s.validateElement(ctx, state.lastAssistantElem); err != nil {
		if !s.suppressValidationExceptions {
			output <- NewErrorElement(err)
			return err
		}
	}
	return nil
}

// mergeMetadataToAssistant merges accumulated metadata to the assistant element.
func (s *ValidationStage) mergeMetadataToAssistant(state *validationState) {
	if state.lastAssistantElem.Metadata == nil {
		state.lastAssistantElem.Metadata = make(map[string]interface{})
	}
	for k, v := range state.metadata {
		state.lastAssistantElem.Metadata[k] = v
	}
}

// forwardAllElements forwards all elements to the output channel.
func (s *ValidationStage) forwardAllElements(
	ctx context.Context,
	elements []StreamElement,
	output chan<- StreamElement,
) error {
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
func (s *StateStoreLoadStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	historyMessages, err := s.loadHistoryMessages(ctx)
	if err != nil {
		return err
	}

	if err := s.emitHistoryMessages(ctx, historyMessages, output); err != nil {
		return err
	}

	return s.forwardInputWithMetadata(ctx, input, output)
}

// loadHistoryMessages loads messages from state store if configured.
func (s *StateStoreLoadStage) loadHistoryMessages(ctx context.Context) ([]types.Message, error) {
	if s.config == nil || s.config.Store == nil {
		return nil, nil
	}

	store, ok := s.config.Store.(statestore.Store)
	if !ok {
		return nil, fmt.Errorf("state store load: invalid store type")
	}

	state, err := store.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, fmt.Errorf("state store load: failed to load state: %w", err)
	}

	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}

	for i := range state.Messages {
		state.Messages[i].Source = "statestore"
	}
	return state.Messages, nil
}

// emitHistoryMessages sends history messages to output channel.
func (s *StateStoreLoadStage) emitHistoryMessages(
	ctx context.Context,
	messages []types.Message,
	output chan<- StreamElement,
) error {
	for i := range messages {
		elem := NewMessageElement(&messages[i])
		elem.Metadata["from_history"] = true
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// forwardInputWithMetadata forwards input elements with conversation metadata.
func (s *StateStoreLoadStage) forwardInputWithMetadata(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
		s.enrichElementMetadata(&elem)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// enrichElementMetadata adds conversation metadata to an element.
func (s *StateStoreLoadStage) enrichElementMetadata(elem *StreamElement) {
	if elem.Metadata == nil {
		elem.Metadata = make(map[string]interface{})
	}
	if s.config != nil {
		elem.Metadata["conversation_id"] = s.config.ConversationID
		if s.config.UserID != "" {
			elem.Metadata["user_id"] = s.config.UserID
		}
	}
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

// saveCollectedData holds data collected during state save processing.
type saveCollectedData struct {
	messages []types.Message
	metadata map[string]interface{}
}

// Process collects all messages and saves them to state store.
//
//nolint:lll // Channel signature cannot be shortened
func (s *StateStoreSaveStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.config == nil || s.config.Store == nil {
		return s.forwardAllElements(ctx, input, output)
	}

	collected, err := s.collectAndForward(ctx, input, output)
	if err != nil {
		return err
	}

	return s.saveToStateStore(ctx, collected)
}

// forwardAllElements forwards all input elements when no store is configured.
func (s *StateStoreSaveStage) forwardAllElements(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
		// Always forward error elements unconditionally - they carry critical
		// error information that must reach accumulateResult even when context
		// is canceled. This prevents errors from being silently dropped.
		if elem.Error != nil {
			output <- elem
			continue
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// collectAndForward collects messages/metadata while forwarding elements.
func (s *StateStoreSaveStage) collectAndForward(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) (*saveCollectedData, error) {
	collected := &saveCollectedData{}

	for elem := range input {
		if elem.Message != nil {
			collected.messages = append(collected.messages, *elem.Message)
		}
		s.mergeElementMetadata(&elem, collected)

		// Always forward error elements unconditionally - they carry critical
		// error information that must reach accumulateResult even when context
		// is canceled. This prevents errors from being silently dropped.
		if elem.Error != nil {
			output <- elem
			continue
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return collected, nil
}

// mergeElementMetadata merges element metadata into collected data.
func (s *StateStoreSaveStage) mergeElementMetadata(elem *StreamElement, collected *saveCollectedData) {
	if elem.Metadata == nil {
		return
	}
	if collected.metadata == nil {
		collected.metadata = make(map[string]interface{})
	}
	for k, v := range elem.Metadata {
		collected.metadata[k] = v
	}
}

// saveToStateStore saves collected data to the state store.
func (s *StateStoreSaveStage) saveToStateStore(ctx context.Context, collected *saveCollectedData) error {
	store, ok := s.config.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("state store save: invalid store type")
	}

	state, err := s.loadOrCreateState(ctx, store)
	if err != nil {
		return err
	}

	s.updateStateWithCollectedData(state, collected)
	return store.Save(ctx, state)
}

// loadOrCreateState loads existing state or creates new one.
func (s *StateStoreSaveStage) loadOrCreateState(
	ctx context.Context,
	store statestore.Store,
) (*statestore.ConversationState, error) {
	state, err := store.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, fmt.Errorf("state store save: failed to load state: %w", err)
	}

	if state != nil {
		return state, nil
	}

	return s.createNewState(), nil
}

// createNewState creates a new conversation state with config defaults.
func (s *StateStoreSaveStage) createNewState() *statestore.ConversationState {
	state := &statestore.ConversationState{
		ID:       s.config.ConversationID,
		UserID:   s.config.UserID,
		Messages: make([]types.Message, 0),
		Metadata: make(map[string]interface{}),
	}
	for k, v := range s.config.Metadata {
		state.Metadata[k] = v
	}
	return state
}

// updateStateWithCollectedData updates state with collected messages and metadata.
func (s *StateStoreSaveStage) updateStateWithCollectedData(
	state *statestore.ConversationState,
	collected *saveCollectedData,
) {
	state.Messages = make([]types.Message, len(collected.messages))
	copy(state.Messages, collected.messages)

	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}
	for k, v := range collected.metadata {
		state.Metadata[k] = v
	}
}
