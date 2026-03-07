package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolArgsExcludedSessionHandler checks that a tool was NOT called
// with specific argument values across the session.
// Params: tool_name string, excluded_args map[string]any.
// Also accepts legacy param forbidden_args map[string][]any (from
// the original tools_not_called_with_args validator).
type ToolArgsExcludedSessionHandler struct{}

// Type returns the eval type identifier.
func (h *ToolArgsExcludedSessionHandler) Type() string {
	return "tool_args_excluded_session"
}

// Eval ensures the tool was never called with excluded args.
func (h *ToolArgsExcludedSessionHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (_ *evals.EvalResult, _ error) {
	toolName, _ := params["tool_name"].(string)

	// Build forbidden map: arg name → set of forbidden string values.
	// Accepts either excluded_args (map[string]any) or
	// forbidden_args (map[string][]any, legacy format).
	forbidden := buildForbiddenArgMap(params)

	if toolName == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "tool_name parameter is required",
		}, nil
	}

	if len(forbidden) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no excluded_args to check",
		}, nil
	}

	var violations []string
	for i := range evalCtx.ToolCalls {
		tc := &evalCtx.ToolCalls[i]
		if tc.ToolName != toolName {
			continue
		}
		violations = append(
			violations,
			checkForbiddenArgs(tc, forbidden)...,
		)
	}

	if len(violations) > 0 {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"excluded args found: %s",
				strings.Join(violations, "; "),
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "no excluded args detected",
	}, nil
}

// buildForbiddenArgMap builds a map of arg name → set of forbidden
// string values from either excluded_args or forbidden_args params.
func buildForbiddenArgMap(params map[string]any) map[string]map[string]bool {
	// Try excluded_args first (new format: map[string]any, single value)
	if ea, ok := params["excluded_args"].(map[string]any); ok && len(ea) > 0 {
		result := make(map[string]map[string]bool, len(ea))
		for k, v := range ea {
			result[k] = map[string]bool{asString(v): true}
		}
		return result
	}

	// Try forbidden_args (legacy format from tools_not_called_with_args)
	fa, ok := params["forbidden_args"]
	if !ok {
		return nil
	}

	switch m := fa.(type) {
	case map[string]any:
		return forbiddenFromMapAny(m)
	case map[string][]any:
		return forbiddenFromMapSlice(m)
	default:
		return nil
	}
}

func forbiddenFromMapAny(m map[string]any) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(m))
	for k, v := range m {
		result[k] = toStringSet(v)
	}
	return result
}

func forbiddenFromMapSlice(m map[string][]any) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(m))
	for k, vals := range m {
		set := make(map[string]bool, len(vals))
		for _, val := range vals {
			set[asString(val)] = true
		}
		result[k] = set
	}
	return result
}

// toStringSet converts a value to a set of strings. If the value is
// a slice, each element becomes an entry; otherwise the single value does.
func toStringSet(v any) map[string]bool {
	switch vals := v.(type) {
	case []any:
		set := make(map[string]bool, len(vals))
		for _, val := range vals {
			set[asString(val)] = true
		}
		return set
	case []string:
		set := make(map[string]bool, len(vals))
		for _, val := range vals {
			set[val] = true
		}
		return set
	default:
		return map[string]bool{asString(v): true}
	}
}

// checkForbiddenArgs returns descriptions for each forbidden arg match
// found in a single tool call.
func checkForbiddenArgs(
	tc *evals.ToolCallRecord,
	forbidden map[string]map[string]bool,
) []string {
	var results []string
	for k, forbiddenSet := range forbidden {
		av, ok := tc.Arguments[k]
		if !ok {
			continue
		}
		if forbiddenSet[asString(av)] {
			results = append(results, fmt.Sprintf(
				"turn %d: %s=%v", tc.TurnIndex, k, av,
			))
		}
	}
	return results
}
