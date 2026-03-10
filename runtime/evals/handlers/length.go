package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// MinLengthHandler checks that CurrentOutput has at least the specified character count.
// Accepts params: min or min_characters (int).
type MinLengthHandler struct{}

// Type returns the eval type identifier.
func (h *MinLengthHandler) Type() string { return "min_length" }

// Eval checks that the output meets the minimum length requirement.
func (h *MinLengthHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	minLen := extractInt(params, "min", 0)
	if minLen == 0 {
		minLen = extractInt(params, "min_characters", 0)
	}
	if minLen == 0 {
		minLen = extractInt(params, "min_chars", 0)
	}
	actual := len(evalCtx.CurrentOutput)
	passed := actual >= minLen

	var score *float64
	if minLen > 0 {
		s := float64(actual) / float64(minLen)
		if s > 1.0 {
			s = 1.0
		}
		score = scorePtr(s)
	} else {
		score = scorePtr(1.0)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Score:       score,
		Value:       map[string]any{"length": actual, "min": minLen},
		Explanation: fmt.Sprintf("length %d, min %d", actual, minLen),
	}, nil
}

// MaxLengthHandler checks that CurrentOutput does not exceed the specified character count.
// Accepts params: max or max_characters (int).
type MaxLengthHandler struct{}

// Type returns the eval type identifier.
func (h *MaxLengthHandler) Type() string { return "max_length" }

// Eval checks that the output does not exceed the maximum length.
func (h *MaxLengthHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	maxLen := extractInt(params, "max", 0)
	if maxLen == 0 {
		maxLen = extractInt(params, "max_characters", 0)
	}
	if maxLen == 0 {
		maxLen = extractInt(params, "max_chars", 0)
	}
	if maxLen == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "missing or zero 'max'/'max_characters' param",
		}, nil
	}

	actual := len(evalCtx.CurrentOutput)
	passed := actual <= maxLen

	var score *float64
	if actual > 0 {
		s := float64(maxLen) / float64(actual)
		if s > 1.0 {
			s = 1.0
		}
		score = scorePtr(s)
	} else {
		score = scorePtr(1.0)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Score:       score,
		Value:       map[string]any{"length": actual, "max": maxLen},
		Explanation: fmt.Sprintf("length %d, max %d", actual, maxLen),
	}, nil
}
