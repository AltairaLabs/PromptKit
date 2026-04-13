package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
	workflowCap         *WorkflowCapability
	transExec           *workflow.TransitionExecutor
	artifactExec        *workflow.ArtifactExecutor
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

	// Set custom logger before any logging occurs — only once
	if cfg.logger != nil {
		setLoggerOnce(cfg.logger)
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

	// Create and init WorkflowCapability
	wfCap := NewWorkflowCapability()
	if err := wfCap.Init(newCapabilityContext(p, promptName, cfg)); err != nil {
		_ = conv.Close()
		return nil, fmt.Errorf("workflow capability init failed: %w", err)
	}

	wc := &WorkflowConversation{
		machine:             machine,
		workflowSpec:        spec,
		packPath:            packPath,
		sdkPack:             p,
		activeConv:          conv,
		opts:                opts,
		emitter:             conv.newEmitter(cfg.eventBus),
		stateStore:          cfg.stateStore,
		workflowID:          cfg.conversationID,
		contextCarryForward: cfg.contextCarryForward,
		workflowCap:         wfCap,
	}

	// Register workflow tools for the initial state
	wc.registerWorkflowTools()

	return wc, nil
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

	// Create and init WorkflowCapability
	wfCap := NewWorkflowCapability()
	if err := wfCap.Init(newCapabilityContext(p, promptName, cfg)); err != nil {
		_ = conv.Close()
		return nil, fmt.Errorf("workflow capability init failed: %w", err)
	}

	wc := &WorkflowConversation{
		machine:             machine,
		workflowSpec:        spec,
		packPath:            packPath,
		sdkPack:             p,
		activeConv:          conv,
		opts:                opts,
		stateStore:          cfg.stateStore,
		workflowID:          workflowID,
		contextCarryForward: cfg.contextCarryForward,
		workflowCap:         wfCap,
	}
	wc.emitter = conv.newEmitter(cfg.eventBus)

	// Register workflow tools for the current state
	wc.registerWorkflowTools()

	return wc, nil
}

// Send sends a message to the active state's conversation and returns the response.
// If the LLM calls the workflow__transition tool, the transition is processed
// after the Send completes.
//
//	resp, err := wc.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
func (wc *WorkflowConversation) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	wc.mu.Lock()
	if wc.closed {
		wc.mu.Unlock()
		return nil, ErrWorkflowClosed
	}
	conv := wc.activeConv
	if wc.transExec != nil {
		wc.transExec.ClearPending()
	}
	wc.mu.Unlock()

	resp, err := conv.Send(ctx, message, opts...)
	if err != nil {
		return nil, err
	}

	// Commit any pending transition from a workflow__transition tool call.
	// The TransitionExecutor deferred the ProcessEvent until now.
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.closed {
		return nil, ErrWorkflowClosed
	}
	if err := wc.commitDeferredTransition(); err != nil {
		return resp, err
	}
	return resp, nil
}

// commitDeferredTransition commits any pending transition recorded by the
// workflow__transition tool during the just-completed Send. Errors from
// ProcessEvent are routed through emitWorkflowError before being returned
// so observability events fire even when the transition aborts. Caller
// must hold wc.mu.
func (wc *WorkflowConversation) commitDeferredTransition() error {
	if wc.transExec == nil {
		return nil
	}
	pending := wc.transExec.Pending()
	if pending == nil {
		return nil
	}
	contextSummary := pending.ContextSummary
	result, commitErr := wc.transExec.CommitPending()
	if commitErr != nil {
		wc.emitWorkflowError(pending.Event, commitErr)
		return commitErr
	}
	if result != nil {
		if _, transErr := wc.applyTransition(result, contextSummary); transErr != nil {
			return transErr
		}
	}
	return nil
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

	// Build context summary from active conversation if carry-forward is enabled
	var contextSummary string
	if wc.contextCarryForward && wc.activeConv != nil {
		contextSummary = buildContextSummary(wc.machine.CurrentState(), wc.activeConv)
	}

	return wc.transitionInternal(event, contextSummary)
}

// registerWorkflowTools registers the workflow tool executors and descriptors
// for the current state on the active conversation's tool registry.
func (wc *WorkflowConversation) registerWorkflowTools() {
	registry := wc.activeConv.ToolRegistry()
	state := wc.workflowSpec.States[wc.machine.CurrentState()]

	// Create and register transition executor (deferred commit pattern)
	wc.transExec = workflow.NewTransitionExecutor(wc.machine, wc.workflowSpec)
	registry.RegisterExecutor(wc.transExec)
	wc.transExec.RegisterForState(registry, state)

	// Create and register artifact executor if spec declares artifacts
	wc.artifactExec = workflow.NewArtifactExecutor(wc.machine)
	registry.RegisterExecutor(wc.artifactExec)
	workflow.RegisterArtifactTool(registry, wc.workflowSpec)
}

// applyTransition handles post-commit transition logic: close old conversation,
// open new one, re-register tools, persist context, emit events.
func (wc *WorkflowConversation) applyTransition(
	result *workflow.TransitionResult, contextSummary string,
) (string, error) {
	toState := result.To

	// Close old conversation
	if wc.activeConv != nil {
		_ = wc.activeConv.Close()
	}

	// Build options, injecting context as a template variable if available
	opts := wc.opts
	if contextSummary != "" {
		opts = append(append([]Option{}, wc.opts...), WithVariables(map[string]string{
			"workflow_context": contextSummary,
		}))
	}

	// Inject artifact values as template variables
	if arts := wc.machine.Artifacts(); len(arts) > 0 {
		artVars := make(map[string]string, len(arts))
		for k, v := range arts {
			artVars["artifacts."+k] = v
		}
		opts = append(append([]Option{}, opts...), WithVariables(artVars))
	}

	// Open new conversation for the new state
	promptName := wc.machine.CurrentPromptTask()
	conv, err := Open(wc.packPath, promptName, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			toState, promptName, err)
	}
	wc.activeConv = conv

	// Re-register workflow tools for the new state
	wc.registerWorkflowTools()

	// Persist workflow context if state store is configured
	if wc.stateStore != nil && wc.workflowID != "" {
		wc.persistWorkflowContext()
	}

	// Emit transition events (transitioned, max_visits_exceeded if this was
	// a redirect, and completed if the new state is terminal).
	wc.emitTransitionEvents(result, toState, promptName)

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

// roleSystem is the role string for system messages (extracted as a constant to satisfy goconst).
const roleSystem = "system"

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
	if err != nil {
		logger.Warn("failed to load workflow state, creating fresh state",
			"workflow_id", wc.workflowID, "error", err)
	}
	if err != nil || state == nil {
		// Create new state if not found or on load error
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
	if err := wc.stateStore.Save(ctx, state); err != nil {
		logger.Warn("failed to persist workflow context", "workflow_id", wc.workflowID, "error", err)
	}
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

// emitTransitionEvents and maxVisitsForState live in sdk/workflow_events.go.
