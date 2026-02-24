package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// SkillNotActivatedHandler checks that specific skills were NOT activated.
// Params: skill_names []string.
type SkillNotActivatedHandler struct{}

// Type returns the eval type identifier.
func (h *SkillNotActivatedHandler) Type() string { return "skill_not_activated" }

// Eval ensures forbidden skills were never activated.
func (h *SkillNotActivatedHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	forbidden := extractStringSlice(params, "skill_names")
	forbiddenSet := make(map[string]struct{}, len(forbidden))
	for _, n := range forbidden {
		forbiddenSet[n] = struct{}{}
	}

	var violations []evals.EvalViolation
	for _, tc := range evalCtx.ToolCalls {
		if tc.ToolName != skillActivateToolName {
			continue
		}
		if tc.Arguments == nil {
			continue
		}
		skillName, _ := tc.Arguments["name"].(string)
		if _, bad := forbiddenSet[skillName]; bad {
			violations = append(violations, evals.EvalViolation{
				TurnIndex:   tc.TurnIndex,
				Description: fmt.Sprintf("forbidden skill %q was activated", skillName),
				Evidence:    map[string]any{"skill": skillName},
			})
		}
	}

	if len(violations) > 0 {
		names := make([]string, len(violations))
		for i, v := range violations {
			names[i] = v.Evidence["skill"].(string)
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: fmt.Sprintf("forbidden skills activated: %s", strings.Join(names, ", ")),
			Violations:  violations,
		}, nil
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Explanation: "no forbidden skills were activated",
	}, nil
}
