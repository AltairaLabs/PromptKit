package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// LLMJudgeToolCallsHandler evaluates tool call behavior via an LLM judge.
// Instead of judging the assistant's text response, it feeds tool call data
// (names, arguments, results) to the judge for evaluation.
// Params:
//   - tools []string (optional): filter to specific tool names
//   - criteria string: what to evaluate
//   - rubric string (optional): detailed scoring guidance
//   - model string (optional): model override
//   - min_score float64 (optional): minimum score to pass
type LLMJudgeToolCallsHandler struct{}

// Type returns the eval type identifier.
func (h *LLMJudgeToolCallsHandler) Type() string { return "llm_judge_tool_calls" }

// Eval runs the LLM judge on formatted tool call data.
func (h *LLMJudgeToolCallsHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: extractErr.Error(),
		}, nil
	}

	filtered := filterToolCallViews(evalCtx.ToolCalls, extractStringSlice(params, "tools"))
	if len(filtered) == 0 {
		return &evals.EvalResult{
			Type: h.Type(), Passed: true,
			Explanation: "no matching tool calls to judge",
			Skipped:     true,
			SkipReason:  "no matching tool calls",
		}, nil
	}

	opts := buildJudgeOpts(formatToolCallViews(filtered), params)
	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: fmt.Sprintf("judge error: %v", judgeErr),
		}, nil
	}

	result := buildEvalResult(h.Type(), judgeResult, params)
	result.Details = map[string]any{
		"tool_calls_sent": len(filtered),
	}
	return result, nil
}

// filterToolCallViews converts records to views and optionally filters by tool names.
func filterToolCallViews(records []evals.ToolCallRecord, toolNames []string) []toolCallView {
	views := viewsFromRecords(records)
	if len(toolNames) == 0 {
		return views
	}
	toolSet := make(map[string]bool, len(toolNames))
	for _, t := range toolNames {
		toolSet[t] = true
	}
	var filtered []toolCallView
	for _, v := range views {
		if toolSet[v.Name] {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// formatToolCallViews formats tool call views as structured text for the judge prompt.
func formatToolCallViews(calls []toolCallView) string {
	var b strings.Builder
	for i, tc := range calls {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "TOOL CALL %d (index %d):\n", i+1, tc.Index)
		fmt.Fprintf(&b, "  Tool: %s\n", tc.Name)

		argsJSON, err := json.Marshal(tc.Args)
		if err != nil {
			fmt.Fprintf(&b, "  Arguments: %v\n", tc.Args)
		} else {
			fmt.Fprintf(&b, "  Arguments: %s\n", string(argsJSON))
		}

		if tc.Result != "" {
			fmt.Fprintf(&b, "  Result: %s\n", tc.Result)
		} else {
			b.WriteString("  Result: (none)\n")
		}

		if tc.Error != "" {
			fmt.Fprintf(&b, "  Error: %s\n", tc.Error)
		} else {
			b.WriteString("  Error: (none)\n")
		}
	}
	return b.String()
}
