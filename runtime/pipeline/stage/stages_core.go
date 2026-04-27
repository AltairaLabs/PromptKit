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

	// Forward all elements; per-Turn data lives on TurnState above.
	for elem := range input {
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
	config    *pipeline.StateStoreConfig
	turnState *TurnState
}

// NewStateStoreLoadStage creates a new state store load stage.
func NewStateStoreLoadStage(config *pipeline.StateStoreConfig) *StateStoreLoadStage {
	return NewStateStoreLoadStageWithTurnState(config, nil)
}

// NewStateStoreLoadStageWithTurnState creates a state store load stage
// that publishes ConversationID/UserID onto the supplied TurnState.
func NewStateStoreLoadStageWithTurnState(config *pipeline.StateStoreConfig, turnState *TurnState) *StateStoreLoadStage {
	return &StateStoreLoadStage{
		BaseStage: NewBaseStage("statestore_load", StageTypeTransform),
		config:    config,
		turnState: turnState,
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

	if s.turnState != nil && s.config != nil {
		s.turnState.ConversationID = s.config.ConversationID
		s.turnState.UserID = s.config.UserID
	}

	historyMessages, err := s.loadHistoryMessages(ctx)
	if err != nil {
		return err
	}

	if err := s.emitHistoryMessages(ctx, historyMessages, output); err != nil {
		return err
	}

	return s.forwardInput(ctx, input, output)
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
		elem.Meta.FromHistory = true
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// forwardInput forwards input elements unchanged. Conversation/user IDs
// are published to TurnState in Process() before the loop runs, so
// per-element enrichment is no longer required.
func (s *StateStoreLoadStage) forwardInput(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
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
	config    *pipeline.StateStoreConfig
	turnState *TurnState
}

// NewStateStoreSaveStage creates a new state store save stage.
func NewStateStoreSaveStage(config *pipeline.StateStoreConfig) *StateStoreSaveStage {
	return &StateStoreSaveStage{
		BaseStage: NewBaseStage("statestore_save", StageTypeSink),
		config:    config,
	}
}

// NewStateStoreSaveStageWithTurnState creates a save stage that also merges
// TurnState.ProviderRequestMetadata into the persisted state.Metadata. This
// is the path consumers use to persist per-Turn coordination data (e.g. arena
// turn counters) that previously rode on the deleted StreamElement.Metadata
// bag.
func NewStateStoreSaveStageWithTurnState(
	config *pipeline.StateStoreConfig,
	turnState *TurnState,
) *StateStoreSaveStage {
	return &StateStoreSaveStage{
		BaseStage: NewBaseStage("statestore_save", StageTypeSink),
		config:    config,
		turnState: turnState,
	}
}

// saveCollectedData holds data collected during state save processing.
type saveCollectedData struct {
	messages []types.Message
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

// updateStateWithCollectedData updates state with collected messages and any
// per-Turn provider-request metadata produced by upstream stages.
func (s *StateStoreSaveStage) updateStateWithCollectedData(
	state *statestore.ConversationState,
	collected *saveCollectedData,
) {
	state.Messages = make([]types.Message, len(collected.messages))
	copy(state.Messages, collected.messages)

	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}

	if s.turnState != nil {
		for k, v := range s.turnState.ProviderRequestMetadata {
			state.Metadata[k] = v
		}
	}
}
