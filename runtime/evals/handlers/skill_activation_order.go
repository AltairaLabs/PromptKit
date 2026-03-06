package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// SkillActivationOrderHandler checks that skills were activated in a specified subsequence order.
// Params: sequence []string — the expected skill names in order.
type SkillActivationOrderHandler struct{}

// Type returns the eval type identifier.
func (h *SkillActivationOrderHandler) Type() string { return "skill_activation_order" }

// Eval checks that the expected sequence appears as a subsequence of the actual skill activations.
func (h *SkillActivationOrderHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	sequence := extractStringSlice(params, "sequence")

	if len(sequence) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "sequence param is required and must be non-empty",
		}, nil
	}

	activated := extractActivatedSkills(evalCtx.ToolCalls)

	if len(activated) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no skill activations found",
			Details:     map[string]any{"expected_sequence": sequence, "activated_skills": activated},
		}, nil
	}

	matched := matchSkillSubsequence(activated, sequence)

	if matched < len(sequence) {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"sequence incomplete: matched %d/%d, missing %q",
				matched, len(sequence), sequence[matched],
			),
			Details: map[string]any{
				"matched_steps":    matched,
				"total_steps":      len(sequence),
				"activated_skills": activated,
			},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: fmt.Sprintf("skill activation sequence [%s] fully matched", strings.Join(sequence, " → ")),
		Details: map[string]any{
			"matched_steps":    matched,
			"activated_skills": activated,
		},
	}, nil
}

// extractActivatedSkills builds an ordered list of skill names from skill__activate tool calls.
func extractActivatedSkills(toolCalls []evals.ToolCallRecord) []string {
	var skills []string
	for _, tc := range toolCalls {
		if tc.ToolName != skillActivateToolName {
			continue
		}
		if tc.Arguments == nil {
			continue
		}
		if name, ok := tc.Arguments["name"].(string); ok && name != "" {
			skills = append(skills, name)
		}
	}
	return skills
}

// matchSkillSubsequence checks how many elements of sequence appear as a subsequence in activated.
func matchSkillSubsequence(activated, sequence []string) int {
	seqIdx := 0
	for _, skill := range activated {
		if seqIdx >= len(sequence) {
			break
		}
		if skill == sequence[seqIdx] {
			seqIdx++
		}
	}
	return seqIdx
}
