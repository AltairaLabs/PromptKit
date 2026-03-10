package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// OutcomeEquivalentHandler checks that a single run's outcome matches an expected value.
// Used in behavioral testing (Phase 6) to verify perturbation-invariant outcomes.
// Params:
//   - metric (string, required): "tool_calls", "final_state", or "content_hash"
//   - expected_tools ([]string, optional): for tool_calls metric
//   - expected_state (string, optional): for final_state metric
//   - expected_content (string, optional): for content_hash metric
type OutcomeEquivalentHandler struct{}

// Type returns the eval type identifier.
func (h *OutcomeEquivalentHandler) Type() string { return "outcome_equivalent" }

// Eval checks that the run's outcome matches the expected value for the given metric.
func (h *OutcomeEquivalentHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	metric, _ := params["metric"].(string)
	if metric == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "missing required param 'metric'",
		}, nil
	}

	switch metric {
	case "tool_calls":
		return h.evalToolCalls(evalCtx, params)
	case "final_state":
		return h.evalFinalState(evalCtx, params)
	case "content_hash":
		return h.evalContentHash(evalCtx, params)
	default:
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("unknown metric %q; must be tool_calls, final_state, or content_hash", metric),
		}, nil
	}
}

func (h *OutcomeEquivalentHandler) evalToolCalls(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	tools := extractStringSlice(params, "expected_tools")
	if len(tools) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no expected_tools specified; skipping single-run comparison",
		}, nil
	}
	return compareToolSets(h.Type(), evalCtx, tools, "expected"), nil
}

func (h *OutcomeEquivalentHandler) evalFinalState(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	state, _ := params["expected_state"].(string)
	if state == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no expected_state specified; skipping single-run comparison",
		}, nil
	}
	return compareWorkflowState(h.Type(), evalCtx, state, "expected"), nil
}

func (h *OutcomeEquivalentHandler) evalContentHash(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	expectedContent, _ := params["expected_content"].(string)
	if expectedContent == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no expected_content specified; skipping single-run comparison",
		}, nil
	}

	actual := evalCtx.CurrentOutput
	matched := actual == expectedContent

	if matched {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Score:       boolScore(true),
			Explanation: "content matches expected output",
			Value:       map[string]any{"matched": true, "length": len(actual)},
			Details:     map[string]any{"length": len(actual)},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      false,
		Score:       boolScore(false),
		Explanation: "content does not match expected output",
		Value:       map[string]any{"matched": false, "actual_length": len(actual), "expected_length": len(expectedContent)},
		Details:     map[string]any{"actual_length": len(actual), "expected_length": len(expectedContent)},
	}, nil
}
