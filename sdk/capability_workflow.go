package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// WorkflowCapability provides the workflow__transition tool for LLM-initiated
// state transitions.
type WorkflowCapability struct {
	workflowSpec *pack.WorkflowSpec
}

// NewWorkflowCapability creates a new WorkflowCapability.
func NewWorkflowCapability() *WorkflowCapability {
	return &WorkflowCapability{}
}

// Name returns the capability identifier.
func (w *WorkflowCapability) Name() string { return "workflow" }

// Init stores the workflow spec from the pack for later tool registration.
func (w *WorkflowCapability) Init(ctx CapabilityContext) error {
	w.workflowSpec = ctx.Pack.Workflow
	return nil
}

// RegisterTools is a no-op at conversation level.
// WorkflowConversation calls RegisterToolsForState per-state.
func (w *WorkflowCapability) RegisterTools(_ *tools.Registry) {}

// RegisterToolsForState registers workflow__transition for a specific state.
// Called by WorkflowConversation when opening a conversation for a state.
func (w *WorkflowCapability) RegisterToolsForState(
	registry *tools.Registry, state *workflow.State,
) {
	if state == nil || len(state.OnEvent) == 0 {
		return // terminal state
	}
	if state.Orchestration == workflow.OrchestrationExternal {
		return // caller drives transitions
	}
	events := workflow.SortedEvents(state.OnEvent)
	desc := workflow.BuildTransitionToolDescriptor(events)
	_ = registry.Register(desc)
}

// Close is a no-op for WorkflowCapability.
func (w *WorkflowCapability) Close() error { return nil }
