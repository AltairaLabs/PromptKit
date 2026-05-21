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
	Event          string           `json:"event"`
	ContextSummary string           `json:"context"`
	HostExtras     tools.HostExtras `json:"host_extras,omitempty"`
}

// TransitionExecutor implements tools.Executor for workflow__transition.
//
// By default it defers the state transition (ProcessEvent) until CommitPending
// is called, ensuring the full pipeline completes before state changes. When
// the transition target state has Control == ControlModeAgent the transition
// commits eagerly inside Execute so the next LLM call in the same pipeline
// turn sees the new state's events. The optional OnCommit callback is invoked
// for both eager and deferred commits so consumers (Arena, SDK) can run their
// post-commit work (re-register tool descriptor, update scenario TaskType,
// emit observability events) from a single hook.
type TransitionExecutor struct {
	mu            sync.Mutex
	sm            *StateMachine
	spec          *Spec
	pending       *PendingTransition
	onCommit      func(*TransitionResult)
	onCommitError func(event string, err error)
}

// NewTransitionExecutor creates a TransitionExecutor for the given state machine.
func NewTransitionExecutor(sm *StateMachine, spec *Spec) *TransitionExecutor {
	return &TransitionExecutor{sm: sm, spec: spec}
}

// SetOnCommit registers a callback fired after every successful commit
// (eager or deferred). Pass nil to clear.
//
// Callbacks run while the executor's internal lock is held; they must not
// re-enter the executor's public methods (Execute / CommitPending / etc.).
func (e *TransitionExecutor) SetOnCommit(fn func(*TransitionResult)) {
	e.mu.Lock()
	e.onCommit = fn
	e.mu.Unlock()
}

// SetOnCommitError registers a callback fired when ProcessEvent fails on
// either commit path (eager from Execute, deferred from CommitPending).
// Consumers wire their workflow observability error emit (e.g.,
// workflow.max_visits_exceeded, workflow.budget_exhausted) through this
// hook so eager-control failures are observable too. Pass nil to clear.
//
// Same locking contract as SetOnCommit: the callback runs while the
// executor's internal lock is held; do not re-enter the executor.
func (e *TransitionExecutor) SetOnCommitError(fn func(event string, err error)) {
	e.mu.Lock()
	e.onCommitError = fn
	e.mu.Unlock()
}

// Name implements tools.Executor. Returns the mode name for registry routing.
func (e *TransitionExecutor) Name() string { return TransitionExecutorMode }

