package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowCapability_Name(t *testing.T) {
	cap := NewWorkflowCapability()
	assert.Equal(t, "workflow", cap.Name())
}

func TestWorkflowCapability_Init(t *testing.T) {
	cap := NewWorkflowCapability()
	p := &pack.Pack{
		Workflow: &pack.WorkflowSpec{
			Version: 1,
			Entry:   "start",
		},
	}
	err := cap.Init(CapabilityContext{Pack: p, PromptName: "test"})
	require.NoError(t, err)
	assert.Equal(t, p.Workflow, cap.workflowSpec)
}

func TestWorkflowCapability_Close(t *testing.T) {
	cap := NewWorkflowCapability()
	assert.NoError(t, cap.Close())
}

func TestRegisterToolsForState_InternalOrchestration(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	state := &workflow.State{
		PromptTask: "gather_info",
		OnEvent: map[string]string{
			"Escalate": "escalation",
			"Resolve":  "resolved",
		},
		Orchestration: workflow.OrchestrationInternal,
	}

	cap.RegisterToolsForState(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	require.NotNil(t, tool, "workflow__transition tool should be registered")
	assert.Equal(t, workflow.TransitionToolName, tool.Name)
	assert.Equal(t, "workflow", tool.Namespace)
}

func TestRegisterToolsForState_HybridOrchestration(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	state := &workflow.State{
		PromptTask: "gather_info",
		OnEvent: map[string]string{
			"Escalate": "escalation",
		},
		Orchestration: workflow.OrchestrationHybrid,
	}

	cap.RegisterToolsForState(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	assert.NotNil(t, tool, "tool should be registered for hybrid orchestration")
}

func TestRegisterToolsForState_ExternalOrchestration(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	state := &workflow.State{
		PromptTask: "gather_info",
		OnEvent: map[string]string{
			"Escalate": "escalation",
		},
		Orchestration: workflow.OrchestrationExternal,
	}

	cap.RegisterToolsForState(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool, "tool should NOT be registered for external orchestration")
}

func TestRegisterToolsForState_TerminalState(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	state := &workflow.State{
		PromptTask: "confirm",
		// No OnEvent = terminal state
	}

	cap.RegisterToolsForState(registry, state)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool, "tool should NOT be registered for terminal state")
}

func TestRegisterToolsForState_NilState(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	// Should not panic
	cap.RegisterToolsForState(registry, nil)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool)
}

func TestRegisterTools_IsNoOp(t *testing.T) {
	cap := NewWorkflowCapability()
	registry := tools.NewRegistry()

	// RegisterTools at conversation level is a no-op
	cap.RegisterTools(registry)

	tool := registry.Get(workflow.TransitionToolName)
	assert.Nil(t, tool, "RegisterTools should not register any tools")
}
