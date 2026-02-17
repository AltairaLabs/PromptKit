package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// WorkflowConversation manages a stateful workflow that transitions between
// different prompts in a pack based on events.
//
// Each state in the workflow maps to a prompt_task in the pack. When a
// transition occurs, the current conversation is closed and a new one is
// opened for the target state's prompt.
//
// Basic usage:
//
//	wc, err := sdk.OpenWorkflow("./support.pack.json")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer wc.Close()
//
//	resp, _ := wc.Send(ctx, "I need help with billing")
//	fmt.Println(resp.Text())
//
//	newState, _ := wc.Transition("Escalate")
//	fmt.Println("Moved to:", newState)
type WorkflowConversation struct {
	mu                  sync.RWMutex
	machine             *workflow.StateMachine
	workflowSpec        *workflow.Spec
	packPath            string
	sdkPack             *pack.Pack
	activeConv          *Conversation
	opts                []Option
	emitter             *events.Emitter
	stateStore          statestore.Store
	workflowID          string
	contextCarryForward bool
	closed              bool
}

// defaultMaxSummaryMessages is the max messages to include in a carry-forward summary.
const defaultMaxSummaryMessages = 10

// OpenWorkflow loads a pack file and creates a WorkflowConversation.
//
// The pack must contain a workflow section. The initial conversation is opened
// for the workflow's entry state prompt_task.
//
//	wc, err := sdk.OpenWorkflow("./support.pack.json",
//	    sdk.WithModel("gpt-4o"),
//	)
func OpenWorkflow(packPath string, opts ...Option) (*WorkflowConversation, error) {
	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	// Determine skip-schema from options
	cfg := &config{}
	for _, opt := range opts {
		if optErr := opt(cfg); optErr != nil {
			return nil, fmt.Errorf("failed to apply option: %w", optErr)
		}
	}

	p, err := pack.Load(absPath, pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	if p.Workflow == nil {
		return nil, ErrNoWorkflow
	}

	// Convert SDK workflow spec to runtime workflow spec
	spec := convertWorkflowSpec(p.Workflow)
	machine := workflow.NewStateMachine(spec)

	// Open initial conversation for entry state's prompt_task
	promptName := machine.CurrentPromptTask()
	conv, err := Open(packPath, promptName, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to open initial conversation for state %q (prompt %q): %w",
			machine.CurrentState(), promptName, err)
	}

	// Create emitter from event bus if configured
	var emitter *events.Emitter
	if cfg.eventBus != nil {
		emitter = events.NewEmitter(cfg.eventBus, "", "", "")
	}

	return &WorkflowConversation{
		machine:             machine,
		workflowSpec:        spec,
		packPath:            packPath,
		sdkPack:             p,
		activeConv:          conv,
		opts:                opts,
		emitter:             emitter,
		stateStore:          cfg.stateStore,
		workflowID:          cfg.conversationID,
		contextCarryForward: cfg.contextCarryForward,
	}, nil
}

// ResumeWorkflow restores a WorkflowConversation from a previously persisted state.
//
// The workflow context is loaded from the state store's metadata["workflow"] key.
// A state store must be configured via WithStateStore.
//
//	wc, err := sdk.ResumeWorkflow("workflow-123", "./support.pack.json",
//	    sdk.WithStateStore(store),
//	)
func ResumeWorkflow(workflowID, packPath string, opts ...Option) (*WorkflowConversation, error) {
	cfg := &config{}
	for _, opt := range opts {
		if optErr := opt(cfg); optErr != nil {
			return nil, fmt.Errorf("failed to apply option: %w", optErr)
		}
	}

	if cfg.stateStore == nil {
		return nil, ErrNoStateStore
	}

	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	p, err := pack.Load(absPath, pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	if p.Workflow == nil {
		return nil, ErrNoWorkflow
	}

	// Load workflow context from state store
	ctx := context.Background()
	state, err := cfg.stateStore.Load(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to load workflow state: %w", err)
	}
	if state == nil {
		return nil, ErrConversationNotFound
	}

	// Extract workflow context from metadata
	wfCtx, err := extractWorkflowContext(state.Metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to restore workflow context: %w", err)
	}

	spec := convertWorkflowSpec(p.Workflow)
	machine := workflow.NewStateMachineFromContext(spec, wfCtx)

	// Open conversation for current state's prompt_task
	promptName := machine.CurrentPromptTask()
	optsWithID := make([]Option, len(opts), len(opts)+1)
	copy(optsWithID, opts)
	optsWithID = append(optsWithID, WithConversationID(workflowID))
	conv, err := Open(packPath, promptName, optsWithID...)
	if err != nil {
		return nil, fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			machine.CurrentState(), promptName, err)
	}

	var emitter *events.Emitter
	if cfg.eventBus != nil {
		emitter = events.NewEmitter(cfg.eventBus, "", "", "")
	}

	return &WorkflowConversation{
		machine:             machine,
		workflowSpec:        spec,
		packPath:            packPath,
		sdkPack:             p,
		activeConv:          conv,
		opts:                opts,
		emitter:             emitter,
		stateStore:          cfg.stateStore,
		workflowID:          workflowID,
		contextCarryForward: cfg.contextCarryForward,
	}, nil
}

