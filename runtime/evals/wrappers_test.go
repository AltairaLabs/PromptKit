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

// newWrapperTestRegistry creates a registry with the given mock handler and wrapper handlers registered.
func newWrapperTestRegistry(handlers ...*mockHandler) *EvalTypeRegistry {
	r := NewEmptyEvalTypeRegistry()
	for _, h := range handlers {
		r.Register(h)
	}
	r.Register(&AssertionEvalHandler{registry: r})
	r.Register(&GuardrailEvalHandler{registry: r})
	return r
}

// --- AssertionEvalHandler ---

func TestAssertionEvalHandler_MinScore_Pass(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.8)},
	})
	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type":   "test",
		"eval_params": map[string]any{},
		"min_score":   0.7,
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if passed, _ := result.Value.(bool); !passed {
		t.Fatal("expected pass: score 0.8 >= min_score 0.7")
	}
}

func TestAssertionEvalHandler_MinScore_Fail(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.3)},
	})
	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type": "test",
		"min_score": 0.7,
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if passed, _ := result.Value.(bool); passed {
		t.Fatal("expected fail: score 0.3 < min_score 0.7")
	}
}

func TestAssertionEvalHandler_MaxScore_Pass(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.5)},
	})
	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type": "test",
		"max_score": 0.8,
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if passed, _ := result.Value.(bool); !passed {
		t.Fatal("expected pass: score 0.5 <= max_score 0.8")
	}
}

func TestAssertionEvalHandler_MaxScore_Fail(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.9)},
	})
	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type": "test",
		"max_score": 0.8,
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if passed, _ := result.Value.(bool); passed {
		t.Fatal("expected fail: score 0.9 > max_score 0.8")
	}
}

func TestAssertionEvalHandler_NoThresholds_DefaultsToMinScore1(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.5)},
	})
	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type": "test",
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if passed, _ := result.Value.(bool); passed {
		t.Fatal("expected fail: no thresholds defaults to min_score=1.0, score 0.5 < 1.0")
	}
}

func TestAssertionEvalHandler_NestedEvalParams(t *testing.T) {
	// Verify that eval_params are passed to the inner handler
	inner := &captureParamsHandler{typeName: "test", result: &EvalResult{Score: float64Ptr(1.0)}}
	reg := NewEmptyEvalTypeRegistry()
	reg.Register(inner)
	reg.Register(&AssertionEvalHandler{registry: reg})

	handler, _ := reg.Get(WrapperTypeAssertion)
	params := map[string]any{
		"eval_type": "test",
		"min_score": 0.5,
		"eval_params": map[string]any{
			"criteria": "check quality",
			"judge":    "mock",
		},
	}

	_, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}

	// Inner handler should receive eval_params, not wrapper params
	if inner.capturedParams == nil {
		t.Fatal("inner handler was not called")
	}
	if inner.capturedParams["criteria"] != "check quality" {
		t.Fatalf("expected criteria='check quality', got %v", inner.capturedParams["criteria"])
	}
	if _, hasMinScore := inner.capturedParams["min_score"]; hasMinScore {
		t.Fatal("min_score should NOT be passed to inner handler")
	}
}

func TestAssertionEvalHandler_MissingEvalType(t *testing.T) {
	reg := newWrapperTestRegistry()
	handler, _ := reg.Get(WrapperTypeAssertion)

	_, err := handler.Eval(context.Background(), &EvalContext{}, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing eval_type")
	}
}

func TestAssertionEvalHandler_UnknownInnerType(t *testing.T) {
	reg := newWrapperTestRegistry()
	handler, _ := reg.Get(WrapperTypeAssertion)

	_, err := handler.Eval(context.Background(), &EvalContext{}, map[string]any{
		"eval_type": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for unknown inner eval type")
	}
}

func TestAssertionEvalHandler_Type(t *testing.T) {
	h := &AssertionEvalHandler{}
	if h.Type() != WrapperTypeAssertion {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

// --- GuardrailEvalHandler ---

func TestGuardrailEvalHandler_TriggeredOnLowScore(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.2)},
	})
	handler, _ := reg.Get(WrapperTypeGuardrail)
	params := map[string]any{
		"eval_type": "test",
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if triggered, ok := result.Details["triggered"].(bool); !ok || !triggered {
		t.Fatal("expected triggered=true in details")
	}
	if action, ok := result.Details["action"].(string); !ok || action != "block" {
		t.Fatal("expected action=block (default)")
	}
}

func TestGuardrailEvalHandler_NotTriggeredOnHighScore(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(1.0)},
	})
	handler, _ := reg.Get(WrapperTypeGuardrail)
	params := map[string]any{
		"eval_type": "test",
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if triggered, ok := result.Details["triggered"].(bool); !ok || triggered {
		t.Fatal("expected triggered=false")
	}
}

func TestGuardrailEvalHandler_CustomMinScore(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.6)},
	})
	handler, _ := reg.Get(WrapperTypeGuardrail)
	params := map[string]any{
		"eval_type": "test",
		"min_score": 0.8,
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if triggered, ok := result.Details["triggered"].(bool); !ok || !triggered {
		t.Fatal("expected triggered: score 0.6 < min_score 0.8")
	}
}

func TestGuardrailEvalHandler_CustomAction(t *testing.T) {
	reg := newWrapperTestRegistry(&mockHandler{
		typeName: "test",
		result:   &EvalResult{Score: float64Ptr(0.0)},
	})
	handler, _ := reg.Get(WrapperTypeGuardrail)
	params := map[string]any{
		"eval_type": "test",
		"action":    "warn",
	}

	result, err := handler.Eval(context.Background(), &EvalContext{}, params)
	if err != nil {
		t.Fatal(err)
	}
	if action, ok := result.Details["action"].(string); !ok || action != "warn" {
		t.Fatalf("expected action=warn, got %v", result.Details["action"])
	}
}

func TestGuardrailEvalHandler_Type(t *testing.T) {
	h := &GuardrailEvalHandler{}
	if h.Type() != WrapperTypeGuardrail {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestGuardrailEvalHandler_MissingEvalType(t *testing.T) {
	reg := newWrapperTestRegistry()
	handler, _ := reg.Get(WrapperTypeGuardrail)

	_, err := handler.Eval(context.Background(), &EvalContext{}, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing eval_type")
	}
}

// --- helpers ---

// captureParamsHandler captures the params passed to it for inspection.
type captureParamsHandler struct {
	typeName       string
	result         *EvalResult
	capturedParams map[string]any
}

func (c *captureParamsHandler) Type() string { return c.typeName }

func (c *captureParamsHandler) Eval(
	_ context.Context, _ *EvalContext, params map[string]any,
) (*EvalResult, error) {
	c.capturedParams = params
	r := *c.result
	return &r, nil
}
