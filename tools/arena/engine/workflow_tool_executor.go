package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

// SkillFilterer controls which skills are available based on workflow state.
type SkillFilterer interface {
	SetFilter(glob string) []string
}

type workflowScenarioIDKey struct{}

// withWorkflowScenarioID stores the workflow scenario ID in context for per-run dispatch.
func withWorkflowScenarioID(ctx context.Context, scenarioID string) context.Context {
	return context.WithValue(ctx, workflowScenarioIDKey{}, scenarioID)
}

// workflowScenarioIDFromCtx retrieves the workflow scenario ID from context.
func workflowScenarioIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(workflowScenarioIDKey{}).(string); ok {
		return v
	}
	return ""
}

// workflowRunState holds per-run workflow state for concurrent scenario execution.
type workflowRunState struct {
	transExec   *workflow.TransitionExecutor
	scenario    *arenaconfig.Scenario
	transitions []map[string]any
	skillFilter string // current skill glob filter for this run
	emitter     *events.Emitter
	// compositionRecorder captures per-step outputs for RFC 0010 testability; nil for non-composition runs.
	compositionRecorder *stage.CompositionRecorder
}

// workflowTransitionExecutor routes workflow__transition tool calls to per-run
// TransitionExecutors. Each scenario run gets its own TransitionExecutor via
// RegisterRun. The executor defers ProcessEvent until CommitPendingTransition
// is called after the turn/pipeline completes.
type workflowTransitionExecutor struct {
	mu            sync.Mutex
	wfSpec        *workflow.Spec
	registry      *tools.Registry
	runs          map[string]*workflowRunState // keyed by scenario ID
	skillFilterer SkillFilterer
}

func newWorkflowTransitionExecutor(
	wfSpec *workflow.Spec,
	registry *tools.Registry,
) *workflowTransitionExecutor {
	return &workflowTransitionExecutor{
		wfSpec:   wfSpec,
		registry: registry,
		runs:     make(map[string]*workflowRunState),
	}
}

// Name implements tools.Executor.
func (e *workflowTransitionExecutor) Name() string { return workflow.TransitionExecutorMode }

// RegisterRun creates a fresh TransitionExecutor for a scenario run with no
// observability emitter. Use RegisterRunWithEmitter when events should be
// emitted on commits.
func (e *workflowTransitionExecutor) RegisterRun(runID string, scenario *arenaconfig.Scenario) {
	e.RegisterRunWithEmitter(runID, scenario, nil)
}

// RegisterRunWithEmitter creates a fresh TransitionExecutor for a scenario run
// and captures the per-run emitter.
//
// The emitter is used by the OnCommit hook wired here, which fires when the
// deferred transition commits via CommitPendingTransition. Pass nil for runs
// that don't need observability events.
func (e *workflowTransitionExecutor) RegisterRunWithEmitter(
	runID string, scenario *arenaconfig.Scenario, emitter *events.Emitter,
) {
	e.RegisterRunAtState(runID, scenario, emitter, e.wfSpec.Entry)
}

// RegisterRunAtState is like RegisterRunWithEmitter but starts the per-run state
// machine at startState instead of the workflow entry. Used to pin a scenario to
// a single stage for unit testing. Passing the entry is equivalent to
// RegisterRunWithEmitter.
func (e *workflowTransitionExecutor) RegisterRunAtState(
	runID string, scenario *arenaconfig.Scenario, emitter *events.Emitter, startState string,
) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sm := workflow.NewStateMachineFromContext(e.wfSpec, workflow.NewContext(startState, time.Now()))
	transExec := workflow.NewTransitionExecutor(sm, e.wfSpec)
	run := &workflowRunState{
		transExec: transExec,
		scenario:  scenario,
		emitter:   emitter,
	}
	e.runs[runID] = run
	transExec.SetOnCommit(func(tr *workflow.TransitionResult) {
		e.applyPostCommit(runID, tr)
	})
	// OnCommitError fires on deferred ProcessEvent failures, so
	// max_visits_exceeded / budget_exhausted events emit when a commit trips.
	transExec.SetOnCommitError(func(event string, err error) {
		e.mu.Lock()
		run := e.runs[runID]
		e.mu.Unlock()
		if run == nil {
			return
		}
		e.emitWorkflowError(run, run.emitter, event, err)
	})
}

