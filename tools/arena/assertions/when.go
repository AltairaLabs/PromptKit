package assertions

import (
	"fmt"
	"regexp"
)

// ShouldRun evaluates when-conditions against the current turn's tool trace.
// Returns whether the assertion should run and a reason string if skipped.
// When no tool trace is available (e.g. duplex path), returns true to let the
// validator itself decide how to handle the missing data.
func (w *AssertionWhen) ShouldRun(
	params map[string]interface{},
) (shouldRun bool, reason string) {
	trace, ok := resolveTurnToolTrace(params)
	if !ok {
		return true, ""
	}

	if w.AnyToolCalled && len(trace) == 0 {
		return false, "no tool calls in turn"
	}

	if ok, msg := w.checkToolCalled(trace); !ok {
		return false, msg
	}

	if ok, msg := w.checkToolCalledPattern(trace); !ok {
		return false, msg
	}

	if w.MinToolCalls > 0 && len(trace) < w.MinToolCalls {
		return false, fmt.Sprintf(
			"only %d tool call(s), need %d", len(trace), w.MinToolCalls,
		)
	}

	return true, ""
}

// checkToolCalled checks if a specific tool name was called in the trace.
func (w *AssertionWhen) checkToolCalled(
	trace []TurnToolCall,
) (ok bool, reason string) {
	if w.ToolCalled == "" {
		return true, ""
	}
	for _, tc := range trace {
		if tc.Name == w.ToolCalled {
			return true, ""
		}
	}
	return false, fmt.Sprintf("tool %q not called", w.ToolCalled)
}

// checkToolCalledPattern checks if any tool name matches the regex pattern.
func (w *AssertionWhen) checkToolCalledPattern(
	trace []TurnToolCall,
) (ok bool, reason string) {
	if w.ToolCalledPattern == "" {
		return true, ""
	}
	re, err := regexp.Compile(w.ToolCalledPattern)
	if err != nil {
		return false, fmt.Sprintf(
			"invalid tool_called_pattern %q: %v",
			w.ToolCalledPattern, err,
		)
	}
	for _, tc := range trace {
		if re.MatchString(tc.Name) {
			return true, ""
		}
	}
	return false, fmt.Sprintf(
		"no tool matching pattern %q", w.ToolCalledPattern,
	)
}
