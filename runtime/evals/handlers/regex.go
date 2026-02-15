package handlers

import (
	"context"
	"fmt"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// RegexHandler checks if CurrentOutput matches a regex pattern.
// Params: pattern string.
type RegexHandler struct{}

// Type returns the eval type identifier.
func (h *RegexHandler) Type() string { return "regex" }

// Eval checks that the current output matches the regex pattern.
func (h *RegexHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	patternStr, _ := params["pattern"].(string)
	if patternStr == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no pattern specified",
		}, nil
	}

	re, compileErr := regexp.Compile(patternStr)
	if compileErr != nil {
		return &evals.EvalResult{
			Type:  h.Type(),
			Error: fmt.Sprintf("invalid regex: %v", compileErr),
		}, nil
	}

	matched := re.MatchString(evalCtx.CurrentOutput)

	// expect_match (default true): when false, the eval passes if the
	// pattern does NOT match — useful for "must not contain" checks.
	expectMatch := true
	if v, ok := params["expect_match"].(bool); ok {
		expectMatch = v
	}

	passed := matched == expectMatch
	explanation := fmt.Sprintf(
		"pattern %q matched: %t (expect_match: %t)", patternStr, matched, expectMatch,
	)

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Explanation: explanation,
	}, nil
}
