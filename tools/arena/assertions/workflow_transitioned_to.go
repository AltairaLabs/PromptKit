package assertions

import (
	"context"
	"fmt"
)

// WorkflowTransitionedToValidator checks that a transition to a specific state occurred.
// Params:
//   - state: string (required) — the target state to look for in the transition history
//
// Type: "transitioned_to"
type WorkflowTransitionedToValidator struct{}

// Type returns the validator type name.
func (v *WorkflowTransitionedToValidator) Type() string { return "transitioned_to" }

// NewWorkflowTransitionedToValidator constructs a new WorkflowTransitionedToValidator.
func NewWorkflowTransitionedToValidator() ConversationValidator {
	return &WorkflowTransitionedToValidator{}
}

// ValidateConversation checks if the workflow transitioned to the specified state.
func (v *WorkflowTransitionedToValidator) ValidateConversation(
	_ context.Context,
	convCtx *ConversationContext,
	params map[string]interface{},
) ConversationValidationResult {
	target, _ := params["state"].(string)
	if target == "" {
		return ConversationValidationResult{
			Passed:  false,
			Message: "transitioned_to: missing required param 'state'",
		}
	}

	raw, ok := convCtx.Metadata.Extras["workflow_transitions"]
	if !ok {
		return ConversationValidationResult{
			Passed:  false,
			Message: "transitioned_to: no workflow transitions available in context",
		}
	}

	transitions, ok := raw.([]interface{})
	if !ok {
		return ConversationValidationResult{
			Passed:  false,
			Message: "transitioned_to: invalid transitions data in context",
		}
	}

	for _, t := range transitions {
		tr, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		if to, _ := tr["to"].(string); to == target {
			return ConversationValidationResult{
				Passed:  true,
				Message: fmt.Sprintf("workflow transitioned to %q", target),
				Details: map[string]interface{}{
					"transition": tr,
				},
			}
		}
	}

	return ConversationValidationResult{
		Passed:  false,
		Message: fmt.Sprintf("no transition to state %q found", target),
		Details: map[string]interface{}{
			"target":      target,
			"transitions": transitions,
		},
	}
}
