package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
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
			Score:       inverseRatioScore(errorCount, totalCalls),
			Value:       map[string]any{keyErrorCount: errorCount, keyTotalCalls: totalCalls},
			Explanation: fmt.Sprintf("tool errors found: %s", strings.Join(names, "; ")),
			Details:     map[string]any{keyErrors: errors},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       map[string]any{keyErrorCount: 0, keyTotalCalls: totalCalls},
		Explanation: "no tool errors found",
	}, nil
}
