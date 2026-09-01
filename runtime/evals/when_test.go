package evals

import (
	"strings"
	"testing"
)

func TestShouldRunWhen_NilWhen(t *testing.T) {
	ok, reason := ShouldRunWhen(nil, nil)
	if !ok {
		t.Error("nil when should return true")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestShouldRunWhen_AnyToolCalled_NoTools(t *testing.T) {
	when := &EvalWhen{AnyToolCalled: true}
	ok, reason := ShouldRunWhen(EncodeEvalWhen(when), nil)
	if ok {
		t.Error("should skip when no tool calls")
	}
	if reason != "no tool calls in turn" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldRunWhen_AnyToolCalled_WithTools(t *testing.T) {
	when := &EvalWhen{AnyToolCalled: true}
	calls := []ToolCallRecord{{ToolName: "search"}}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if !ok {
		t.Error("should run when tool calls present")
	}
}

func TestShouldRunWhen_ToolCalled_Match(t *testing.T) {
	when := &EvalWhen{ToolCalled: "search"}
	calls := []ToolCallRecord{
		{ToolName: "format"},
		{ToolName: "search"},
	}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if !ok {
		t.Error("should run when named tool was called")
	}
}

func TestShouldRunWhen_ToolCalled_NoMatch(t *testing.T) {
	when := &EvalWhen{ToolCalled: "search"}
	calls := []ToolCallRecord{{ToolName: "format"}}
	ok, reason := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if ok {
		t.Error("should skip when named tool not called")
	}
	if reason != `tool "search" not called` {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldRunWhen_ToolCalledPattern_Match(t *testing.T) {
	when := &EvalWhen{ToolCalledPattern: "workflow__.*"}
	calls := []ToolCallRecord{{ToolName: "workflow__transition"}}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if !ok {
		t.Error("should run when pattern matches")
	}
}

func TestShouldRunWhen_ToolCalledPattern_NoMatch(t *testing.T) {
	when := &EvalWhen{ToolCalledPattern: "workflow__.*"}
	calls := []ToolCallRecord{{ToolName: "search"}}
	ok, reason := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if ok {
		t.Error("should skip when pattern doesn't match")
	}
	if reason != `no tool matching pattern "workflow__.*"` {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldRunWhen_ToolCalledPattern_InvalidRegex(t *testing.T) {
	when := &EvalWhen{ToolCalledPattern: "[invalid"}
	calls := []ToolCallRecord{{ToolName: "search"}}
	ok, reason := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if ok {
		t.Error("should skip on invalid regex")
	}
	if reason == "" {
		t.Error("expected reason for invalid regex")
	}
}

func TestShouldRunWhen_MinToolCalls_Met(t *testing.T) {
	when := &EvalWhen{MinToolCalls: 2}
	calls := []ToolCallRecord{
		{ToolName: "a"},
		{ToolName: "b"},
	}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if !ok {
		t.Error("should run when min tool calls met")
	}
}

func TestShouldRunWhen_MinToolCalls_NotMet(t *testing.T) {
	when := &EvalWhen{MinToolCalls: 3}
	calls := []ToolCallRecord{{ToolName: "a"}}
	ok, reason := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if ok {
		t.Error("should skip when min tool calls not met")
	}
	if reason != "only 1 tool call(s), need 3" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestShouldRunWhen_CombinedConditions(t *testing.T) {
	when := &EvalWhen{
		AnyToolCalled: true,
		ToolCalled:    "search",
		MinToolCalls:  2,
	}
	calls := []ToolCallRecord{
		{ToolName: "search"},
		{ToolName: "format"},
	}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if !ok {
		t.Error("should run when all conditions met")
	}
}

func TestShouldRunWhen_CombinedConditions_PartialFail(t *testing.T) {
	when := &EvalWhen{
		AnyToolCalled: true,
		ToolCalled:    "missing_tool",
		MinToolCalls:  1,
	}
	calls := []ToolCallRecord{{ToolName: "search"}}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), calls)
	if ok {
		t.Error("should skip when one condition fails")
	}
}

func TestShouldRunWhen_EmptyWhen(t *testing.T) {
	when := &EvalWhen{}
	ok, _ := ShouldRunWhen(EncodeEvalWhen(when), nil)
	if !ok {
		t.Error("empty when (no conditions) should return true")
	}
}

// TestValidateEvalWhen_RejectsUnsupportedKeys — the spec's `when` is an open
// object, so a pack can carry a key promptkit does not implement (the spec's
// own examples, has_variable and turn_count_gte, are two such keys). Silently
// decoding those away left the eval ungated (#1931).
func TestValidateEvalWhen_RejectsUnsupportedKeys(t *testing.T) {
	err := ValidateEvalWhen(map[string]any{"turn_count_gte": 3})
	if err == nil {
		t.Fatal("an unimplemented when key must be reported, not ignored")
	}
	if !strings.Contains(err.Error(), "turn_count_gte") {
		t.Errorf("error must name the offending key, got %q", err)
	}
	if !strings.Contains(err.Error(), "tool_called") {
		t.Errorf("error must list the supported keys, got %q", err)
	}
}

// TestValidateEvalWhen_NamesEveryUnsupportedKeyInOrder — an author fixing a
// `when` block should see all of its bad keys at once, deterministically.
func TestValidateEvalWhen_NamesEveryUnsupportedKeyInOrder(t *testing.T) {
	err := ValidateEvalWhen(map[string]any{
		"turn_count_gte": 3,
		"has_variable":   "customer_tier",
		"tool_calledd":   "search",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	want := `"has_variable", "tool_calledd", "turn_count_gte"`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected sorted key list %s, got %q", want, err)
	}
}

// TestValidateEvalWhen_RejectsAWrongValueType — a recognised key carrying the
// wrong type also decodes to nothing, and left the eval just as ungated.
func TestValidateEvalWhen_RejectsAWrongValueType(t *testing.T) {
	err := ValidateEvalWhen(map[string]any{"min_tool_calls": "lots"})
	if err == nil {
		t.Fatal("a when value of the wrong type must be reported")
	}
	if !strings.Contains(err.Error(), "min_tool_calls") {
		t.Errorf("error must name the offending key, got %q", err)
	}
}

// TestValidateEvalWhen_AcceptsWhatPromptkitImplements — asserted alongside the
// negative cases so they cannot pass by rejecting everything.
func TestValidateEvalWhen_AcceptsWhatPromptkitImplements(t *testing.T) {
	for _, raw := range []map[string]any{
		nil,
		{},
		{"tool_called": "search"},
		{"tool_called_pattern": "workflow__.*"},
		{"any_tool_called": true},
		{"min_tool_calls": 2},
		{"tool_called": "search", "min_tool_calls": 2},
	} {
		if err := ValidateEvalWhen(raw); err != nil {
			t.Errorf("ValidateEvalWhen(%v) = %v, want nil", raw, err)
		}
	}
}

// TestShouldRunWhen_UnsupportedKeyDoesNotSilentlyRun — the gate an author wrote
// cannot be honoured, so the eval must not run as though no gate existed.
func TestShouldRunWhen_UnsupportedKeyDoesNotSilentlyRun(t *testing.T) {
	ok, reason := ShouldRunWhen(map[string]any{"turn_count_gte": 3}, nil)
	if ok {
		t.Error("an eval gated on an unimplemented key must not run")
	}
	if !strings.Contains(reason, "turn_count_gte") {
		t.Errorf("reason must name the offending key, got %q", reason)
	}
}
