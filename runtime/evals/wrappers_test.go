package evals

import (
	"context"
	"testing"
)

// mockHandler is a test handler that returns configurable results.
type mockHandler struct {
	typeName string
	result   *EvalResult
	err      error
}

func (m *mockHandler) Type() string { return m.typeName }

func (m *mockHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Return a copy to avoid shared mutation
	r := *m.result
	return &r, nil
}

// --- extractInnerParams ---

func TestExtractInnerParams_StripsWrapperKeys(t *testing.T) {
	params := map[string]any{
		"min_score": 0.5,
		"max_score": 0.9,
		"action":    "block",
		"direction": "up",
		"pattern":   "hello",
		"fields":    []string{"a", "b"},
	}
	inner := extractInnerParams(params)

	// Wrapper keys should be stripped
	for _, key := range []string{"min_score", "max_score", "action", "direction"} {
		if _, ok := inner[key]; ok {
			t.Errorf("expected wrapper key %q to be stripped", key)
		}
	}
	// Non-wrapper keys should pass through
	for _, key := range []string{"pattern", "fields"} {
		if _, ok := inner[key]; !ok {
			t.Errorf("expected non-wrapper key %q to pass through", key)
		}
	}
}

// --- AssertionEvalHandler ---

func TestAssertionEvalHandler_MinScore_Pass(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.8)},
	}
	h := &AssertionEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"min_score": 0.7}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: score 0.8 >= min_score 0.7")
	}
}

func TestAssertionEvalHandler_MinScore_Fail(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.3)},
	}
	h := &AssertionEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"min_score": 0.7}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: score 0.3 < min_score 0.7")
	}
}

func TestAssertionEvalHandler_MaxScore_Pass(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.5)},
	}
	h := &AssertionEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"max_score": 0.8}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: score 0.5 <= max_score 0.8")
	}
}

func TestAssertionEvalHandler_MaxScore_Fail(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.9)},
	}
	h := &AssertionEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"max_score": 0.8}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: score 0.9 > max_score 0.8")
	}
}

func TestAssertionEvalHandler_NoThresholds_FallsBackToInner(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.5)},
	}
	h := &AssertionEvalHandler{Inner: inner, EvalType: "test"}

	result, err := h.Eval(context.Background(), &EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: no thresholds, falls back to inner Score=0.5 (IsPassed()=false)")
	}
}

func TestAssertionEvalHandler_Type(t *testing.T) {
	h := &AssertionEvalHandler{EvalType: "sentence_count"}
	if h.Type() != "assertion:sentence_count" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

// --- GuardrailEvalHandler ---

func TestGuardrailEvalHandler_TriggeredOnLowScore(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.2)},
	}
	h := &GuardrailEvalHandler{Inner: inner, EvalType: "test"}

	result, err := h.Eval(context.Background(), &EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: guardrail triggered on inner Score=0.2 (IsPassed()=false)")
	}
	if triggered, ok := result.Details["triggered"].(bool); !ok || !triggered {
		t.Fatal("expected triggered=true in details")
	}
	if action, ok := result.Details["action"].(string); !ok || action != "block" {
		t.Fatal("expected action=block (default)")
	}
}

func TestGuardrailEvalHandler_NotTriggeredOnHighScore(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(1.0)},
	}
	h := &GuardrailEvalHandler{Inner: inner, EvalType: "test"}

	result, err := h.Eval(context.Background(), &EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatal("expected pass: guardrail not triggered on inner Score=1.0 (IsPassed()=true)")
	}
	if triggered, ok := result.Details["triggered"].(bool); !ok || triggered {
		t.Fatal("expected triggered=false")
	}
}

func TestGuardrailEvalHandler_CustomMinScore(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.6)},
	}
	h := &GuardrailEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"min_score": 0.8}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail: score 0.6 < min_score 0.8 triggers guardrail")
	}
}

func TestGuardrailEvalHandler_CustomAction(t *testing.T) {
	inner := &mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.0)},
	}
	h := &GuardrailEvalHandler{Inner: inner, EvalType: "test"}
	params := map[string]any{"action": "warn"}

	result, err := h.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if action, ok := result.Details["action"].(string); !ok || action != "warn" {
		t.Fatalf("expected action=warn, got %v", result.Details["action"])
	}
}

func TestGuardrailEvalHandler_Type(t *testing.T) {
	h := &GuardrailEvalHandler{EvalType: "content_excludes"}
	if h.Type() != "guardrail:content_excludes" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}
