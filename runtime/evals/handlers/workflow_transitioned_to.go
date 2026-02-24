package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// WorkflowTransitionedToHandler checks that a transition to a specific state occurred.
// Params: state string (required).
type WorkflowTransitionedToHandler struct{}

// Type returns the eval type identifier.
func (h *WorkflowTransitionedToHandler) Type() string { return "transitioned_to" }

// Eval checks if the workflow transitioned to the specified state.
func (h *WorkflowTransitionedToHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	target, _ := params["state"].(string)
	if target == "" {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "missing required param 'state'",
		}, nil
	}

	raw, ok := evalCtx.Extras["workflow_transitions"]
	if !ok {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "no workflow transitions available in context",
		}, nil
	}

	transitions, ok := raw.([]any)
	if !ok {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "invalid transitions data in context",
		}, nil
	}

	for _, t := range transitions {
		tr, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if to, _ := tr["to"].(string); to == target {
			return &evals.EvalResult{
				Type: h.Type(), Passed: true,
				Explanation: fmt.Sprintf("workflow transitioned to %q", target),
				Details:     map[string]any{"transition": tr},
			}, nil
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Explanation: fmt.Sprintf("no transition to state %q found", target),
		Details:     map[string]any{"target": target, "transitions": transitions},
	}, nil
}
