package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

var (
	// ErrInvalidEvent is returned when an event is not defined for the current state.
	ErrInvalidEvent = errors.New("invalid event for current state")
	// ErrTerminalState is returned when trying to process an event in a terminal state.
	ErrTerminalState = errors.New("current state is terminal (no outgoing transitions)")
)

// TimeFunc returns the current time. Override for deterministic tests.
type TimeFunc func() time.Time

// StateMachine manages workflow state transitions.
type StateMachine struct {
	mu      sync.RWMutex
	spec    *Spec
	context *Context
	now     TimeFunc
}

// NewStateMachine creates a state machine from a workflow spec.
// It initializes the context to the entry state.
func NewStateMachine(spec *Spec) *StateMachine {
	return &StateMachine{
		spec:    spec,
		context: NewContext(spec.Entry, time.Now()),
		now:     time.Now,
	}
}

// NewStateMachineFromContext restores a state machine from persisted context.
func NewStateMachineFromContext(spec *Spec, ctx *Context) *StateMachine {
	return &StateMachine{
		spec:    spec,
		context: ctx,
		now:     time.Now,
	}
}

// WithTimeFunc sets a custom time function for deterministic tests.
func (sm *StateMachine) WithTimeFunc(fn TimeFunc) *StateMachine {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.now = fn
	return sm
}

// CurrentState returns the name of the current state.
func (sm *StateMachine) CurrentState() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.context.CurrentState
}

// CurrentPromptTask returns the prompt_task for the current state.
func (sm *StateMachine) CurrentPromptTask() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return ""
	}
	return state.PromptTask
}

// ProcessEvent applies an event and transitions to the target state.
// Returns a TransitionResult describing the transition (including any
// max_visits redirect). Returns ErrMaxVisitsExceeded when the target
// state's visit limit is reached and no on_max_visits fallback is set.
// Returns ErrBudgetExhausted when a workflow-level budget limit is reached.
func (sm *StateMachine) ProcessEvent(event string) (*TransitionResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Budget check runs before event resolution.
	if err := sm.checkBudgetLocked(); err != nil {
		return nil, err
	}

	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return nil, fmt.Errorf("%w: state %q not found in spec",
			ErrInvalidEvent, sm.context.CurrentState)
	}

	if state.Terminal || len(state.OnEvent) == 0 {
		return nil, fmt.Errorf("%w: state %q is terminal",
			ErrTerminalState, sm.context.CurrentState)
	}

	target, ok := state.OnEvent[event]
	if !ok {
		return nil, fmt.Errorf("%w: event %q not defined for state %q (available: %v)",
			ErrInvalidEvent, event, sm.context.CurrentState, sm.availableEventsLocked())
	}

	fromState := sm.context.CurrentState
	originalTarget := target

	// Loop guard: check max_visits on the target state before entering it.
	targetState := sm.spec.States[target]
	if targetState != nil && targetState.MaxVisits > 0 {
		visits := sm.context.VisitCounts[target]
		if visits >= targetState.MaxVisits {
			if targetState.OnMaxVisits != "" {
				target = targetState.OnMaxVisits
			} else {
				return nil, fmt.Errorf("%w: state %q visited %d times (max %d)",
					ErrMaxVisitsExceeded, originalTarget, visits, targetState.MaxVisits)
			}
		}
	}

	sm.context.RecordTransition(fromState, target, event, sm.now())

	result := &TransitionResult{
		From:  fromState,
		To:    target,
		Event: event,
	}
	if target != originalTarget {
		result.Redirected = true
		result.RedirectReason = fmt.Sprintf("max_visits (%d) reached for %q",
			targetState.MaxVisits, originalTarget)
		result.OriginalTarget = originalTarget
	}

	logger.Info("workflow state transition",
		"from", fromState, "to", target, "event", event,
		"redirected", result.Redirected)
	return result, nil
}

// IsTerminal returns true if the current state is terminal.
// A state is terminal when explicitly marked (Terminal: true) or
// when it has no outgoing transitions (backward compatible).
func (sm *StateMachine) IsTerminal() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return true
	}
	return state.Terminal || len(state.OnEvent) == 0
}

// AvailableEvents returns the set of valid events for the current state, sorted.
func (sm *StateMachine) AvailableEvents() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.availableEventsLocked()
}

// availableEventsLocked returns the set of valid events. Caller must hold at least a read lock.
func (sm *StateMachine) availableEventsLocked() []string {
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil || len(state.OnEvent) == 0 {
		return nil
	}
	events := make([]string, 0, len(state.OnEvent))
	for e := range state.OnEvent {
		events = append(events, e)
	}
	slices.Sort(events)
	return events
}

// Context returns a snapshot of the current workflow context for persistence.
func (sm *StateMachine) Context() *Context {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.context.Clone()
}

// IncrementToolCalls adds n to the workflow-wide tool call counter.
// Thread-safe; intended to be called by the SDK after tool executions.
func (sm *StateMachine) IncrementToolCalls(n int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.context.IncrementToolCalls(n)
}

// checkBudgetLocked checks workflow-level budget limits.
// Caller must hold the write lock.
func (sm *StateMachine) checkBudgetLocked() error {
	budget := sm.parseBudget()
	if budget == nil {
		return nil
	}
	if budget.MaxTotalVisits > 0 && sm.context.TotalVisits() >= budget.MaxTotalVisits {
		return fmt.Errorf("%w: total visits %d reached limit %d",
			ErrBudgetExhausted, sm.context.TotalVisits(), budget.MaxTotalVisits)
	}
	if budget.MaxToolCalls > 0 && sm.context.TotalToolCalls >= budget.MaxToolCalls {
		return fmt.Errorf("%w: tool calls %d reached limit %d",
			ErrBudgetExhausted, sm.context.TotalToolCalls, budget.MaxToolCalls)
	}
	if budget.MaxWallTimeSec > 0 {
		elapsed := sm.now().Sub(sm.context.StartedAt)
		if int(elapsed.Seconds()) >= budget.MaxWallTimeSec {
			return fmt.Errorf("%w: wall time %ds reached limit %ds",
				ErrBudgetExhausted, int(elapsed.Seconds()), budget.MaxWallTimeSec)
		}
	}
	return nil
}

// parseBudget extracts the Budget from Spec.Engine["budget"], if present.
func (sm *StateMachine) parseBudget() *Budget {
	if sm.spec.Engine == nil {
		return nil
	}
	raw, ok := sm.spec.Engine["budget"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var b Budget
	if err := json.Unmarshal(data, &b); err != nil {
		return nil
	}
	if b.MaxTotalVisits == 0 && b.MaxToolCalls == 0 && b.MaxWallTimeSec == 0 {
		return nil
	}
	return &b
}
