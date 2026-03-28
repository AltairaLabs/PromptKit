package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TransitionExecutorMode is the executor name used for Mode-based routing.
const TransitionExecutorMode = "workflow-transition"

// PendingTransition captures a deferred workflow transition from a tool call.
type PendingTransition struct {
	Event          string `json:"event"`
	ContextSummary string `json:"context"`
}

// TransitionExecutor implements tools.Executor for workflow__transition.
// It defers the actual state transition (ProcessEvent) until CommitPending
// is called, ensuring the full pipeline completes before state changes.
//
// Both SDK and Arena use this executor. After pipeline/turn execution,
// the consumer calls CommitPending() to apply the transition.
type TransitionExecutor struct {
	mu      sync.Mutex
	sm      *StateMachine
	spec    *Spec
	pending *PendingTransition
}

// NewTransitionExecutor creates a TransitionExecutor for the given state machine.
func NewTransitionExecutor(sm *StateMachine, spec *Spec) *TransitionExecutor {
	return &TransitionExecutor{sm: sm, spec: spec}
}

// Name implements tools.Executor. Returns the mode name for registry routing.
func (e *TransitionExecutor) Name() string { return TransitionExecutorMode }

// Execute implements tools.Executor. Stores the transition request and returns
// a confirmation to the LLM. Does NOT call ProcessEvent — that happens in
// CommitPending after the pipeline completes.
func (e *TransitionExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	var a struct {
		Event   string `json:"event"`
		Context string `json:"context"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("failed to parse transition args: %w", err)
	}

	e.mu.Lock()
	e.pending = &PendingTransition{Event: a.Event, ContextSummary: a.Context}
	e.mu.Unlock()

	return json.Marshal(buildTransitionResponse(a.Event, e.spec))
}

// CommitPending applies the pending transition by calling ProcessEvent.
// Returns nil, nil if no transition is pending. After commit, the pending
// state is cleared. Thread-safe.
func (e *TransitionExecutor) CommitPending() (*TransitionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pending == nil {
		return nil, nil
	}

	pt := e.pending
	e.pending = nil

	return e.sm.ProcessEvent(pt.Event)
}

// Pending returns the current pending transition, or nil if none.
func (e *TransitionExecutor) Pending() *PendingTransition {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pending
}

// ClearPending discards any pending transition without committing.
func (e *TransitionExecutor) ClearPending() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending = nil
}

// RegisterForState registers the workflow__transition tool in the given
// registry for the specified state, with Mode set for executor routing.
// Skips terminal states and externally orchestrated states.
func (e *TransitionExecutor) RegisterForState(registry *tools.Registry, state *State) {
	if registry == nil || state == nil || len(state.OnEvent) == 0 {
		return
	}
	if state.Terminal || state.Orchestration == OrchestrationExternal {
		return
	}
	evts := SortedEvents(state.OnEvent)
	desc := BuildTransitionToolDescriptor(evts)
	desc.Mode = TransitionExecutorMode
	_ = registry.Register(desc)
}

// buildTransitionResponse creates the LLM-facing response for a scheduled transition.
func buildTransitionResponse(event string, spec *Spec) map[string]string {
	result := map[string]string{
		"status": "transition_scheduled",
		"event":  event,
	}
	// Look up target state for informative response
	for _, state := range spec.States {
		if target, ok := state.OnEvent[event]; ok {
			result["target_state"] = target
			break
		}
	}
	return result
}
