package guardrails

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// stubHandler is a test EvalTypeHandler with configurable behavior.
type stubHandler struct {
	typeName string
	result   *evals.EvalResult
	err      error
}

func (s *stubHandler) Type() string { return s.typeName }

func (s *stubHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	return s.result, s.err
}

func floatPtr(f float64) *float64 { return &f }

func TestGuardrailHookAdapter_PassingHandler(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_pass",
		result: &evals.EvalResult{
			Passed: true,
			Score:  floatPtr(1.0),
		},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_pass",
		params:    map[string]any{},
		direction: "output",
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "hello world"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if !decision.Allow {
		t.Errorf("expected Allow, got Deny: %s", decision.Reason)
	}
}

func TestGuardrailHookAdapter_FailingScore(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_fail",
		result: &evals.EvalResult{
			Passed:      false,
			Score:       floatPtr(0.0),
			Explanation: "content violation detected",
		},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_fail",
		params:    map[string]any{},
		direction: "output",
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "bad content"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if decision.Allow {
		t.Fatal("expected Deny, got Allow")
	}
	if decision.Reason != "content violation detected" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
	if decision.Metadata["validator_type"] != "test_fail" {
		t.Errorf("unexpected validator_type: %v", decision.Metadata["validator_type"])
	}
	if decision.Metadata["score"] == nil {
		t.Error("expected score in metadata")
	}
}

func TestGuardrailHookAdapter_FailingPassed(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_fail_passed",
		result: &evals.EvalResult{
			Passed:      false,
			Score:       floatPtr(0.0),
			Explanation: "missing required field",
		},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_fail_passed",
		params:    map[string]any{},
		direction: "output",
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "some output"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if decision.Allow {
		t.Fatal("expected Deny, got Allow")
	}
	if decision.Reason != "missing required field" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}

func TestGuardrailHookAdapter_InputDirection(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_input",
		result: &evals.EvalResult{
			Passed: false,
			Score:  floatPtr(0.0),
		},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_input",
		params:    map[string]any{},
		direction: "input",
	}

	// AfterCall should always allow when direction is "input"
	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "anything"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if !decision.Allow {
		t.Error("expected Allow for AfterCall with input direction")
	}

	// BeforeCall should evaluate when direction is "input"
	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Content: "user input"}},
	}
	decision = adapter.BeforeCall(context.Background(), req)
	if decision.Allow {
		t.Error("expected Deny for BeforeCall with input direction and failing handler")
	}
}

func TestGuardrailHookAdapter_BothDirection(t *testing.T) {
	callCount := 0
	handler := &stubHandler{
		typeName: "test_both",
		result: &evals.EvalResult{
			Passed: true,
			Score:  floatPtr(1.0),
		},
	}
	// Wrap to count calls
	countingHandler := &countingStubHandler{inner: handler, count: &callCount}

	adapter := &GuardrailHookAdapter{
		handler:   countingHandler,
		evalType:  "test_both",
		params:    map[string]any{},
		direction: "both",
	}

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Content: "hello"}},
	}
	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "world"},
	}

	adapter.BeforeCall(context.Background(), req)
	adapter.AfterCall(context.Background(), req, resp)

	if callCount != 2 {
		t.Errorf("expected 2 eval calls for 'both' direction, got %d", callCount)
	}
}

// countingStubHandler wraps a handler and counts Eval calls.
type countingStubHandler struct {
	inner evals.EvalTypeHandler
	count *int
}

func (c *countingStubHandler) Type() string { return c.inner.Type() }

func (c *countingStubHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	*c.count++
	return c.inner.Eval(ctx, evalCtx, params)
}

func TestGuardrailHookAdapter_HandlerError(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_error",
		err:      errors.New("eval failed"),
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_error",
		params:    map[string]any{},
		direction: "output",
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "test"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if decision.Allow {
		t.Fatal("expected Deny on handler error")
	}
	if decision.Reason != "guardrail error: eval failed" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}

func TestGuardrailHookAdapter_Name(t *testing.T) {
	adapter := &GuardrailHookAdapter{
		evalType: "my_guardrail",
	}
	if adapter.Name() != "my_guardrail" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "my_guardrail")
	}
}

func TestGuardrailHookAdapter_OutputDirection_BeforeCallAllows(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_output",
		result: &evals.EvalResult{
			Passed: false,
			Score:  floatPtr(0.0),
		},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_output",
		params:    map[string]any{},
		direction: "output",
	}

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Content: "user input"}},
	}
	decision := adapter.BeforeCall(context.Background(), req)
	if !decision.Allow {
		t.Error("expected Allow for BeforeCall with output direction")
	}
}

func TestGuardrailHookAdapter_BeforeCall_NilRequest(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_nil",
		result:   &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_nil",
		params:    map[string]any{},
		direction: "input",
	}

	decision := adapter.BeforeCall(context.Background(), nil)
	if !decision.Allow {
		t.Error("expected Allow for nil request")
	}
}

