package evals

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// ShouldRunWhen evaluates `when` preconditions against the current eval
// context's tool call records. Returns whether the eval should run and a reason
// string if skipped. When toolCalls is nil (e.g. duplex path), returns true to
// let the handler itself decide how to handle the missing data.
//
// Takes the raw map because that is what the spec defines: $defs/Eval.when is
// additionalProperties:true with no named properties, so the generated type is
// map[string]any and EvalWhen is promptkit's own reading of it. Decoding here
// keeps that reading in one place instead of at each call site.
func ShouldRunWhen(raw map[string]any, toolCalls []ToolCallRecord) (shouldRun bool, reason string) {
	when := DecodeEvalWhen(raw)
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

// DecodeEvalWhen reads promptkit's when-conditions out of the spec's open
// `when` object. A map that decodes to no conditions yields nil, so an
// unrecognized shape does not silently gate an eval off.
func DecodeEvalWhen(raw map[string]any) *EvalWhen {
	if len(raw) == 0 {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var when EvalWhen
	if err := json.Unmarshal(data, &when); err != nil {
		return nil
	}
	if when == (EvalWhen{}) {
		return nil
	}
	return &when
}

// EncodeEvalWhen is the inverse of DecodeEvalWhen: it renders promptkit's
// when-conditions into the spec's open `when` object, for anything building an
// eval programmatically rather than loading one from a pack.
func EncodeEvalWhen(when *EvalWhen) map[string]any {
	if when == nil || *when == (EvalWhen{}) {
		return nil
	}
	data, err := json.Marshal(when)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
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