// Execute implements tools.Executor.
//
// If the event's target state declares Control == ControlModeAgent the
// transition is committed immediately (ProcessEvent + OnCommit) and the
// returned response carries the new state's prompt_task, description, and
// available events so the LLM's next tool-loop iteration can act on them.
//
// Otherwise the request is stored as pending and CommitPending applies it
// after the pipeline turn finishes. This is the historical (default)
// behavior and matches RFC 0005's deferred-commit pattern.
//
// Context-summary handling differs between paths:
//   - Deferred (control: user): the LLM's `context` argument is stored on
//     the PendingTransition and surfaced to the new conversation as the
//     `workflow_context` template variable when the consumer opens it.
//   - Eager (control: agent): the `context` argument is **discarded**.
//     Agent-controlled states are transient — the conversation never
//     reopens for them — so there's no destination prompt to seed. If
//     you need the LLM to summarize before handing off to a terminal
//     state, do it on the *user-controlled* transition that ends the
//     chain, not on the agent-controlled hops along the way.
func (e *TransitionExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	var a struct {
		Event   string `json:"event"`
		Context string `json:"context"`
	}
	// Decode typed fields and capture any unknown top-level args. Hosts use
	// sdk.WithToolDescriptorOverride to extend workflow__transition's input
	// schema with deployment-specific fields; without this passthrough those
	// fields would be silently dropped before the host's OnCommit callback
	// could see them.
	rawExtras, err := tools.DecodeArgsExtras(args, &a, "event", "context")
	if err != nil {
		return nil, fmt.Errorf("failed to parse transition args: %w", err)
	}
	var extras tools.HostExtras
	if len(rawExtras) > 0 {
		extras = tools.HostExtras(rawExtras)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.isAgentControlledTargetLocked(a.Event) {
		tr, err := e.sm.ProcessEvent(a.Event)
		if err != nil {
			if e.onCommitError != nil {
				e.onCommitError(a.Event, err)
			}
			return nil, fmt.Errorf("agent-controlled transition failed: %w", err)
		}
		tr.HostExtras = extras
		if e.onCommit != nil {
			e.onCommit(tr)
		}
		return json.Marshal(buildEagerTransitionResponse(tr, e.spec))
	}

	e.pending = &PendingTransition{
		Event:          a.Event,
		ContextSummary: a.Context,
		HostExtras:     extras,
	}
	return json.Marshal(buildTransitionResponse(a.Event, e.spec))
}

// isAgentControlledTargetLocked looks up the target state for event on the
// current source state and reports whether it declares Control == ControlModeAgent.
// Returns false for unknown events; ProcessEvent will surface the error on commit.
// Caller must hold e.mu.
func (e *TransitionExecutor) isAgentControlledTargetLocked(event string) bool {
	current := e.sm.CurrentState()
	src, ok := e.spec.States[current]
	if !ok || src == nil {
		return false
	}
	target, ok := src.OnEvent[event]
	if !ok {
		return false
	}
	return e.spec.States[target].IsAgentControlled()
}

// CommitPending applies the pending transition by calling ProcessEvent.
// Returns nil, nil if no transition is pending. After commit, the pending
// state is cleared. Thread-safe. Fires OnCommit on success.
func (e *TransitionExecutor) CommitPending() (*TransitionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.pending == nil {
		return nil, nil
	}

	pt := e.pending
	e.pending = nil

	tr, err := e.sm.ProcessEvent(pt.Event)
	if err != nil {
		if e.onCommitError != nil {
			e.onCommitError(pt.Event, err)
		}
		return nil, err
	}
	tr.HostExtras = pt.HostExtras
	if e.onCommit != nil {
		e.onCommit(tr)
	}
	return tr, nil
}

// Pending returns the current pending transition, or nil if none.
func (e *TransitionExecutor) Pending() *PendingTransition {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pending
}

// StateMachine returns the underlying state machine for metadata access.
func (e *TransitionExecutor) StateMachine() *StateMachine {
	return e.sm
}

// ClearPending discards any pending transition without committing.
func (e *TransitionExecutor) ClearPending() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending = nil
}

// RegisterForState registers the workflow__transition tool in the given
// registry for the specified state, with Mode set for executor routing.
//
// When called with a terminal state (Terminal: true OR no on_event), the
// previous descriptor — if any — is removed from the registry instead of
// re-registered. Without this, the LLM would still see workflow__transition
// in the tool list after entering a terminal state and could call it with
// the previous state's now-stale events. Externally orchestrated states
// skip both register and unregister: the caller owns the descriptor's
// lifecycle.
func (e *TransitionExecutor) RegisterForState(registry *tools.Registry, state *State) {
	if registry == nil || state == nil {
		return
	}
	if state.Orchestration == OrchestrationExternal {
		return
	}
	if state.Terminal || len(state.OnEvent) == 0 {
		registry.Unregister(TransitionToolName)
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

// buildEagerTransitionResponse creates the LLM-facing response for an
// already-committed (agent-controlled) transition. It carries the new
// state's prompt_task, description, and available events so the LLM
// can act on them in the same pipeline turn.
func buildEagerTransitionResponse(tr *TransitionResult, spec *Spec) map[string]any {
	result := map[string]any{
		"status": "transitioned",
		"from":   tr.From,
		"to":     tr.To,
		"event":  tr.Event,
	}
	if tr.Redirected {
		result["redirected"] = true
		result["redirect_reason"] = tr.RedirectReason
		result["original_target"] = tr.OriginalTarget
	}
	if newState := spec.States[tr.To]; newState != nil {
		if newState.PromptTask != "" {
			result["prompt_task"] = newState.PromptTask
		}
		if newState.Description != "" {
			result["description"] = newState.Description
		}
		if len(newState.OnEvent) > 0 {
			result["available_events"] = SortedEvents(newState.OnEvent)
		}
		if newState.Terminal || len(newState.OnEvent) == 0 {
			result["terminal"] = true
		}
	}
	return result
}
