package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

const defaultMaxRepeats = 1

// ToolNoRepeatHandler detects consecutive repeated calls to the same tool.
// Params:
//   - tools: []string — tool names to check (empty = all tools)
//   - max_repeats: int — maximum allowed consecutive calls to the same tool (default 1)
type ToolNoRepeatHandler struct{}

// Type returns the eval type identifier.
func (h *ToolNoRepeatHandler) Type() string { return "tool_no_repeat" }

// Eval checks for consecutive repeated tool calls.
func (h *ToolNoRepeatHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	tools := extractStringSlice(params, "tools")
	maxRepeats := defaultMaxRepeats
	if v := extractIntPtr(params, "max_repeats"); v != nil {
		maxRepeats = *v
	}

	toolScope := make(map[string]bool, len(tools))
	for _, t := range tools {
		toolScope[t] = true
	}

	var violations []map[string]any
	consecutive := 1
	for i := 1; i < len(views); i++ {
		if views[i].Name == views[i-1].Name {
			consecutive++
		} else {
			consecutive = 1
		}
		if consecutive > maxRepeats {
			name := views[i].Name
			if len(toolScope) > 0 && !toolScope[name] {
				continue
			}
			violations = append(violations, map[string]any{
				"tool":        name,
				"consecutive": consecutive,
				"at_index":    views[i].Index,
			})
		}
	}

	if len(violations) > 0 {
		names := make([]string, len(violations))
		for i, v := range violations {
			names[i] = fmt.Sprintf("%s(%dx)", v["tool"], v["consecutive"])
		}
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Score:  boolScore(false),
			Explanation: fmt.Sprintf(
				"repeated tool calls detected (max %d): %s",
				maxRepeats, strings.Join(names, ", "),
			),
			Value:   map[string]any{"repeated_tools": violations},
			Details: map[string]any{"violations": violations},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("no tool called more than %d time(s) consecutively", maxRepeats),
		Value:       map[string]any{"repeated_tools": []map[string]any{}},
	}, nil
}
