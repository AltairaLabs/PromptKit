package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
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
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgMissingState,
		}, nil
	}

	raw, ok := evalCtx.Extras["workflow_transitions"]
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgNoTransitions,
		}, nil
	}

	transitions, ok := raw.([]any)
	if !ok {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: msgInvalidTransitions,
		}, nil
	}

	for _, t := range transitions {
		tr, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if to, _ := tr["to"].(string); to == target {
			return &evals.EvalResult{
				Type:        h.Type(),
				Score:       boolScore(true),
				Value:       map[string]any{keyTarget: target, keyFound: true, keyTransition: tr},
				Explanation: fmt.Sprintf("workflow transitioned to %q", target),
				Details:     map[string]any{keyTransition: tr},
			}, nil
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(false),
		Value:       map[string]any{keyTarget: target, keyFound: false, keyTransitions: transitions},
		Explanation: fmt.Sprintf("no transition to state %q found", target),
		Details:     map[string]any{keyTarget: target, keyTransitions: transitions},
	}, nil
}
