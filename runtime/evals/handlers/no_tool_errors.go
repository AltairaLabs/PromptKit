package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// NoToolErrorsHandler checks that no tool calls returned errors.
// Params: tools []string (optional) — if set, only checks calls matching those tool names.
type NoToolErrorsHandler struct{}

// Type returns the eval type identifier.
func (h *NoToolErrorsHandler) Type() string { return "no_tool_errors" }

// Eval checks for tool errors in the eval context's tool calls.
func (h *NoToolErrorsHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	tools := extractStringSlice(params, "tools")

	errors := coreNoToolErrors(views, tools)

	totalCalls := len(views)
	errorCount := len(errors)

	if errorCount > 0 {
		names := make([]string, errorCount)
		for i, e := range errors {
			names[i] = fmt.Sprintf("%s: %s", e["tool"], e["error"])
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Score:       inverseRatioScore(errorCount, totalCalls),
			Value:       map[string]any{"error_count": errorCount, "total_calls": totalCalls},
			Explanation: fmt.Sprintf("tool errors found: %s", strings.Join(names, "; ")),
			Details:     map[string]any{"errors": errors},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       boolScore(true),
		Value:       map[string]any{"error_count": 0, "total_calls": totalCalls},
		Explanation: "no tool errors found",
	}, nil
}
