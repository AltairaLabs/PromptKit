package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

const skillActivateToolName = "skill__activate"

// SkillActivatedHandler checks that specific skills were activated.
// Scans evalCtx.ToolCalls for "skill__activate" calls and extracts the "name" argument.
// Params: skill_names []string, min_calls int (optional, default 1).
type SkillActivatedHandler struct{}

// Type returns the eval type identifier.
func (h *SkillActivatedHandler) Type() string { return "skill_activated" }

// Eval checks that required skills were activated at least the minimum number of times.
func (h *SkillActivatedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	required := extractStringSlice(params, "skill_names")
	minCalls := extractInt(params, "min_calls", 1)

	counts := countSkillCalls(evalCtx.ToolCalls)

	var missing []string
	for _, name := range required {
		if counts[name] < minCalls {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("missing skill activations: %s", strings.Join(missing, ", ")),
			Details:     map[string]any{"counts": counts, "min_calls": minCalls},
		}, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Explanation: "all required skills were activated",
		Details:     map[string]any{"counts": counts},
	}, nil
}

// countSkillCalls counts skill activations from skill__activate tool calls.
func countSkillCalls(toolCalls []evals.ToolCallRecord) map[string]int {
	counts := make(map[string]int)
	for _, tc := range toolCalls {
		if tc.ToolName != skillActivateToolName {
			continue
		}
		if tc.Arguments == nil {
			continue
		}
		if name, ok := tc.Arguments["name"].(string); ok && name != "" {
			counts[name]++
		}
	}
	return counts
}
