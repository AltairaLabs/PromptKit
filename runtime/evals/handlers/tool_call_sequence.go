package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolCallSequenceHandler checks that tool calls appear in a specified subsequence order.
// Params: sequence []string — the expected tool names in order.
type ToolCallSequenceHandler struct{}

// Type returns the eval type identifier.
func (h *ToolCallSequenceHandler) Type() string { return "tool_call_sequence" }

// Eval checks subsequence ordering of tool calls.
func (h *ToolCallSequenceHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	sequence := extractStringSlice(params, "sequence")

	if len(sequence) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "empty sequence always passes",
		}, nil
	}

	matched, actualTools := coreToolCallSequence(views, sequence)

	if matched < len(sequence) {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"sequence incomplete: matched %d/%d, missing %q",
				matched, len(sequence), sequence[matched],
			),
			Details: map[string]any{
				"matched_steps": matched,
				"total_steps":   len(sequence),
				"actual_tools":  actualTools,
			},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: fmt.Sprintf("sequence [%s] fully matched", strings.Join(sequence, " → ")),
		Details: map[string]any{
			"matched_steps": matched,
			"actual_tools":  actualTools,
		},
	}, nil
}
