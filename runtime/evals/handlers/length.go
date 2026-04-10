package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// Compile-time interface check for streaming support.
var _ evals.StreamableEvalHandler = (*MaxLengthHandler)(nil)

// charsPerToken is the estimated number of characters per token,
// matching the guardrail estimation logic.
const charsPerToken = 4

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
			Score:       boolScore(false),
			Explanation: "missing or zero 'max'/'max_characters' param",
		}, nil
	}

	actual := len(evalCtx.CurrentOutput)

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

	value := map[string]any{"length": actual, "max": maxLen}
	explanation := fmt.Sprintf("length %d, max %d", actual, maxLen)

	// Check max_tokens if specified
	maxTokens := extractInt(params, "max_tokens", 0)
	if maxTokens > 0 {
		tokenCount := actual / charsPerToken
		value["tokens"] = tokenCount
		value["max_tokens"] = maxTokens
		if tokenCount > maxTokens {
			tokenScore := float64(maxTokens) / float64(tokenCount)
			if score == nil || tokenScore < *score {
				score = scorePtr(tokenScore)
			}
			explanation = fmt.Sprintf(
				"length %d, max %d; tokens ~%d, max_tokens %d",
				actual, maxLen, tokenCount, maxTokens,
			)
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       score,
		Value:       value,
		Explanation: explanation,
	}, nil
}

// EvalPartial checks partial streaming content against max length limits.
// This enables early abort when streaming content exceeds the limit.
func (h *MaxLengthHandler) EvalPartial(
	_ context.Context, content string, params map[string]any,
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
			Type:  h.Type(),
			Score: scorePtr(1.0),
		}, nil
	}

	actual := len(content)

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

	value := map[string]any{"length": actual, "max": maxLen}

	// Check max_tokens if specified
	maxTokens := extractInt(params, "max_tokens", 0)
	if maxTokens > 0 {
		tokenCount := actual / charsPerToken
		value["tokens"] = tokenCount
		value["max_tokens"] = maxTokens
		if tokenCount > maxTokens {
			tokenScore := float64(maxTokens) / float64(tokenCount)
			if score == nil || tokenScore < *score {
				score = scorePtr(tokenScore)
			}
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       score,
		Value:       value,
		Explanation: fmt.Sprintf("stream length %d, max %d", actual, maxLen),
	}, nil
}

// ValidateParams checks that at least one of the canonical or aliased
// max-length keys is set to a positive integer. Called at guardrail
// hook construction and at eval preflight so invalid pack validators
// fail loudly at load time instead of at request time.
func (h *MaxLengthHandler) ValidateParams(params map[string]any) error {
	maxLen := extractInt(params, "max", 0)
	if maxLen == 0 {
		maxLen = extractInt(params, "max_characters", 0)
	}
	if maxLen == 0 {
		maxLen = extractInt(params, "max_chars", 0)
	}
	if maxLen <= 0 {
		return fmt.Errorf(
			"%s requires one of 'max', 'max_characters', or 'max_chars' to be a positive integer",
			h.Type())
	}
	return nil
}

// ValidateParams checks that at least one of the canonical or aliased
// min-length keys is set to a positive integer.
func (h *MinLengthHandler) ValidateParams(params map[string]any) error {
	minLen := extractInt(params, "min", 0)
	if minLen == 0 {
		minLen = extractInt(params, "min_characters", 0)
	}
	if minLen == 0 {
		minLen = extractInt(params, "min_chars", 0)
	}
	if minLen <= 0 {
		return fmt.Errorf(
			"%s requires one of 'min', 'min_characters', or 'min_chars' to be a positive integer",
			h.Type())
	}
	return nil
}