// Execute routes the tool call to the per-run TransitionExecutor.
// The executor defers ProcessEvent — call CommitPendingTransition after the turn.
func (e *workflowTransitionExecutor) Execute(
	ctx context.Context, desc *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	scenarioID := workflowScenarioIDFromCtx(ctx)

	e.mu.Lock()
	run := e.runs[scenarioID]
	e.mu.Unlock()

	if run == nil {
		return nil, fmt.Errorf("no active workflow run for scenario %q", scenarioID)
	}

	return run.transExec.Execute(ctx, desc, args)
}

// CommitPendingTransition commits any pending deferred transition for a run.
// Called from the PostTurnHook after the pipeline turn completes and from
// WorkflowMetadata reads inside the pipeline. Returns nil and is a no-op when
// nothing is pending.
//
// Post-commit work (transition history, observability events, scenario
// TaskType update, tool re-registration, skill filter) runs via the OnCommit
// hook wired in RegisterRunWithEmitter. OnCommitError handles the
// failure-path observability symmetrically.
//
// The emitter parameter is kept for source compatibility with older callers
// but is no longer consulted — the per-run emitter captured at registration
// is used for both success and failure events.
func (e *workflowTransitionExecutor) CommitPendingTransition(
	runID string, _ *events.Emitter,
) error {
	e.mu.Lock()
	run := e.runs[runID]
	e.mu.Unlock()

	if run == nil || run.transExec.Pending() == nil {
		return nil
	}

	if _, err := run.transExec.CommitPending(); err != nil {
		// OnCommitError already emitted the observability event; just wrap
		// the underlying error for the caller.
		return fmt.Errorf("transition commit failed: %w", err)
	}
	return nil
}

// applyPostCommit runs the work for every successful commit: record the
// transition, log it, fire observability events, update scenario TaskType,
// re-register the transition tool for the new state's events, and store the
// new state's skill filter.
//
// Wired into the runtime TransitionExecutor's OnCommit hook from RegisterRun.
func (e *workflowTransitionExecutor) applyPostCommit(
	runID string, tr *workflow.TransitionResult,
) {
	if tr == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	run := e.runs[runID]
	if run == nil {
		return
	}

	transitionRecord := map[string]any{
		"from": tr.From, "to": tr.To, "event": tr.Event,
	}
	if tr.Redirected {
		transitionRecord["redirected"] = true
		transitionRecord["redirect_reason"] = tr.RedirectReason
		transitionRecord["original_target"] = tr.OriginalTarget
	}
	run.transitions = append(run.transitions, transitionRecord)
	logger.Info("workflow state transition", "from", tr.From, "to", tr.To,
		"event", tr.Event, "redirected", tr.Redirected)

	if run.emitter != nil {
		if tr.Redirected {
			run.emitter.WorkflowMaxVisitsExceeded(&events.WorkflowMaxVisitsExceededData{
				FromState:      tr.From,
				OriginalTarget: tr.OriginalTarget,
				Event:          tr.Event,
				VisitCount:     run.transExec.StateMachine().Context().VisitCounts[tr.OriginalTarget],
				MaxVisits:      maxVisitsForWorkflowState(e.wfSpec, tr.OriginalTarget),
				RedirectedTo:   tr.To,
				Terminated:     false,
			})
		}
		run.emitter.WorkflowTransitioned(tr.From, tr.To, tr.Event, "")
		if newState := e.wfSpec.States[tr.To]; newState != nil &&
			(newState.Terminal || len(newState.OnEvent) == 0) {
			run.emitter.WorkflowCompleted(
				tr.To, run.transExec.StateMachine().Context().TransitionCount())
		}
	}

	if newState := e.wfSpec.States[tr.To]; newState != nil {
		if run.scenario != nil {
			run.scenario.TaskType = newState.PromptTask
		}
		// Re-register the transition tool for the new state's events, but never
		// unregister it: e.registry is shared across every scenario run, so the
		// unregister that runtime's RegisterForState performs on terminal-state
		// entry would strip workflow__transition from sibling runs that still
		// need it (issue #1480). registerTransitionTool no-ops on terminal
		// states, leaving the tool registered for other runs.
		registerTransitionTool(e.registry, newState)
		run.skillFilter = newState.Skills
	}
}

