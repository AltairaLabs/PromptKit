package evals

import (
	"fmt"
	"regexp"
)

// ShouldRunWhen evaluates EvalWhen preconditions against the current eval context's
// tool call records. Returns whether the eval should run and a reason string if skipped.
// When toolCalls is nil (e.g. duplex path), returns true to let the handler itself
// decide how to handle the missing data.
func ShouldRunWhen(when *EvalWhen, toolCalls []ToolCallRecord) (shouldRun bool, reason string) {
	if when == nil {
		return true, ""
	}

	if when.AnyToolCalled && len(toolCalls) == 0 {
		return false, "no tool calls in turn"
	}

	if ok, msg := checkToolCalled(when.ToolCalled, toolCalls); !ok {
		return false, msg
	}

	if ok, msg := checkToolCalledPattern(when.ToolCalledPattern, toolCalls); !ok {
		return false, msg
	}

	if when.MinToolCalls > 0 && len(toolCalls) < when.MinToolCalls {
		return false, fmt.Sprintf(
			"only %d tool call(s), need %d", len(toolCalls), when.MinToolCalls,
		)
	}

	return true, ""
}

// checkToolCalled checks if a specific tool name was called.
func checkToolCalled(toolName string, toolCalls []ToolCallRecord) (ok bool, reason string) {
	if toolName == "" {
		return true, ""
	}
	for i := range toolCalls {
		if toolCalls[i].ToolName == toolName {
			return true, ""
		}
	}
	return false, fmt.Sprintf("tool %q not called", toolName)
}

// checkToolCalledPattern checks if any tool name matches the regex pattern.
func checkToolCalledPattern(pattern string, toolCalls []ToolCallRecord) (ok bool, reason string) {
	if pattern == "" {
		return true, ""
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Sprintf(
			"invalid tool_called_pattern %q: %v", pattern, err,
		)
	}
	for i := range toolCalls {
		if re.MatchString(toolCalls[i].ToolName) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("no tool matching pattern %q", pattern)
}
