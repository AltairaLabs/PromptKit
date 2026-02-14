package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContainsHandler checks if CurrentOutput contains all specified
// patterns (case-insensitive). Params: patterns []string.
type ContainsHandler struct{}

// Type returns the eval type identifier.
func (h *ContainsHandler) Type() string { return "contains" }

// Eval checks that all patterns appear in the current output.
func (h *ContainsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no patterns specified",
		}, nil
	}

	contentLower := strings.ToLower(evalCtx.CurrentOutput)
	missing := findMissingPatterns(contentLower, patterns)

	passed := len(missing) == 0
	explanation := "all patterns found in output"
	if !passed {
		explanation = fmt.Sprintf(
			"missing patterns: %s",
			strings.Join(missing, ", "),
		)
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
	}, nil
}

// findMissingPatterns returns patterns not found in lowercased content.
func findMissingPatterns(
	contentLower string, patterns []string,
) []string {
	var missing []string
	for _, p := range patterns {
		if !strings.Contains(contentLower, strings.ToLower(p)) {
			missing = append(missing, p)
		}
	}
	return missing
}
