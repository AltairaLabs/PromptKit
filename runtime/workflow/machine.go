package workflow

import (
	"errors"
	"fmt"
	"slices"
	"time"
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
	sm.now = fn
	return sm
}

// CurrentState returns the name of the current state.
func (sm *StateMachine) CurrentState() string {
	return sm.context.CurrentState
}

// CurrentPromptTask returns the prompt_task for the current state.
func (sm *StateMachine) CurrentPromptTask() string {
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return ""
	}
	return state.PromptTask
}

// ProcessEvent applies an event and transitions to the target state.
func (sm *StateMachine) ProcessEvent(event string) error {
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return fmt.Errorf("%w: state %q not found in spec", ErrInvalidEvent, sm.context.CurrentState)
	}

	if len(state.OnEvent) == 0 {
		return fmt.Errorf("%w: state %q has no transitions", ErrTerminalState, sm.context.CurrentState)
	}

	target, ok := state.OnEvent[event]
	if !ok {
		return fmt.Errorf("%w: event %q not defined for state %q (available: %v)",
			ErrInvalidEvent, event, sm.context.CurrentState, sm.AvailableEvents())
	}

	sm.context.RecordTransition(sm.context.CurrentState, target, event, sm.now())
	return nil
}

// IsTerminal returns true if the current state has no outgoing transitions.
func (sm *StateMachine) IsTerminal() bool {
	state := sm.spec.States[sm.context.CurrentState]
	if state == nil {
		return true
	}
	return len(state.OnEvent) == 0
}

// AvailableEvents returns the set of valid events for the current state, sorted.
func (sm *StateMachine) AvailableEvents() []string {
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
	return sm.context.Clone()
}
