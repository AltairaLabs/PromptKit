package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testWorkflowSpec() *workflow.Spec {
	return &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask: "intake",
				OnEvent:    map[string]string{"Escalate": "specialist", "Resolve": "closed"},
			},
			"specialist": {
				PromptTask: "specialist",
				OnEvent:    map[string]string{"Resolve": "closed"},
			},
			"closed": {
				PromptTask: "closed",
			},
		},
	}
}

func TestWorkflowTransitionExecutor_Execute(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test", TaskType: "intake"}
	exec.RegisterRun("test", scenario)

	// Execute stores pending (deferred)
	args, _ := json.Marshal(map[string]string{"event": "Escalate", "context": "test"})
	result, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)

	var res map[string]string
	require.NoError(t, json.Unmarshal(result, &res))
	assert.Equal(t, "transition_scheduled", res["status"])

	// TaskType not yet updated (deferred)
	assert.Equal(t, "intake", scenario.TaskType)

	// Commit applies the transition
	require.NoError(t, exec.CommitPendingTransition("test", nil))
	assert.Equal(t, "specialist", scenario.TaskType)
}

func TestWorkflowTransitionExecutor_InvalidEvent(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)

	// Execute succeeds (stores pending), commit fails
	args, _ := json.Marshal(map[string]string{"event": "NonExistent"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err) // Execute always succeeds (just stores pending)

	err = exec.CommitPendingTransition("test", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NonExistent")
}

func TestWorkflowTransitionExecutor_WorkflowMetadata(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)

	// Initial state
	meta := exec.RunMetadata("test")
	assert.Equal(t, "intake", meta["workflow_current_state"])
	assert.Equal(t, false, meta["workflow_complete"])

	// After transition (execute + commit)
	args, _ := json.Marshal(map[string]string{"event": "Escalate"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("test", nil))

	meta = exec.RunMetadata("test")
	assert.Equal(t, "specialist", meta["workflow_current_state"])
	assert.Equal(t, false, meta["workflow_complete"])
	transitions := meta["workflow_transitions"].([]any)
	require.Len(t, transitions, 1)
	tr := transitions[0].(map[string]any)
	assert.Equal(t, "intake", tr["from"])
	assert.Equal(t, "specialist", tr["to"])
}

func TestWorkflowTransitionExecutor_TerminalState(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)

	args, _ := json.Marshal(map[string]string{"event": "Resolve"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("test", nil))

	meta := exec.RunMetadata("test")
	assert.Equal(t, true, meta["workflow_complete"])
}

func TestWorkflowTransitionExecutor_ConcurrentRuns(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	s1 := &config.Scenario{ID: "s1", TaskType: "intake"}
	s2 := &config.Scenario{ID: "s2", TaskType: "intake"}
	exec.RegisterRun("s1", s1)
	exec.RegisterRun("s2", s2)

	// Transition s1 to specialist (using s1's context)
	ctx1 := withWorkflowScenarioID(context.Background(), "s1")
	args, _ := json.Marshal(map[string]string{"event": "Escalate"})
	_, err := exec.Execute(ctx1, nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("s1", nil))
	assert.Equal(t, "specialist", s1.TaskType)

	// s2 should still be able to Escalate (its own state machine)
	ctx2 := withWorkflowScenarioID(context.Background(), "s2")
	_, err = exec.Execute(ctx2, nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("s2", nil))
	assert.Equal(t, "specialist", s2.TaskType)
}

func TestWorkflowTransitionExecutor_MaxVisitsRedirect(t *testing.T) {
	spec := &workflow.Spec{
		Version: 2,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {
				PromptTask: "start",
				OnEvent:    map[string]string{"Go": "loop"},
			},
			"loop": {
				PromptTask:  "loop",
				MaxVisits:   1,
				OnMaxVisits: "done",
				OnEvent:     map[string]string{"Again": "loop"},
			},
			"done": {PromptTask: "done"},
		},
	}
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test", TaskType: "start"}
	exec.RegisterRun("test", scenario)

	// First transition: start -> loop (visit 1)
	args, _ := json.Marshal(map[string]string{"event": "Go", "context": "test"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("test", nil))
	assert.Equal(t, "loop", scenario.TaskType)

	// Second transition: loop -> loop, but max_visits=1 so redirect to done
	args2, _ := json.Marshal(map[string]string{"event": "Again", "context": "test"})
	_, err = exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args2)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("test", nil))
	assert.Equal(t, "done", scenario.TaskType, "should redirect to on_max_visits target")

	// Verify redirect info in transitions metadata
	meta := exec.RunMetadata("test")
	transitions := meta["workflow_transitions"].([]any)
	lastTransition := transitions[len(transitions)-1].(map[string]any)
	assert.Equal(t, true, lastTransition["redirected"])
	assert.Contains(t, lastTransition["redirect_reason"].(string), "max_visits")
}

