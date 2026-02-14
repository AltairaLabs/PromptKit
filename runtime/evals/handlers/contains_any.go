package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContainsAnyHandler checks that at least one assistant message
// contains at least one of the specified patterns.
// Params: patterns []string (case-insensitive matching).
type ContainsAnyHandler struct{}

// Type returns the eval type identifier.
func (h *ContainsAnyHandler) Type() string {
	return "contains_any"
}

// Eval checks assistant messages for any matching pattern.
func (h *ContainsAnyHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no patterns specified",
		}, nil
	}

	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		for _, p := range patterns {
			if containsInsensitive(content, p) {
				return &evals.EvalResult{
					Type:   h.Type(),
					Passed: true,
					Explanation: fmt.Sprintf(
						"turn %d contains pattern %q",
						i, p,
					),
				}, nil
			}
		}
	}

	return &evals.EvalResult{
		Type:   h.Type(),
		Passed: false,
		Explanation: fmt.Sprintf(
			"no assistant message contained any of: %s",
			strings.Join(patterns, ", "),
		),
	}, nil
}