// StateMachine returns the per-run state machine for direct access (e.g., metadata).
func (e *workflowTransitionExecutor) StateMachine(runID string) *workflow.StateMachine {
	e.mu.Lock()
	defer e.mu.Unlock()
	if run := e.runs[runID]; run != nil {
		return run.transExec.StateMachine()
	}
	return nil
}

// RunMetadata returns workflow metadata for a specific run.
func (e *workflowTransitionExecutor) RunMetadata(runID string) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	run := e.runs[runID]
	return buildRunMetadata(run)
}

// SkillFilter returns the current skill filter for a specific run.
func (e *workflowTransitionExecutor) SkillFilter(runID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if run := e.runs[runID]; run != nil {
		return run.skillFilter
	}
	return ""
}

// SetCompositionRecorder stores the per-run composition recorder on the run state.
// Called from prepareWorkflowScenario after the run is registered.
func (e *workflowTransitionExecutor) SetCompositionRecorder(runID string, rec *stage.CompositionRecorder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if run := e.runs[runID]; run != nil {
		run.compositionRecorder = rec
	}
}

// CompositionRecorder returns the per-run composition recorder, or nil if not set.
func (e *workflowTransitionExecutor) CompositionRecorder(runID string) *stage.CompositionRecorder {
	e.mu.Lock()
	defer e.mu.Unlock()
	if run := e.runs[runID]; run != nil {
		return run.compositionRecorder
	}
	return nil
}

// UnregisterRun removes a completed run's state to prevent unbounded map growth.
func (e *workflowTransitionExecutor) UnregisterRun(runID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.runs, runID)
}

func buildRunMetadata(run *workflowRunState) map[string]any {
	if run == nil {
		return nil
	}
	sm := run.transExec.StateMachine()
	transitions := make([]any, len(run.transitions))
	for i, t := range run.transitions {
		transitions[i] = t
	}
	return map[string]any{
		"workflow_current_state": sm.CurrentState(),
		"workflow_complete":      sm.IsTerminal(),
		"workflow_transitions":   transitions,
	}
}

// workflowRunMetadataProvider wraps the executor for a specific scenario run.
//
// emitter is wired per-run when the provider is created so any commit
// that fires from WorkflowMetadata() emits the same observability events
// that a post-turn-hook commit would emit.
type workflowRunMetadataProvider struct {
	exec       *workflowTransitionExecutor
	scenarioID string
	emitter    *events.Emitter
}

// WorkflowMetadata returns metadata for the specific run.
//
// If a transition was queued earlier in this turn (the LLM called
// workflow__transition and the executor deferred the commit), this
// commits it eagerly before returning the metadata. Per-turn workflow
// assertions (state_is, transitioned_to, workflow_complete) need to
// see the result of the agent's just-fired transition; without this,
// they observe pre-commit state because the assertion stage runs
// inside the pipeline while the standard post-turn commit hook fires
// after the pipeline returns.
//
// CommitPendingTransition is a no-op when nothing is pending, so the
// post-turn hook still runs harmlessly. Errors during commit are
// non-fatal here — the post-turn hook will surface the same error on
// its retry — but they are logged so silent metadata-time failures
// don't mask a real workflow problem.
func (p *workflowRunMetadataProvider) WorkflowMetadata() map[string]any {
	if err := p.exec.CommitPendingTransition(p.scenarioID, p.emitter); err != nil {
		logger.Warn("workflow commit at metadata read time failed",
			"scenario_id", p.scenarioID, "error", err)
	}
	return p.exec.RunMetadata(p.scenarioID)
}

// registerTransitionTool registers the workflow__transition tool with the executor
// routing mode set so the registry dispatches to workflowTransitionExecutor.
func registerTransitionTool(registry *tools.Registry, state *workflow.State) {
	if registry == nil || state == nil || state.Terminal || len(state.OnEvent) == 0 {
		return
	}
	if state.Orchestration == workflow.OrchestrationExternal {
		return
	}
	evts := workflow.SortedEvents(state.OnEvent)
	desc := workflow.BuildTransitionToolDescriptor(evts)
	desc.Mode = workflow.TransitionExecutorMode
	_ = registry.Register(desc)
}
