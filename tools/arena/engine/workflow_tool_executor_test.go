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

	// Execute Escalate transition
	args, _ := json.Marshal(map[string]string{"event": "Escalate", "context": "test"})
	result, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)

	var res struct {
		NewState string `json:"new_state"`
		Event    string `json:"event"`
	}
	require.NoError(t, json.Unmarshal(result, &res))
	assert.Equal(t, "specialist", res.NewState)
	assert.Equal(t, "Escalate", res.Event)

	// Verify scenario TaskType was updated
	assert.Equal(t, "specialist", scenario.TaskType)
}

func TestWorkflowTransitionExecutor_InvalidEvent(t *testing.T) {
	spec := testWorkflowSpec()
	registry := tools.NewRegistry()
	exec := newWorkflowTransitionExecutor(spec, registry)

	scenario := &config.Scenario{ID: "test"}
	exec.RegisterRun("test", scenario)

	args, _ := json.Marshal(map[string]string{"event": "NonExistent"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
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

	// After transition
	args, _ := json.Marshal(map[string]string{"event": "Escalate"})
	_, err := exec.Execute(withWorkflowScenarioID(context.Background(), "test"), nil, args)
	require.NoError(t, err)

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
	assert.Equal(t, "specialist", s1.TaskType)

	// s2 should still be able to Escalate (its own state machine)
	ctx2 := withWorkflowScenarioID(context.Background(), "s2")
	_, err = exec.Execute(ctx2, nil, args)
	require.NoError(t, err)
	assert.Equal(t, "specialist", s2.TaskType)
}

func TestRegisterTransitionTool(t *testing.T) {
	registry := tools.NewRegistry()
	state := &workflow.State{
		OnEvent: map[string]string{"Escalate": "specialist", "Resolve": "closed"},
	}

	registerTransitionTool(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	require.NotNil(t, tool)
	assert.Equal(t, workflowTransitionMode, tool.Mode)
}

func TestRegisterTransitionTool_TerminalState(t *testing.T) {
	registry := tools.NewRegistry()
	state := &workflow.State{} // no events = terminal

	registerTransitionTool(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool)
}