// Send sends a message to the active state's conversation and returns the response.
//
//	resp, err := wc.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
func (wc *WorkflowConversation) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	wc.mu.RLock()
	if wc.closed {
		wc.mu.RUnlock()
		return nil, ErrWorkflowClosed
	}
	conv := wc.activeConv
	wc.mu.RUnlock()

	return conv.Send(ctx, message, opts...)
}

// Transition processes an event and moves to the next state.
//
// The current conversation is closed and a new one is opened for the target
// state's prompt_task. Returns the new state name.
//
//	newState, err := wc.Transition("Escalate")
//	if errors.Is(err, workflow.ErrInvalidEvent) {
//	    fmt.Println("Available events:", wc.AvailableEvents())
//	}
func (wc *WorkflowConversation) Transition(event string) (string, error) {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.closed {
		return "", ErrWorkflowClosed
	}

	if wc.machine.IsTerminal() {
		return "", ErrWorkflowTerminal
	}

	fromState := wc.machine.CurrentState()

	// Capture context summary before transition if carry-forward is enabled
	var summary string
	if wc.contextCarryForward && wc.activeConv != nil {
		summary = buildContextSummary(fromState, wc.activeConv)
	}

	if err := wc.machine.ProcessEvent(event); err != nil {
		return "", err
	}

	toState := wc.machine.CurrentState()

	// Close old conversation
	if wc.activeConv != nil {
		_ = wc.activeConv.Close()
	}

	// Build options, injecting summary as a template variable if available
	opts := wc.opts
	if summary != "" {
		opts = append(append([]Option{}, wc.opts...), WithVariables(map[string]string{
			"workflow_context": summary,
		}))
	}

	// Open new conversation for the new state
	promptName := wc.machine.CurrentPromptTask()
	conv, err := Open(wc.packPath, promptName, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			toState, promptName, err)
	}
	wc.activeConv = conv

	// Persist workflow context if state store is configured and state is not transient
	if wc.stateStore != nil && wc.workflowID != "" {
		wc.persistWorkflowContext()
	}

	// Emit transition event
	if wc.emitter != nil {
		wc.emitter.WorkflowTransitioned(fromState, toState, event, promptName)
		if wc.machine.IsTerminal() {
			wc.emitter.WorkflowCompleted(toState, wc.machine.Context().TransitionCount())
		}
	}

	return toState, nil
}

// CurrentState returns the current workflow state name.
func (wc *WorkflowConversation) CurrentState() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.CurrentState()
}

// CurrentPromptTask returns the prompt_task for the current state.
func (wc *WorkflowConversation) CurrentPromptTask() string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.CurrentPromptTask()
}

// IsComplete returns true if the workflow is in a terminal state (no outgoing transitions).
func (wc *WorkflowConversation) IsComplete() bool {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.IsTerminal()
}

// AvailableEvents returns the events available in the current state, sorted alphabetically.
func (wc *WorkflowConversation) AvailableEvents() []string {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.AvailableEvents()
}

// Context returns a snapshot of the workflow execution context including
// transition history and metadata.
func (wc *WorkflowConversation) Context() *workflow.Context {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.machine.Context()
}