func TestRegisterTransitionTool(t *testing.T) {
	registry := tools.NewRegistry()
	state := &workflow.State{
		OnEvent: map[string]string{"Escalate": "specialist", "Resolve": "closed"},
	}

	registerTransitionTool(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	require.NotNil(t, tool)
	assert.Equal(t, workflow.TransitionExecutorMode, tool.Mode)
}

func TestRegisterTransitionTool_TerminalState(t *testing.T) {
	registry := tools.NewRegistry()
	state := &workflow.State{} // no events = terminal

	registerTransitionTool(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool)
}

type mockSkillFilterer struct {
	lastFilter string
}

func (m *mockSkillFilterer) SetFilter(glob string) []string {
	m.lastFilter = glob
	return nil
}

func TestWorkflowTransitionExecutor_Name(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)
	assert.Equal(t, workflow.TransitionExecutorMode, exec.Name())
}

func TestWorkflowTransitionExecutor_StateMachine(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	// No run registered
	assert.Nil(t, exec.StateMachine("nonexistent"))

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)
	sm := exec.StateMachine("test")
	assert.NotNil(t, sm)
	assert.Equal(t, "intake", sm.CurrentState())
}

func TestWorkflowTransitionExecutor_UnregisterRun(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)
	assert.NotNil(t, exec.RunMetadata("test"))

	exec.UnregisterRun("test")
	assert.Nil(t, exec.RunMetadata("test"))
}

func TestWorkflowRunMetadataProvider(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)

	provider := &workflowRunMetadataProvider{exec: exec, scenarioID: "test"}
	meta := provider.WorkflowMetadata()
	assert.Equal(t, "intake", meta["workflow_current_state"])
}

func TestCommitPendingTransition_SetsSkillFilter(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask: "intake",
				OnEvent:    map[string]string{"RouteBilling": "billing"},
			},
			"billing": {
				PromptTask: "billing",
				Skills:     "skills/billing/*",
			},
		},
	}

	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test", TaskType: "intake"}
	exec.RegisterRun("run1", scenario)

	// Execute a transition
	args, _ := json.Marshal(map[string]string{"event": "RouteBilling"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "run1"), nil, args)
	require.NoError(t, err)

	// Commit should store the skill filter on the per-run state
	err = exec.CommitPendingTransition("run1", nil)
	require.NoError(t, err)
	assert.Equal(t, "skills/billing/*", exec.SkillFilter("run1"))
}

func TestCommitPendingTransition_NilSkillFilterer(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask: "intake",
				OnEvent:    map[string]string{"RouteBilling": "billing"},
			},
			"billing": {
				PromptTask: "billing",
				Skills:     "skills/billing/*",
			},
		},
	}

	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)
	// No skillFilterer set — should not panic

	scenario := &config.Scenario{ID: "test", TaskType: "intake"}
	exec.RegisterRun("run1", scenario)

	args, _ := json.Marshal(map[string]string{"event": "RouteBilling"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "run1"), nil, args)
	require.NoError(t, err)

	err = exec.CommitPendingTransition("run1", nil)
	require.NoError(t, err)
}

// TestWorkflowArtifactExecutor_DispatchesToRun verifies the per-run artifact
// executor mutates the right state machine's artifact map.
func TestWorkflowArtifactExecutor_DispatchesToRun(t *testing.T) {
	spec := &workflow.Spec{
		Version: 2,
		Entry:   "s",
		States: map[string]*workflow.State{
			"s": {
				PromptTask: "s",
				Artifacts: map[string]*workflow.ArtifactDef{
					"notes": {Type: "text/plain", Mode: "append"},
				},
			},
		},
	}
	registry := tools.NewRegistry()
	transExec := newWorkflowTransitionExecutor(spec, registry)
	transExec.RegisterRun("r1", &config.Scenario{ID: "r1"})
	transExec.RegisterRun("r2", &config.Scenario{ID: "r2"})

	artExec := &workflowArtifactExecutor{transExec: transExec}
	assert.Equal(t, workflow.ArtifactExecutorMode, artExec.Name())

	args, _ := json.Marshal(map[string]string{"name": "notes", "value": "first"})
	_, err := artExec.Execute(withWorkflowScenarioID(context.Background(), "r1"), nil, args)
	require.NoError(t, err)

	args2, _ := json.Marshal(map[string]string{"name": "notes", "value": "second"})
	_, err = artExec.Execute(withWorkflowScenarioID(context.Background(), "r1"), nil, args2)
	require.NoError(t, err)

	// r1's artifact has both appended values; r2's artifact is untouched.
	r1Notes := transExec.runs["r1"].transExec.StateMachine().Artifacts()["notes"]
	r2Notes := transExec.runs["r2"].transExec.StateMachine().Artifacts()["notes"]
	assert.Contains(t, r1Notes, "first")
	assert.Contains(t, r1Notes, "second")
	assert.Empty(t, r2Notes)

	// Unknown scenario errors out.
	_, err = artExec.Execute(withWorkflowScenarioID(context.Background(), "missing"), nil, args)
	assert.Error(t, err)
}

