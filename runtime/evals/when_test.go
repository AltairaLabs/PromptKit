package evals

import "testing"

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
	ok, reason := ShouldRunWhen(when, nil)
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
	ok, _ := ShouldRunWhen(when, calls)
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
	ok, _ := ShouldRunWhen(when, calls)
	if !ok {
		t.Error("should run when named tool was called")
	}
}

func TestShouldRunWhen_ToolCalled_NoMatch(t *testing.T) {
	when := &EvalWhen{ToolCalled: "search"}
	calls := []ToolCallRecord{{ToolName: "format"}}
	ok, reason := ShouldRunWhen(when, calls)
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
	ok, _ := ShouldRunWhen(when, calls)
	if !ok {
		t.Error("should run when pattern matches")
	}
}

func TestShouldRunWhen_ToolCalledPattern_NoMatch(t *testing.T) {
	when := &EvalWhen{ToolCalledPattern: "workflow__.*"}
	calls := []ToolCallRecord{{ToolName: "search"}}
	ok, reason := ShouldRunWhen(when, calls)
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
	ok, reason := ShouldRunWhen(when, calls)
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
	ok, _ := ShouldRunWhen(when, calls)
	if !ok {
		t.Error("should run when min tool calls met")
	}
}

func TestShouldRunWhen_MinToolCalls_NotMet(t *testing.T) {
	when := &EvalWhen{MinToolCalls: 3}
	calls := []ToolCallRecord{{ToolName: "a"}}
	ok, reason := ShouldRunWhen(when, calls)
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
	ok, _ := ShouldRunWhen(when, calls)
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
	ok, _ := ShouldRunWhen(when, calls)
	if ok {
		t.Error("should skip when one condition fails")
	}
}

func TestShouldRunWhen_EmptyWhen(t *testing.T) {
	when := &EvalWhen{}
	ok, _ := ShouldRunWhen(when, nil)
	if !ok {
		t.Error("empty when (no conditions) should return true")
	}
}