func TestGuardrailHookAdapter_BeforeCall_EmptyMessages(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_empty",
		result:   &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_empty",
		params:    map[string]any{},
		direction: "input",
	}

	req := &hooks.ProviderRequest{Messages: []types.Message{}}
	decision := adapter.BeforeCall(context.Background(), req)
	if !decision.Allow {
		t.Error("expected Allow for empty messages")
	}
}

func TestGuardrailHookAdapter_ParamsPassedToHandler(t *testing.T) {
	var capturedParams map[string]any
	handler := &capturingHandler{
		typeName: "test_params",
		result:   &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
		capture:  &capturedParams,
	}
	params := map[string]any{
		"words":    []any{"bad", "evil"},
		"severity": "high",
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_params",
		params:    params,
		direction: "output",
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "test"},
	}
	adapter.AfterCall(context.Background(), nil, resp)

	if capturedParams == nil {
		t.Fatal("params were not passed to handler")
	}
	if capturedParams["severity"] != "high" {
		t.Errorf("expected severity=high, got %v", capturedParams["severity"])
	}
}

// capturingHandler records the params passed to Eval.
type capturingHandler struct {
	typeName string
	result   *evals.EvalResult
	capture  *map[string]any
}

func (c *capturingHandler) Type() string { return c.typeName }

func (c *capturingHandler) Eval(
	_ context.Context, _ *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	*c.capture = params
	return c.result, nil
}

func TestGuardrailHookAdapter_ImplementsProviderHook(t *testing.T) {
	var _ hooks.ProviderHook = (*GuardrailHookAdapter)(nil)
}

// streamableStubHandler implements both EvalTypeHandler and StreamableEvalHandler.
type streamableStubHandler struct {
	typeName      string
	result        *evals.EvalResult
	partialResult *evals.EvalResult
	partialErr    error
}

func (s *streamableStubHandler) Type() string { return s.typeName }

func (s *streamableStubHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	return s.result, nil
}

func (s *streamableStubHandler) EvalPartial(
	_ context.Context, _ string, _ map[string]any,
) (*evals.EvalResult, error) {
	return s.partialResult, s.partialErr
}

func TestGuardrailHookAdapter_OnChunk_NonStreamable(t *testing.T) {
	handler := &stubHandler{
		typeName: "test_nonstreamable",
		result:   &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_nonstreamable",
		params:    map[string]any{},
		direction: "output",
	}

	chunk := &providers.StreamChunk{Content: "hello world", Delta: "world"}
	decision := adapter.OnChunk(context.Background(), chunk)
	if !decision.Allow {
		t.Error("expected Allow for non-streamable handler")
	}
}

func TestGuardrailHookAdapter_OnChunk_StreamablePass(t *testing.T) {
	handler := &streamableStubHandler{
		typeName:      "test_streamable",
		result:        &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
		partialResult: &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_streamable",
		params:    map[string]any{},
		direction: "output",
	}

	chunk := &providers.StreamChunk{Content: "safe content", Delta: "content"}
	decision := adapter.OnChunk(context.Background(), chunk)
	if !decision.Allow {
		t.Error("expected Allow for passing streamable handler")
	}
}

func TestGuardrailHookAdapter_OnChunk_StreamableFail(t *testing.T) {
	handler := &streamableStubHandler{
		typeName:      "test_streamable_fail",
		result:        &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
		partialResult: &evals.EvalResult{Passed: false, Score: floatPtr(0.0), Explanation: "forbidden word detected"},
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_streamable_fail",
		params:    map[string]any{},
		direction: "output",
	}

	chunk := &providers.StreamChunk{Content: "bad content", Delta: "content"}
	decision := adapter.OnChunk(context.Background(), chunk)
	if decision.Allow {
		t.Fatal("expected Deny for failing streamable handler")
	}
	if decision.Reason != "forbidden word detected" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}

func TestGuardrailHookAdapter_OnChunk_StreamableError(t *testing.T) {
	handler := &streamableStubHandler{
		typeName:   "test_streamable_err",
		result:     &evals.EvalResult{Passed: true, Score: floatPtr(1.0)},
		partialErr: errors.New("eval partial failed"),
	}
	adapter := &GuardrailHookAdapter{
		handler:   handler,
		evalType:  "test_streamable_err",
		params:    map[string]any{},
		direction: "output",
	}

	chunk := &providers.StreamChunk{Content: "test", Delta: "test"}
	decision := adapter.OnChunk(context.Background(), chunk)
	if decision.Allow {
		t.Fatal("expected Deny on EvalPartial error")
	}
	if decision.Reason != "guardrail streaming error: eval partial failed" {
		t.Errorf("unexpected reason: %s", decision.Reason)
	}
}