// TestEmitWorkflowError_Arena verifies the arena's emitWorkflowError
// helper fires the right typed event for each workflow error type.
func TestEmitWorkflowError_Arena(t *testing.T) {
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(testWorkflowSpec(), registry)
	exec.RegisterRun("r", &config.Scenario{ID: "r"})

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")

	mvCh := make(chan *events.Event, 1)
	bgCh := make(chan *events.Event, 1)
	bus.Subscribe(events.EventWorkflowMaxVisitsExceeded, func(e *events.Event) { mvCh <- e })
	bus.Subscribe(events.EventWorkflowBudgetExhausted, func(e *events.Event) { bgCh <- e })

	run := exec.runs["r"]
	exec.emitWorkflowError(run, emitter, "Go", &workflow.MaxVisitsExceededError{
		FromState: "a", OriginalTarget: "b", Event: "Go", VisitCount: 3, MaxVisits: 3,
	})
	exec.emitWorkflowError(run, emitter, "Go", &workflow.BudgetExhaustedError{
		Limit: workflow.BudgetLimitWallTimeSec, Current: 60, Max: 60, CurrentState: "a",
	})
	// Nil-safety: nil emitter, nil error, unmatched error — all no-ops.
	exec.emitWorkflowError(run, nil, "", &workflow.MaxVisitsExceededError{})
	exec.emitWorkflowError(run, emitter, "", nil)
	exec.emitWorkflowError(run, emitter, "", assert.AnError)

	select {
	case e := <-mvCh:
		data := e.Data.(*events.WorkflowMaxVisitsExceededData)
		assert.Equal(t, "b", data.OriginalTarget)
		assert.True(t, data.Terminated)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for max_visits_exceeded")
	}
	select {
	case e := <-bgCh:
		data := e.Data.(*events.WorkflowBudgetExhaustedData)
		assert.Equal(t, workflow.BudgetLimitWallTimeSec, data.Limit)
		assert.Equal(t, 60, data.Max)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for budget_exhausted")
	}
}

// TestCommitPendingTransition_EmitsRedirectedEvent verifies that a commit
// that redirects (max_visits cap hit with on_max_visits fallback) emits
// workflow.max_visits_exceeded alongside workflow.transitioned.
func TestCommitPendingTransition_EmitsRedirectedEvent(t *testing.T) {
	spec := &workflow.Spec{
		Version: 2,
		Entry:   "loop",
		States: map[string]*workflow.State{
			"loop": {
				PromptTask:  "loop",
				MaxVisits:   2,
				OnMaxVisits: "exit",
				OnEvent:     map[string]string{"Again": "loop"},
			},
			"exit": {PromptTask: "exit"},
		},
	}
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "r", TaskType: "loop"}
	exec.RegisterRun("r", scenario)

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")

	received := make(chan *events.Event, 4)
	bus.Subscribe(events.EventWorkflowMaxVisitsExceeded, func(e *events.Event) {
		received <- e
	})

	// First transition: 0 visits → no redirect, visits[loop]=1
	args, _ := json.Marshal(map[string]string{"event": "Again"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "r"), nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("r", emitter))

	// Second transition: visits[loop]=1 == MaxVisits, should redirect to exit.
	_, err = exec.Execute(withWorkflowScenarioID(context.Background(), "r"), nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("r", emitter))

	select {
	case e := <-received:
		data, ok := e.Data.(*events.WorkflowMaxVisitsExceededData)
		require.True(t, ok)
		assert.Equal(t, "loop", data.OriginalTarget)
		assert.Equal(t, "exit", data.RedirectedTo)
		assert.False(t, data.Terminated)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for workflow.max_visits_exceeded")
	}
}
