package assertions

import (
	"context"
	"fmt"
)

// WorkflowCompleteValidator checks that the workflow reached a terminal state.
// Params: (none required)
// Type: "workflow_complete"
type WorkflowCompleteValidator struct{}

// Type returns the validator type name.
func (v *WorkflowCompleteValidator) Type() string { return "workflow_complete" }

// NewWorkflowCompleteValidator constructs a new WorkflowCompleteValidator.
func NewWorkflowCompleteValidator() ConversationValidator {
	return &WorkflowCompleteValidator{}
}

// ValidateConversation checks whether the workflow is in a terminal state.
func (v *WorkflowCompleteValidator) ValidateConversation(
	_ context.Context,
	convCtx *ConversationContext,
	_ map[string]interface{},
) ConversationValidationResult {
	raw, ok := convCtx.Metadata.Extras["workflow_complete"]
	if !ok {
		return ConversationValidationResult{
			Passed:  false,
			Message: "workflow_complete: no workflow completion status in context",
		}
	}

	complete, ok := raw.(bool)
	if !ok {
		return ConversationValidationResult{
			Passed:  false,
			Message: "workflow_complete: invalid completion status type in context",
		}
	}

	if complete {
		state, _ := convCtx.Metadata.Extras["workflow_current_state"].(string)
		return ConversationValidationResult{
			Passed:  true,
			Message: fmt.Sprintf("workflow completed in terminal state %q", state),
		}
	}

	return ConversationValidationResult{
		Passed:  false,
		Message: "workflow has not reached a terminal state",
	}
}
