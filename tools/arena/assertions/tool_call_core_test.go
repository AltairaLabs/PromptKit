package assertions

import (
	"testing"
)

func TestToolCallViewsFromTrace(t *testing.T) {
	trace := []TurnToolCall{
		{Name: "get_order", Args: map[string]interface{}{"id": "1"}, Result: "ok", Error: "", RoundIndex: 0},
		{Name: "refund", Args: nil, Result: "done", Error: "fail", RoundIndex: 1},
	}
	views := toolCallViewsFromTrace(trace)

	if len(views) != 2 {
		t.Fatalf("expected 2 views, got %d", len(views))
	}
	if views[0].Name != "get_order" || views[0].Result != "ok" || views[0].Index != 0 {
		t.Errorf("view[0] = %+v", views[0])
	}
	if views[1].Error != "fail" || views[1].Index != 1 {
		t.Errorf("view[1] = %+v", views[1])
	}
}

func TestToolCallViewsFromRecords(t *testing.T) {
	records := []ToolCallRecord{
		{TurnIndex: 0, ToolName: "search", Arguments: map[string]interface{}{"q": "test"}, Result: "found", Error: ""},
		{TurnIndex: 2, ToolName: "read", Result: nil, Error: "not found"},
		{TurnIndex: 3, ToolName: "write", Result: map[string]interface{}{"status": "ok"}, Error: ""},
	}
	views := toolCallViewsFromRecords(records)

	if len(views) != 3 {
		t.Fatalf("expected 3 views, got %d", len(views))
	}
	if views[0].Name != "search" || views[0].Result != "found" || views[0].Index != 0 {
		t.Errorf("view[0] = %+v", views[0])
	}
	// nil Result should produce empty string, not "<nil>"
	if views[1].Result != "" {
		t.Errorf("expected empty result for nil, got %q", views[1].Result)
	}
	if views[1].Error != "not found" {
		t.Errorf("expected error 'not found', got %q", views[1].Error)
	}
	// map Result should be stringified
	if views[2].Result != "map[status:ok]" {
		t.Errorf("expected stringified map, got %q", views[2].Result)
	}
}

func TestCoreNoToolErrors(t *testing.T) {
	calls := []ToolCallView{
		{Name: "get_order", Error: "", Index: 0},
		{Name: "refund", Error: "insufficient funds", Index: 0},
		{Name: "notify", Error: "", Index: 1},
	}

	// No scope — should find 1 error
	errors := coreNoToolErrors(calls, nil)
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if errors[0]["tool"] != "refund" {
		t.Errorf("expected refund error, got %v", errors[0])
	}

	// Scoped to get_order only — no errors
	errors = coreNoToolErrors(calls, []string{"get_order"})
	if len(errors) != 0 {
		t.Errorf("expected 0 errors when scoped to get_order, got %d", len(errors))
	}

	// Scoped to refund — 1 error
	errors = coreNoToolErrors(calls, []string{"refund"})
	if len(errors) != 1 {
		t.Errorf("expected 1 error when scoped to refund, got %d", len(errors))
	}

	// Empty calls
	errors = coreNoToolErrors(nil, nil)
	if len(errors) != 0 {
		t.Errorf("expected 0 errors for empty calls, got %d", len(errors))
	}
}

func TestCoreToolCallCount(t *testing.T) {
	calls := []ToolCallView{
		{Name: "search"},
		{Name: "search"},
		{Name: "read"},
	}

	// Count all
	count, violation := coreToolCallCount(calls, "", countNotSet, countNotSet)
	if count != 3 || violation != "" {
		t.Errorf("count all: count=%d violation=%q", count, violation)
	}

	// Count specific tool
	count, violation = coreToolCallCount(calls, "search", countNotSet, countNotSet)
	if count != 2 || violation != "" {
		t.Errorf("count search: count=%d violation=%q", count, violation)
	}

	// Min violation
	count, violation = coreToolCallCount(calls, "search", 5, countNotSet)
	if count != 2 || violation == "" {
		t.Errorf("expected min violation, count=%d violation=%q", count, violation)
	}

	// Max violation
	count, violation = coreToolCallCount(calls, "search", countNotSet, 1)
	if count != 2 || violation == "" {
		t.Errorf("expected max violation, count=%d violation=%q", count, violation)
	}

	// Within bounds
	count, violation = coreToolCallCount(calls, "search", 1, 5)
	if count != 2 || violation != "" {
		t.Errorf("expected within bounds, count=%d violation=%q", count, violation)
	}
}

