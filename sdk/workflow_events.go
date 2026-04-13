package sdk

import (
	"errors"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// emitTransitionEvents emits the workflow.transitioned event for a successful
// transition, plus workflow.max_visits_exceeded when the transition was a
// max_visits redirect, plus workflow.completed when the new state is terminal.
// Safe to call with a nil emitter (no-op).
func (wc *WorkflowConversation) emitTransitionEvents(
	result *workflow.TransitionResult, toState, promptName string,
) {
	if wc.emitter == nil {
		return
	}
	if result.Redirected {
		wc.emitter.WorkflowMaxVisitsExceeded(&events.WorkflowMaxVisitsExceededData{
			FromState:      result.From,
			OriginalTarget: result.OriginalTarget,
			Event:          result.Event,
			VisitCount:     wc.machine.Context().VisitCounts[result.OriginalTarget],
			MaxVisits:      maxVisitsForState(wc.workflowSpec, result.OriginalTarget),
			RedirectedTo:   result.To,
			Terminated:     false,
		})
	}
	wc.emitter.WorkflowTransitioned(result.From, toState, result.Event, promptName)
	if wc.machine.IsTerminal() {
		wc.emitter.WorkflowCompleted(toState, wc.machine.Context().TransitionCount())
	}
}

// maxVisitsForState returns the max_visits cap declared on the named state,
// or 0 if the state is unknown or has no cap. Used for populating
// observability event fields; safe with nil spec.
func maxVisitsForState(spec *workflow.Spec, name string) int {
	if spec == nil {
		return 0
	}
	s := spec.States[name]
	if s == nil {
		return 0
	}
	return s.MaxVisits
}

// emitWorkflowError emits a typed observability event for errors returned
// from ProcessEvent / CommitPending. Non-fatal: a nil emitter or an error
// that doesn't match a known workflow error type is a no-op. Called from
// every caller that consumes ProcessEvent errors so the event fires exactly
// once per termination regardless of which transition path triggered it.
func (wc *WorkflowConversation) emitWorkflowError(_ string, err error) {
	if wc.emitter == nil || err == nil {
		return
	}
	var mvErr *workflow.MaxVisitsExceededError
	if errors.As(err, &mvErr) {
		wc.emitter.WorkflowMaxVisitsExceeded(&events.WorkflowMaxVisitsExceededData{
			FromState:      mvErr.FromState,
			OriginalTarget: mvErr.OriginalTarget,
			Event:          mvErr.Event,
			VisitCount:     mvErr.VisitCount,
			MaxVisits:      mvErr.MaxVisits,
			Terminated:     true,
		})
		return
	}
	var bErr *workflow.BudgetExhaustedError
	if errors.As(err, &bErr) {
		wc.emitter.WorkflowBudgetExhausted(&events.WorkflowBudgetExhaustedData{
			Limit:           bErr.Limit,
			Current:         bErr.Current,
			Max:             bErr.Max,
			CurrentState:    bErr.CurrentState,
			TransitionCount: wc.machine.Context().TransitionCount(),
		})
		return
	}
}
