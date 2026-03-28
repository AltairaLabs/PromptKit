package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
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
	require.NoError(t, exec.CommitPendingTransition("test"))
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

	err = exec.CommitPendingTransition("test")
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
	require.NoError(t, exec.CommitPendingTransition("test"))

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
	require.NoError(t, exec.CommitPendingTransition("test"))

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
	require.NoError(t, exec.CommitPendingTransition("s1"))
	assert.Equal(t, "specialist", s1.TaskType)

	// s2 should still be able to Escalate (its own state machine)
	ctx2 := withWorkflowScenarioID(context.Background(), "s2")
	_, err = exec.Execute(ctx2, nil, args)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("s2"))
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
	require.NoError(t, exec.CommitPendingTransition("test"))
	assert.Equal(t, "loop", scenario.TaskType)

	// Second transition: loop -> loop, but max_visits=1 so redirect to done
	args2, _ := json.Marshal(map[string]string{"event": "Again", "context": "test"})
	_, err = exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args2)
	require.NoError(t, err)
	require.NoError(t, exec.CommitPendingTransition("test"))
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
