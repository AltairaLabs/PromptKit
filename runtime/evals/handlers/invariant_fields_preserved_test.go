package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestInvariantFieldsPreservedHandler_Type(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	if h.Type() != "invariant_fields_preserved" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestInvariantFieldsPreserved_MissingParams(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}

	// No params at all
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail with missing params")
	}

	// Only tool, no fields
	result, err = h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"tool": "update_order",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail with missing fields param")
	}

	// Only fields, no tool
	result, err = h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{
		"fields": []any{"name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail with missing tool param")
	}
}

func TestInvariantFieldsPreserved_NoMatchingToolCalls(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("other_tool", map[string]any{"name": "Alice"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass with no matching tool calls: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_SingleCall(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass with single call: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_FieldsPreserved(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123", "status": "active"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123", "status": "done"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — fields preserved: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_FieldDisappears(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 'id' disappeared in second call")
	}
	violations := result.Details["violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0]["field"] != "id" {
		t.Fatalf("expected violation on 'id', got %q", violations[0]["field"])
	}
}

func TestInvariantFieldsPreserved_FieldNeverPresent(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — 'id' was never present, so not lost: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_MultipleFieldsMixed(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123", "email": "a@b.com"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "email": "a@b.com"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id", "email"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 'id' lost at call 1, 'email' lost at call 2")
	}
	violations := result.Details["violations"].([]map[string]any)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
}

func TestInvariantFieldsPreserved_OtherToolCallsIgnored(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("other_tool", map[string]any{"foo": "bar"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — other tool calls should be ignored: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_FieldAppearsLater(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass — 'id' appeared later and was preserved: %s", result.Explanation)
	}
}

func TestInvariantFieldsPreserved_FieldAppearsAndDisappears(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice"}, nil, ""),
		},
	}
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []any{"id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsPassed() {
		t.Fatal("expected fail — 'id' appeared at call 1 then disappeared at call 2")
	}
}

func TestInvariantFieldsPreserved_StringSliceFields(t *testing.T) {
	h := &InvariantFieldsPreservedHandler{}
	evalCtx := &evals.EvalContext{
		ToolCalls: []evals.ToolCallRecord{
			toolCall("update_order", map[string]any{"name": "Alice", "id": "123"}, nil, ""),
			toolCall("update_order", map[string]any{"name": "Alice", "id": "456"}, nil, ""),
		},
	}
	// Use []string directly to test that extractStringSlice handles both types
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool":   "update_order",
		"fields": []string{"name", "id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsPassed() {
		t.Fatalf("expected pass with []string fields: %s", result.Explanation)
	}
}
