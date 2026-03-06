package assertions

import (
	"fmt"
	"regexp"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// WhenEvaluator wraps an AssertionWhen with compiled-regex caching for
// efficient repeated evaluation of tool-call pattern conditions.
type WhenEvaluator struct {
	w               *config.AssertionWhen
	compiledPattern *regexp.Regexp
}

// NewWhenEvaluator creates a WhenEvaluator for the given condition.
func NewWhenEvaluator(w *config.AssertionWhen) *WhenEvaluator {
	return &WhenEvaluator{w: w}
}

// ShouldRun evaluates when-conditions against the current turn's tool trace.
// Returns whether the assertion should run and a reason string if skipped.
// When no tool trace is available (e.g. duplex path), returns true to let the
// validator itself decide how to handle the missing data.
func (e *WhenEvaluator) ShouldRun(
	params map[string]interface{},
) (shouldRun bool, reason string) {
	trace, ok := resolveTurnToolTrace(params)
	if !ok {
		return true, ""
	}

	if e.w.AnyToolCalled && len(trace) == 0 {
		return false, "no tool calls in turn"
	}

	if ok, msg := e.checkToolCalled(trace); !ok {
		return false, msg
	}

	if ok, msg := e.checkToolCalledPattern(trace); !ok {
		return false, msg
	}

	if e.w.MinToolCalls > 0 && len(trace) < e.w.MinToolCalls {
		return false, fmt.Sprintf(
			"only %d tool call(s), need %d", len(trace), e.w.MinToolCalls,
		)
	}

	return true, ""
}

// checkToolCalled checks if a specific tool name was called in the trace.
func (e *WhenEvaluator) checkToolCalled(
	trace []TurnToolCall,
) (ok bool, reason string) {
	if e.w.ToolCalled == "" {
		return true, ""
	}
	for _, tc := range trace {
		if tc.Name == e.w.ToolCalled {
			return true, ""
		}
	}
	return false, fmt.Sprintf("tool %q not called", e.w.ToolCalled)
}

// checkToolCalledPattern checks if any tool name matches the regex pattern.
func (e *WhenEvaluator) checkToolCalledPattern(
	trace []TurnToolCall,
) (ok bool, reason string) {
	if e.w.ToolCalledPattern == "" {
		return true, ""
	}

	// Compile once and cache the regex for subsequent calls.
	if e.compiledPattern == nil {
		re, err := regexp.Compile(e.w.ToolCalledPattern)
		if err != nil {
			return false, fmt.Sprintf(
				"invalid tool_called_pattern %q: %v",
				e.w.ToolCalledPattern, err,
			)
		}
		e.compiledPattern = re
	}

	for _, tc := range trace {
		if e.compiledPattern.MatchString(tc.Name) {
			return true, ""
		}
	}
	return false, fmt.Sprintf(
		"no tool matching pattern %q", e.w.ToolCalledPattern,
	)
}
