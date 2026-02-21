package pack

// WorkflowSpec is the SDK's mirror of runtime/workflow.WorkflowSpec.
// It is kept as a separate type to avoid coupling the SDK's internal package
// graph to runtime/workflow, following the same pattern as other Pack fields.
type WorkflowSpec struct {
	Version int                       `json:"version"`
	Entry   string                    `json:"entry"`
	States  map[string]*WorkflowState `json:"states"`
	Engine  map[string]any            `json:"engine,omitempty"`
}

// WorkflowState is the SDK's mirror of runtime/workflow.WorkflowState.
type WorkflowState struct {
	PromptTask    string            `json:"prompt_task"`
	Description   string            `json:"description,omitempty"`
	OnEvent       map[string]string `json:"on_event,omitempty"`
	Persistence   string            `json:"persistence,omitempty"`
	Orchestration string            `json:"orchestration,omitempty"`
	Skills        string            `json:"skills,omitempty"`
}
