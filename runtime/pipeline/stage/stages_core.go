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
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// PromptAssemblyStage loads and assembles prompts from the prompt registry.
// It populates TurnState (Template, AllowedTools, Validators) on its first
// iteration; downstream stages read from TurnState. See ARCHITECTURE.md §4.
type PromptAssemblyStage struct {
	BaseStage
	promptRegistry *prompt.Registry
	taskType       string
	baseVariables  map[string]string
	turnState      *TurnState
}

// NewPromptAssemblyStage creates a prompt assembly stage with no TurnState
// wired. Useful for tests that only need the loadTemplate side; production
// callers should use NewPromptAssemblyStageWithTurnState so downstream
// stages can read the loaded template.
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

// NewPromptAssemblyStageWithTurnState creates a stage that publishes the
// loaded template, allowed tools, and validator configs onto the supplied
// TurnState before forwarding the first element.
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

// Process loads the prompt template and populates TurnState. It does NOT
// render the template (that is TemplateStage's job) and does NOT set
// variables (that is VariableProviderStage's job). All input elements are
// forwarded unchanged.
//
//nolint:lll // Channel signature cannot be shortened
func (s *PromptAssemblyStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	// Load raw template (no rendering)
	tmpl := s.loadTemplate(ctx)

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

func (s *PromptAssemblyStage) loadTemplate(ctx context.Context) *prompt.Template {
	defaultTemplate := &prompt.Template{
		RawTemplate:  "You are a helpful AI assistant.",
		AllowedTools: nil,
		Validators:   nil,
	}

	if s.promptRegistry == nil {
		logger.Warn("Using default system prompt, no prompt registry configured", "task_type", s.taskType)
		return defaultTemplate
	}

	// Merge per-request variables (carried on the context, e.g. from an SDK
	// structured input) over the static base variables. Without this, a required
	// variable supplied only per-request would fail LoadTemplate's required-var
	// check and the pack prompt would be discarded for the default.
	loadVars := s.baseVariables
	if reqVars := variables.RequestVars(ctx); len(reqVars) > 0 {
		loadVars = make(map[string]string, len(s.baseVariables)+len(reqVars))
		for k, v := range s.baseVariables {
			loadVars[k] = v
		}
		for k, v := range reqVars {
			loadVars[k] = v
		}
	}

	tmpl, err := s.promptRegistry.LoadTemplate(s.taskType, loadVars, "")
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

	// Derive the turn from the persisted transcript rather than counting
	// executions. A counter is only right while one pipeline instance outlives
	// the conversation: rebuild the pipeline per turn and it restarts, and it
	// cannot know about turns that happened before this process started. The
	// transcript knows both, which is also what the number has to line up with.
	s.publishTurnIndex(countUserTurns(historyMessages))

	if err := s.emitHistoryMessages(ctx, historyMessages, output); err != nil {
		return err
	}

	return s.forwardInput(ctx, input, output)
}

// countUserTurns counts the user turns in a transcript.
func countUserTurns(messages []types.Message) int {
	n := 0
	for i := range messages {
		if messages[i].Role == roleUser {
			n++
		}
	}
	return n
}

// publishTurnIndex records how many user turns the transcript already holds.
// forwardInput adds this turn's own user message as it passes, so downstream
// stages read the 1-based number of the turn being executed.
//
// Counting the incoming message rather than assuming one is what keeps a
// resumed turn correct: when a turn suspends for a client tool and the user
// message was already persisted, it arrives in history and no new user element
// follows, so the turn does not advance twice.
func (s *StateStoreLoadStage) publishTurnIndex(priorTurns int) {
	s.turnState.SetTurnIndex(priorTurns)
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
		// This turn's own user message completes the count started from the
		// persisted transcript. Writing before the forward keeps the
		// happens-before: the next stage observes the turn index by the time
		// the element reaches it.
		if elem.Message != nil && elem.Message.Role == roleUser && !elem.Meta.FromHistory {
			s.turnState.AdvanceTurn()
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
