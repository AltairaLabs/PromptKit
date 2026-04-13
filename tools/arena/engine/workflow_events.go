package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
)

// workflowArtifactExecutor dispatches workflow__set_artifact calls to the
// per-run state machine, keyed on the scenario ID carried in the context.
// Mirrors workflowTransitionExecutor's per-run isolation so concurrent
// scenarios don't stomp on each other's artifact maps.
type workflowArtifactExecutor struct {
	transExec *workflowTransitionExecutor
}

// Name implements tools.Executor.
func (a *workflowArtifactExecutor) Name() string { return workflow.ArtifactExecutorMode }

// Execute routes to the per-run ArtifactExecutor via the shared transition
// executor's run map.
func (a *workflowArtifactExecutor) Execute(
	ctx context.Context, desc *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	scenarioID := workflowScenarioIDFromCtx(ctx)
	a.transExec.mu.Lock()
	run := a.transExec.runs[scenarioID]
	a.transExec.mu.Unlock()
	if run == nil {
		return nil, fmt.Errorf("no active workflow run for scenario %q", scenarioID)
	}
	artExec := workflow.NewArtifactExecutor(run.transExec.StateMachine())
	return artExec.Execute(ctx, desc, args)
}

// emitWorkflowError emits the typed workflow error event (max_visits_exceeded
// or budget_exhausted) for a ProcessEvent / CommitPending error. No-op when
// emitter is nil or err doesn't match a known workflow error type.
func (e *workflowTransitionExecutor) emitWorkflowError(
	run *workflowRunState, emitter *events.Emitter, _ string, err error,
) {
	if emitter == nil || err == nil {
		return
	}
	var mvErr *workflow.MaxVisitsExceededError
	if errors.As(err, &mvErr) {
		emitter.WorkflowMaxVisitsExceeded(&events.WorkflowMaxVisitsExceededData{
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
		transitions := 0
		if run != nil {
			transitions = run.transExec.StateMachine().Context().TransitionCount()
		}
		emitter.WorkflowBudgetExhausted(&events.WorkflowBudgetExhaustedData{
			Limit:           bErr.Limit,
			Current:         bErr.Current,
			Max:             bErr.Max,
			CurrentState:    bErr.CurrentState,
			TransitionCount: transitions,
		})
		return
	}
}

// maxVisitsForWorkflowState returns the max_visits cap declared on a state
// in the given spec, or 0 if not set.
func maxVisitsForWorkflowState(spec *workflow.Spec, name string) int {
	if spec == nil {
		return 0
	}
	s := spec.States[name]
	if s == nil {
		return 0
	}
	return s.MaxVisits
}
