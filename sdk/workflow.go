package sdk

import (
	"context"
	"fmt"
	"sync"

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
	mu         sync.RWMutex
	machine    *workflow.StateMachine
	packPath   string
	sdkPack    *pack.Pack
	activeConv *Conversation
	opts       []Option
	closed     bool
}

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

	return &WorkflowConversation{
		machine:    machine,
		packPath:   packPath,
		sdkPack:    p,
		activeConv: conv,
		opts:       opts,
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

	if err := wc.machine.ProcessEvent(event); err != nil {
		return "", err
	}

	// Close old conversation
	if wc.activeConv != nil {
		_ = wc.activeConv.Close()
	}

	// Open new conversation for the new state
	promptName := wc.machine.CurrentPromptTask()
	conv, err := Open(wc.packPath, promptName, wc.opts...)
	if err != nil {
		return "", fmt.Errorf("failed to open conversation for state %q (prompt %q): %w",
			wc.machine.CurrentState(), promptName, err)
	}
	wc.activeConv = conv

	return wc.machine.CurrentState(), nil
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
