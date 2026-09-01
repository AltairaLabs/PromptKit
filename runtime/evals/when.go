package evals

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// supportedWhenKeys is the set of `when` conditions promptkit implements, and
// supportedWhenKeyList the same set rendered for an error message. Both are
// derived from EvalWhen's json tags rather than written out, so adding a
// condition to the struct cannot leave this set behind.
var supportedWhenKeys, supportedWhenKeyList = func() (map[string]bool, string) {
	t := reflect.TypeFor[EvalWhen]()
	keys := make(map[string]bool, t.NumField())
	names := make([]string, 0, t.NumField())
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return keys, strings.Join(names, ", ")
}()

// ValidateEvalWhen reports an authoring fault in a `when` object: a condition
// promptkit does not implement, or a recognized condition carrying a value of
// the wrong type.
//
// The spec defines $defs/Eval.when as additionalProperties:true with no named
// properties, and its own two examples — has_variable and turn_count_gte — are
// conditions promptkit does not implement. So nothing upstream rejects a key
// this runtime cannot honor, and until v1.8.0 opened promptconfig.json's `when`
// to match the spec, the closed schema was the only thing catching a typo.
// Neither running the eval as though no gate had been written nor skipping it
// as though the gate had failed is a defensible reading of "the author asked
// for something this runtime cannot do", so it is reported instead (#1931).
func ValidateEvalWhen(raw map[string]any) error {
	if len(raw) == 0 {
		return nil
	}

	unsupported := make([]string, 0, len(raw))
	for key := range raw {
		if !supportedWhenKeys[key] {
			unsupported = append(unsupported, strconv.Quote(key))
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return fmt.Errorf(
			"unsupported when condition(s): %s (supported: %s)",
			strings.Join(unsupported, ", "), supportedWhenKeyList,
		)
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("when is not encodable: %w", err)
	}
	var when EvalWhen
	if err := json.Unmarshal(data, &when); err != nil {
		return fmt.Errorf("when has a value of the wrong type: %w", err)
	}
	return nil
}

// ShouldRunWhen evaluates `when` preconditions against the current eval
// context's tool call records. Returns whether the eval should run and a reason
// string if skipped. When toolCalls is nil (e.g. duplex path), returns true to
// let the handler itself decide how to handle the missing data.
//
// A `when` this runtime cannot honor gates the eval off with the authoring
// fault as its reason, rather than running it unconditionally. Callers that can
// report an error rather than a skip — the eval runner does — should check
// ValidateEvalWhen first and surface that instead.
//
// Takes the raw map because that is what the spec defines: $defs/Eval.when is
// additionalProperties:true with no named properties, so the generated type is
// map[string]any and EvalWhen is promptkit's own reading of it. Decoding here
// keeps that reading in one place instead of at each call site.
func ShouldRunWhen(raw map[string]any, toolCalls []ToolCallRecord) (shouldRun bool, reason string) {
	if err := ValidateEvalWhen(raw); err != nil {
		return false, err.Error()
	}

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
// `when` object. A map that decodes to no conditions yields nil, meaning no
// gate — which is correct only for a `when` that is empty or that sets its
// conditions to their zero values. An unrecognized or wrongly typed `when` also
// decodes to nothing, and is an authoring fault rather than an absent gate, so
// callers must reject it with ValidateEvalWhen before reaching here.
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
