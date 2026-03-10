package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// DirectionalHandler compares a run's output against a baseline expectation.
// Used in behavioral testing (Phase 6) to verify perturbation-invariant behavior.
// Params:
//   - check (string, required): "same_tool_calls", "same_outcome", or "similar_content"
//   - baseline_tools ([]string, optional): expected tool names for same_tool_calls
//   - baseline_state (string, optional): expected workflow state for same_outcome
//   - baseline_content (string, optional): expected content substring for similar_content
//   - threshold (float64, optional): minimum overlap ratio for similar_content (default 0.5)
type DirectionalHandler struct{}

// Type returns the eval type identifier.
func (h *DirectionalHandler) Type() string { return "directional" }

// Eval checks that the run's output matches the baseline for the given check type.
func (h *DirectionalHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	check, _ := params["check"].(string)
	if check == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "missing required param 'check'",
		}, nil
	}

	switch check {
	case "same_tool_calls":
		return h.checkSameToolCalls(evalCtx, params)
	case "same_outcome":
		return h.checkSameOutcome(evalCtx, params)
	case "similar_content":
		return h.checkSimilarContent(evalCtx, params)
	default:
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("unknown check %q; must be same_tool_calls, same_outcome, or similar_content", check),
		}, nil
	}
}

func (h *DirectionalHandler) checkSameToolCalls(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	tools := extractStringSlice(params, "baseline_tools")
	if len(tools) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no baseline_tools specified; skipping comparison",
		}, nil
	}
	return compareToolSets(h.Type(), evalCtx, tools, "baseline"), nil
}

func (h *DirectionalHandler) checkSameOutcome(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	state, _ := params["baseline_state"].(string)
	if state == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no baseline_state specified; skipping comparison",
		}, nil
	}
	return compareWorkflowState(h.Type(), evalCtx, state, "baseline"), nil
}

func (h *DirectionalHandler) checkSimilarContent(
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	baselineContent, _ := params["baseline_content"].(string)
	if baselineContent == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no baseline_content specified; skipping comparison",
		}, nil
	}

	threshold := 0.5
	if t, ok := extractFloat64(params, "threshold"); ok {
		threshold = t
	}

	actual := evalCtx.CurrentOutput
	score := wordOverlap(actual, baselineContent)
	passed := score >= threshold

	result := &evals.EvalResult{
		Type:   h.Type(),
		Passed: passed,
		Score:  &score,
		Value:  map[string]any{"overlap_score": score, "threshold": threshold},
		Details: map[string]any{
			"overlap_score": score,
			"threshold":     threshold,
		},
	}

	if passed {
		result.Explanation = fmt.Sprintf("content similarity %.2f >= threshold %.2f", score, threshold)
	} else {
		result.Explanation = fmt.Sprintf("content similarity %.2f < threshold %.2f", score, threshold)
	}

	return result, nil
}

// wordOverlap computes the Jaccard similarity of word sets between two strings.
func wordOverlap(a, b string) float64 {
	wordsA := wordSet(a)
	wordsB := wordSet(b)

	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return 0.0
	}

	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA)
	for w := range wordsB {
		if !wordsA[w] {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// wordSet splits a string into lowercase words and returns the unique set.
func wordSet(s string) map[string]bool {
	words := strings.Fields(strings.ToLower(s))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		set[w] = true
	}
	return set
}