func TestCoreToolResultIncludes(t *testing.T) {
	calls := []ToolCallView{
		{Name: "search", Result: "Found: order ORD-123 shipped"},
		{Name: "search", Result: "Found: order ORD-456 pending"},
		{Name: "other", Result: "irrelevant"},
	}

	// All patterns match in both search calls
	matchCount, missing := coreToolResultIncludes(calls, "search", []string{"found", "order"})
	if matchCount != 2 || len(missing) != 0 {
		t.Errorf("expected 2 matches, got %d, missing=%v", matchCount, missing)
	}

	// Pattern only in one call
	matchCount, missing = coreToolResultIncludes(calls, "search", []string{"shipped"})
	if matchCount != 1 {
		t.Errorf("expected 1 match for 'shipped', got %d", matchCount)
	}
	if len(missing) != 1 {
		t.Errorf("expected 1 missing detail, got %d", len(missing))
	}

	// No tool filter
	matchCount, _ = coreToolResultIncludes(calls, "", []string{"found"})
	if matchCount != 2 {
		t.Errorf("expected 2 matches with no tool filter, got %d", matchCount)
	}

	// Empty calls
	matchCount, _ = coreToolResultIncludes(nil, "", []string{"x"})
	if matchCount != 0 {
		t.Errorf("expected 0 matches for empty calls, got %d", matchCount)
	}
}

func TestCoreToolResultMatches(t *testing.T) {
	calls := []ToolCallView{
		{Name: "get_order", Result: `{"order_id":"ORD-123"}`},
		{Name: "get_order", Result: "no order"},
	}

	matchCount, err := coreToolResultMatches(calls, "get_order", `ORD-\d+`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matchCount != 1 {
		t.Errorf("expected 1 match, got %d", matchCount)
	}

	// Invalid regex
	_, err = coreToolResultMatches(calls, "", `[invalid`)
	if err == nil {
		t.Error("expected error for invalid regex")
	}

	// No tool filter
	matchCount, _ = coreToolResultMatches(calls, "", `order`)
	if matchCount != 2 {
		t.Errorf("expected 2 matches with no tool filter, got %d", matchCount)
	}
}

func TestCoreToolCallSequence(t *testing.T) {
	calls := []ToolCallView{
		{Name: "search"},
		{Name: "read"},
		{Name: "write"},
	}

	// Full match
	matched, actual := coreToolCallSequence(calls, []string{"search", "write"})
	if matched != 2 {
		t.Errorf("expected 2 matched, got %d", matched)
	}
	if len(actual) != 3 {
		t.Errorf("expected 3 actual tools, got %d", len(actual))
	}

	// Incomplete match
	matched, _ = coreToolCallSequence(calls, []string{"search", "delete"})
	if matched != 1 {
		t.Errorf("expected 1 matched, got %d", matched)
	}

	// Empty sequence
	matched, _ = coreToolCallSequence(calls, nil)
	if matched != 0 {
		t.Errorf("expected 0 matched for nil sequence, got %d", matched)
	}

	// Empty calls
	matched, _ = coreToolCallSequence(nil, []string{"search"})
	if matched != 0 {
		t.Errorf("expected 0 matched for nil calls, got %d", matched)
	}
}

func TestCoreToolCallChain(t *testing.T) {
	calls := []ToolCallView{
		{Name: "get_order", Result: `{"order_id":"ORD-123"}`, Args: map[string]interface{}{"id": "123"}, Index: 0},
		{Name: "process_refund", Result: "done", Args: map[string]interface{}{"order_id": "ORD-123"}, Index: 1},
	}

	steps := []chainStep{
		{tool: "get_order", resultIncludes: []string{"order_id"}, noError: true},
		{tool: "process_refund", argsMatch: map[string]string{"order_id": `ORD-\d+`}},
	}

	completed, failure := coreToolCallChain(calls, steps)
	if failure != nil {
		t.Fatalf("unexpected failure: %v", failure)
	}
	if completed != 2 {
		t.Errorf("expected 2 completed steps, got %d", completed)
	}

	// Chain fails on constraint
	failSteps := []chainStep{
		{tool: "get_order", resultIncludes: []string{"not_present"}},
	}
	completed, failure = coreToolCallChain(calls, failSteps)
	if failure == nil {
		t.Error("expected failure for missing pattern")
	}
	if completed != 0 {
		t.Errorf("expected 0 completed, got %d", completed)
	}

	// Incomplete chain
	longSteps := []chainStep{
		{tool: "get_order"},
		{tool: "process_refund"},
		{tool: "send_notification"},
	}
	completed, failure = coreToolCallChain(calls, longSteps)
	if failure != nil {
		t.Errorf("unexpected constraint failure: %v", failure)
	}
	if completed != 2 {
		t.Errorf("expected 2 completed, got %d", completed)
	}

	// Empty steps
	completed, failure = coreToolCallChain(calls, nil)
	if failure != nil || completed != 0 {
		t.Errorf("expected 0 completed/no failure for empty steps, got %d/%v", completed, failure)
	}

	// Error constraint
	errorCalls := []ToolCallView{
		{Name: "get_order", Error: "not found", Index: 0},
	}
	errorSteps := []chainStep{
		{tool: "get_order", noError: true},
	}
	_, failure = coreToolCallChain(errorCalls, errorSteps)
	if failure == nil {
		t.Error("expected failure for error with noError=true")
	}
}
