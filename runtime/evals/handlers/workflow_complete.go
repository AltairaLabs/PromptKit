package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// WorkflowCompleteHandler checks that the workflow reached a terminal state.
// Reads evalCtx.Extras["workflow_complete"] (bool) and ["workflow_current_state"] (string).
type WorkflowCompleteHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowCompleteHandler) Type() string { return "workflow_complete" }

// Eval checks whether the workflow is in a terminal state.
func (h *WorkflowCompleteHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	_ map[string]any,
) (*evals.EvalResult, error) {
	raw, ok := evalCtx.Extras["workflow_complete"]
	if !ok {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no workflow completion status in context",
		}, nil
	}

	complete, ok := raw.(bool)
	if !ok {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "invalid workflow completion status type",
		}, nil
	}

	if complete {
		state, _ := evalCtx.Extras["workflow_current_state"].(string)
		return &evals.EvalResult{
			Type: h.Type(), Passed: true,
			Explanation: fmt.Sprintf("workflow completed in terminal state %q", state),
		}, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: false,
		Explanation: "workflow has not reached a terminal state",
	}, nil
}
