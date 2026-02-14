package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ContentExcludesHandler checks that NONE of the assistant messages
// across the full conversation contain any of the forbidden patterns.
// Params: patterns []string (case-insensitive matching).
type ContentExcludesHandler struct{}

// Type returns the eval type identifier.
func (h *ContentExcludesHandler) Type() string {
	return "content_excludes"
}

// Eval checks all assistant messages for forbidden patterns.
func (h *ContentExcludesHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	patterns := extractStringSlice(params, "patterns")
	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no patterns to check",
		}, nil
	}

	var found []string
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		if !strings.EqualFold(msg.Role, roleAssistant) {
			continue
		}
		content := msg.GetContent()
		for _, p := range patterns {
			if containsInsensitive(content, p) {
				found = append(found, fmt.Sprintf(
					"turn %d contains %q", i, p,
				))
			}
		}
	}

	if len(found) > 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"forbidden content found: %s",
				strings.Join(found, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "no forbidden content detected",
	}, nil
}
