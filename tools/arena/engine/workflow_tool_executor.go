package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
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
	scenario    *config.Scenario
	transitions []map[string]any
	skillFilter string // current skill glob filter for this run
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

// RegisterRun creates a fresh TransitionExecutor for a scenario run.
func (e *workflowTransitionExecutor) RegisterRun(runID string, scenario *config.Scenario) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sm := workflow.NewStateMachine(e.wfSpec)
	e.runs[runID] = &workflowRunState{
		transExec: workflow.NewTransitionExecutor(sm, e.wfSpec),
		scenario:  scenario,
	}
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

// CommitPendingTransition commits the deferred transition for a run.
// Called after the turn/pipeline completes. Updates scenario TaskType and
// re-registers the transition tool for the new state's events.
func (e *workflowTransitionExecutor) CommitPendingTransition(runID string) error {
	e.mu.Lock()
	run := e.runs[runID]
	e.mu.Unlock()

	if run == nil || run.transExec.Pending() == nil {
		return nil
	}

	tr, err := run.transExec.CommitPending()
	if err != nil {
		return fmt.Errorf("transition commit failed: %w", err)
	}
	if tr == nil {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

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

	// Update scenario TaskType and re-register tool for the new state
	if newState := e.wfSpec.States[tr.To]; newState != nil {
		if run.scenario != nil {
			run.scenario.TaskType = newState.PromptTask
		}
		run.transExec.RegisterForState(e.registry, newState)

		// Store skill filter for this run (applied via context, not globally)
		run.skillFilter = newState.Skills
	}

	return nil
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
type workflowRunMetadataProvider struct {
	exec       *workflowTransitionExecutor
	scenarioID string
}

// WorkflowMetadata returns metadata for the specific run.
func (p *workflowRunMetadataProvider) WorkflowMetadata() map[string]any {
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
