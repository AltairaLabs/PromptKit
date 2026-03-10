package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestGuardrailTriggeredHandler_Type(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	if h.Type() != "guardrail_triggered" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestGuardrailTriggered_TriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_banned", Type: "content_excludes", Score: boolScore(false)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_MatchByEvalID(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "banned_words_check", Type: "content_excludes", Score: boolScore(false)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words_check",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass when matching by EvalID: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_NotTriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_length", Type: "max_length", Score: boolScore(true)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "max_length",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_ExpectedTriggerButPassed(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_banned", Type: "content_excludes", Score: boolScore(true)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail when trigger expected but eval passed")
	}
}

func TestGuardrailTriggered_ValidatorNotFound(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail when validator not found but expected to trigger")
	}
}

func TestGuardrailTriggered_ValidatorNotFoundButNotExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "nonexistent",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass when not-found and should_trigger=false: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_NoValidatorType(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail with no validator_type")
	}
}

func TestGuardrailTriggered_DefaultShouldTriggerTrue(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "content_excludes", Score: boolScore(false)},
		},
	}

	// Omit should_trigger — defaults to true.
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass with default should_trigger=true: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_ValidatorAliasParam(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "content_excludes", Score: boolScore(false)},
		},
	}

	// Use "validator" param instead of "validator_type".
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator":      "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass with validator alias: %s", result.Explanation)
	}
}
