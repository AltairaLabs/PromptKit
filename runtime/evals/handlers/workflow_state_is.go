package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// WorkflowStateIsHandler checks that the current workflow state matches an expected value.
// Params: state string (required).
type WorkflowStateIsHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowStateIsHandler) Type() string { return "state_is" }

// Eval checks if the current workflow state matches the expected value.
func (h *WorkflowStateIsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	expected, _ := params["state"].(string)
	if expected == "" {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "missing required param 'state'",
		}, nil
	}

	actual, ok := evalCtx.Extras["workflow_current_state"].(string)
	if !ok {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no workflow state available in context",
		}, nil
	}

	if actual == expected {
		return &evals.EvalResult{
			Type: h.Type(), Passed: true,
			Explanation: fmt.Sprintf("workflow is in expected state %q", expected),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Explanation: fmt.Sprintf("expected state %q but got %q", expected, actual),
		Details:     map[string]any{"expected": expected, "actual": actual},
	}, nil
}