// OrchestrationMode returns the orchestration mode of the current state.
// External orchestration means transitions are driven by outside callers
// (e.g., HTTP handlers, message queues) rather than from within the conversation loop.
func (wc *WorkflowConversation) OrchestrationMode() workflow.Orchestration {
	wc.mu.RLock()
	defer wc.mu.RUnlock()

	if wc.workflowSpec == nil {
		return workflow.OrchestrationInternal
	}
	state := wc.workflowSpec.States[wc.machine.CurrentState()]
	if state == nil || state.Orchestration == "" {
		return workflow.OrchestrationInternal
	}
	return state.Orchestration
}

// ActiveConversation returns the current state's Conversation.
// Use this to access conversation-specific methods like SetVar, OnTool, etc.
func (wc *WorkflowConversation) ActiveConversation() *Conversation {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	return wc.activeConv
}

// Close closes the active conversation and marks the workflow as closed.
func (wc *WorkflowConversation) Close() error {
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.closed {
		return nil
	}
	wc.closed = true

	if wc.activeConv != nil {
		return wc.activeConv.Close()
	}
	return nil
}

// filterRelevantMessages removes system messages from the conversation history.
func filterRelevantMessages(messages []types.Message) []types.Message {
	result := make([]types.Message, 0, len(messages))
	for i := range messages {
		if messages[i].Role != "system" {
			result = append(result, messages[i])
		}
	}
	return result
}

// extractMessageText returns the text content of a message, checking Content
// first and falling back to the first text Part.
func extractMessageText(msg *types.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	for _, part := range msg.Parts {
		if part.Text != nil {
			return *part.Text
		}
	}
	return ""
}

// persistWorkflowContext saves the workflow context to the state store.
// It respects the persistence hint on the current state: transient states skip writes.
func (wc *WorkflowConversation) persistWorkflowContext() {
	// Check if current state is transient
	currentState := wc.machine.CurrentState()
	if wc.workflowSpec != nil {
		if st, ok := wc.workflowSpec.States[currentState]; ok {
			if st.Persistence == workflow.PersistenceTransient {
				return
			}
		}
	}

	ctx := context.Background()
	state, err := wc.stateStore.Load(ctx, wc.workflowID)
	if err != nil || state == nil {
		// Create new state if not found
		state = &statestore.ConversationState{
			ID:       wc.workflowID,
			Metadata: make(map[string]any),
		}
	}
	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}

	wfCtx := wc.machine.Context()
	state.Metadata["workflow"] = wfCtx
	_ = wc.stateStore.Save(ctx, state)
}

// extractWorkflowContext extracts and deserializes workflow.Context from state metadata.
func extractWorkflowContext(metadata map[string]any) (*workflow.Context, error) {
	raw, ok := metadata["workflow"]
	if !ok {
		return nil, fmt.Errorf("no workflow context in metadata")
	}

	// Handle both direct *workflow.Context and JSON-deserialized map
	switch v := raw.(type) {
	case *workflow.Context:
		return v, nil
	case map[string]any:
		// Re-serialize and deserialize through JSON for type safety
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal workflow context: %w", err)
		}
		var wfCtx workflow.Context
		if err := json.Unmarshal(data, &wfCtx); err != nil {
			return nil, fmt.Errorf("failed to unmarshal workflow context: %w", err)
		}
		return &wfCtx, nil
	default:
		return nil, fmt.Errorf("unexpected workflow context type: %T", raw)
	}
}

// convertWorkflowSpec converts the SDK's internal pack.WorkflowSpec to a
// runtime workflow.Spec for use with the state machine.
func convertWorkflowSpec(sdkSpec *pack.WorkflowSpec) *workflow.Spec {
	states := make(map[string]*workflow.State, len(sdkSpec.States))
	for name, s := range sdkSpec.States {
		states[name] = &workflow.State{
			PromptTask:    s.PromptTask,
			Description:   s.Description,
			OnEvent:       s.OnEvent,
			Persistence:   workflow.Persistence(s.Persistence),
			Orchestration: workflow.Orchestration(s.Orchestration),
		}
	}
	return &workflow.Spec{
		Version: sdkSpec.Version,
		Entry:   sdkSpec.Entry,
		States:  states,
		Engine:  sdkSpec.Engine,
	}
}
