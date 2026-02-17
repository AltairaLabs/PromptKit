package assertions

import (
	"context"
	"fmt"
)

// WorkflowStateIsValidator checks that the current workflow state matches an expected value.
// Params:
//   - state: string (required) — the expected state name
//
// Type: "state_is"
type WorkflowStateIsValidator struct{}

// Type returns the validator type name.
func (v *WorkflowStateIsValidator) Type() string { return "state_is" }

// NewWorkflowStateIsValidator constructs a new WorkflowStateIsValidator.
func NewWorkflowStateIsValidator() ConversationValidator {
	return &WorkflowStateIsValidator{}
}

// ValidateConversation checks if the current workflow state matches the expected value.
func (v *WorkflowStateIsValidator) ValidateConversation(
	_ context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	expected, _ := params["state"].(string)
	if expected == "" {
		return ConversationValidationResult{
			Passed:  false,
			Message: "state_is: missing required param 'state'",
		}
	}

	actual, ok := convCtx.Metadata.Extras["workflow_current_state"].(string)
	if !ok {
		return ConversationValidationResult{
			Passed:  false,
			Message: "state_is: no workflow state available in context",
		}
	}

	if actual == expected {
		return ConversationValidationResult{
			Passed:  true,
			Message: fmt.Sprintf("workflow is in expected state %q", expected),
		}
	}

	return ConversationValidationResult{
		Passed:  false,
		Message: fmt.Sprintf("expected state %q but got %q", expected, actual),
		Details: map[string]interface{}{
			"expected": expected,
			"actual":   actual,
		},
	}
}
