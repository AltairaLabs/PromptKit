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
)

// PromptAssemblyStage loads and assembles prompts from the prompt registry.
// It populates TurnState (Template, AllowedTools, Validators) on its first
// iteration and continues stamping the deprecated per-element Metadata bag
// for backward compatibility with Arena's wholesale-copy save stage.
//
// See ARCHITECTURE.md §4.
type PromptAssemblyStage struct {
	BaseStage
	promptRegistry *prompt.Registry
	taskType       string
	baseVariables  map[string]string

	// turnState is the per-Turn shared state populated on first element.
	// May be nil for legacy callers that haven't migrated to TurnState
	// wiring; in that case the stage falls back to the metadata-only path.
	turnState *TurnState
}

// NewPromptAssemblyStage creates a new prompt assembly stage. Pipelines
// that have migrated to TurnState should use NewPromptAssemblyStageWithTurnState.
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

// NewPromptAssemblyStageWithTurnState creates a stage that populates the
// shared *TurnState in addition to the deprecated per-element metadata.
func NewPromptAssemblyStageWithTurnState(
	promptRegistry *prompt.Registry,
	taskType string,
	baseVariables map[string]string,
	turnState *TurnState,
) *PromptAssemblyStage {
	s := NewPromptAssemblyStage(promptRegistry, taskType, baseVariables)
	s.turnState = turnState
	return s
}

// Process loads the prompt template, populates TurnState (when configured),
// and continues stamping per-element metadata for back-compat. It does NOT
// render the template — that is TemplateStage's responsibility.
// It does NOT set metadata["variables"] — that is VariableProviderStage's responsibility.
//
//nolint:lll // Channel signature cannot be shortened
func (s *PromptAssemblyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Load raw template (no rendering)
	tmpl := s.loadTemplate()

	// Populate TurnState once, before forwarding the first element. Channel
	// hand-off to downstream stages establishes happens-before so readers
	// (TemplateStage, ProviderStage, etc.) observe these writes.
	if s.turnState != nil && tmpl != nil {
		s.turnState.Template = tmpl
		s.turnState.AllowedTools = tmpl.AllowedTools
		if len(tmpl.Validators) > 0 {
			s.turnState.Validators = s.extractValidatorConfigs(tmpl.Validators)
		}
	}

	// Forward all elements with enriched metadata
	for elem := range input {
		if tmpl != nil {
			if elem.Metadata == nil {
				elem.Metadata = make(map[string]interface{})
			}
			elem.Metadata["system_template"] = tmpl.RawTemplate
			elem.Metadata["allowed_tools"] = tmpl.AllowedTools
			elem.Metadata["base_variables"] = s.baseVariables
			elem.Metadata["template_task_type"] = tmpl.TaskType
			elem.Metadata["template_default_vars"] = tmpl.DefaultVars
			elem.Metadata["template_required_vars"] = tmpl.RequiredVars
			elem.Metadata["template_fragment_vars"] = tmpl.FragmentVars
			elem.Metadata["template_model_override"] = tmpl.ModelOverride

			// Store validator configs for dynamic validator stage
			if len(tmpl.Validators) > 0 {
				validatorConfigs := s.extractValidatorConfigs(tmpl.Validators)
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

func (s *PromptAssemblyStage) loadTemplate() *prompt.Template {
	defaultTemplate := &prompt.Template{
		RawTemplate:  "You are a helpful AI assistant.",
		AllowedTools: nil,
		Validators:   nil,
	}

	if s.promptRegistry == nil {
		logger.Warn("Using default system prompt, no prompt registry configured", "task_type", s.taskType)
		return defaultTemplate
	}

	tmpl, err := s.promptRegistry.LoadTemplate(s.taskType, s.baseVariables, "")
	if err != nil {
		logger.Warn("Using default system prompt, no prompt found for task type", "task_type", s.taskType)
		return defaultTemplate
	}

	logger.Debug("Loaded prompt template",
		"task_type", s.taskType,
		"tools", len(tmpl.AllowedTools),
		"base_vars", len(s.baseVariables))

	return tmpl
}

func (s *PromptAssemblyStage) extractValidatorConfigs(
	promptValidators []prompt.ValidatorConfig,
) []prompt.ValidatorConfig {
	configs := make([]prompt.ValidatorConfig, 0, len(promptValidators))
	for _, v := range promptValidators {
		// Skip disabled validators
		if v.Enabled != nil && !*v.Enabled {
			continue
		}
		configs = append(configs, v)
	}
	return configs
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
