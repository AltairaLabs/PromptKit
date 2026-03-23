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

// workflowTransitionMode is the executor name and tool Mode value for routing.
const workflowTransitionMode = "workflow-transition"

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
	sm          *workflow.StateMachine
	scenario    *config.Scenario
	transitions []map[string]any
}

// workflowTransitionExecutor implements tools.Executor for the workflow__transition tool.
// It supports concurrent scenario execution by maintaining per-run state keyed by
// a run token set on the request metadata.
type workflowTransitionExecutor struct {
	mu       sync.Mutex
	wfSpec   *workflow.Spec
	registry *tools.Registry
	runs     map[string]*workflowRunState // keyed by scenario ID
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
func (e *workflowTransitionExecutor) Name() string { return workflowTransitionMode }

// RegisterRun creates a fresh state machine for a scenario run.
// Called from prepareWorkflowScenario before execution starts.
func (e *workflowTransitionExecutor) RegisterRun(runID string, scenario *config.Scenario) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs[runID] = &workflowRunState{
		sm:       workflow.NewStateMachine(e.wfSpec),
		scenario: scenario,
	}
}

// Execute processes a workflow__transition tool call from the LLM.
func (e *workflowTransitionExecutor) Execute(
	ctx context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	var a struct {
		Event   string `json:"event"`
		Context string `json:"context"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("failed to parse transition args: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Find the run for this scenario using context-propagated scenario ID
	scenarioID := workflowScenarioIDFromCtx(ctx)
	run := e.runs[scenarioID]
	if run == nil {
		return nil, fmt.Errorf("no active workflow run for transition event %q", a.Event)
	}

	from := run.sm.CurrentState()
	if err := run.sm.ProcessEvent(a.Event); err != nil {
		return nil, fmt.Errorf("transition event %q failed: %w", a.Event, err)
	}
	to := run.sm.CurrentState()

	run.transitions = append(run.transitions, map[string]any{
		"from": from, "to": to, "event": a.Event,
	})
	logger.Info("workflow state transition", "from", from, "to", to, "event", a.Event)

	// Update scenario TaskType and re-register tool for the new state
	if newState := e.wfSpec.States[to]; newState != nil {
		if run.scenario != nil {
			run.scenario.TaskType = newState.PromptTask
		}
		registerTransitionTool(e.registry, newState)
	}

	result, _ := json.Marshal(struct {
		NewState string `json:"new_state"`
		Event    string `json:"event"`
	}{NewState: to, Event: a.Event})
	return result, nil
}

// RunMetadata returns workflow metadata for a specific run.
func (e *workflowTransitionExecutor) RunMetadata(runID string) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	run := e.runs[runID]
	return buildRunMetadata(run)
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
	transitions := make([]any, len(run.transitions))
	for i, t := range run.transitions {
		transitions[i] = t
	}
	return map[string]any{
		"workflow_current_state": run.sm.CurrentState(),
		"workflow_complete":      run.sm.IsTerminal(),
		"workflow_transitions":   transitions,
	}
}

// workflowRunMetadataProvider wraps the executor for a specific scenario run.
// This allows per-run metadata to be returned to the eval orchestrator even when
// multiple scenarios run concurrently.
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
	if registry == nil || state == nil || len(state.OnEvent) == 0 {
		return
	}
	if state.Orchestration == workflow.OrchestrationExternal {
		return
	}
	evts := workflow.SortedEvents(state.OnEvent)
	desc := workflow.BuildTransitionToolDescriptor(evts)
	desc.Mode = workflowTransitionMode
	_ = registry.Register(desc)
}
