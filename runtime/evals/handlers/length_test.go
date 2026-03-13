package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestMaxLengthHandler_CharacterCount(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()

	// 10 chars, max 20 => pass
	result, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: "0123456789",
	}, map[string]any{"max": 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Error("expected pass: 10 chars <= 20 max")
	}

	// 10 chars, max 5 => fail
	result2, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: "0123456789",
	}, map[string]any{"max": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Passed {
		t.Error("expected fail: 10 chars > 5 max")
	}
}

func TestMaxLengthHandler_MaxTokens(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()

	// 40 chars = ~10 tokens (at 4 chars/token), max_tokens 5 => fail
	output := "This is a test string with forty chars!!" // 40 chars
	result, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: output,
	}, map[string]any{
		"max":        1000, // char limit is fine
		"max_tokens": 5,    // but token limit is exceeded
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Errorf("expected fail: ~%d tokens > 5 max_tokens", len(output)/4)
	}

	// Verify token info in Value
	val, ok := result.Value.(map[string]any)
	if !ok {
		t.Fatal("expected Value to be map[string]any")
	}
	if val["tokens"] != len(output)/4 {
		t.Errorf("expected tokens=%d, got %v", len(output)/4, val["tokens"])
	}
	if val["max_tokens"] != 5 {
		t.Errorf("expected max_tokens=5, got %v", val["max_tokens"])
	}
}

func TestMaxLengthHandler_MaxTokensPass(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()

	// 8 chars = ~2 tokens, max_tokens 10 => pass
	result, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: "Hi there",
	}, map[string]any{
		"max":        1000,
		"max_tokens": 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Error("expected pass: ~2 tokens <= 10 max_tokens")
	}
}

func TestMaxLengthHandler_MissingMax(t *testing.T) {
	h := &MaxLengthHandler{}
	ctx := context.Background()

	result, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: "hello",
	}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail when no max param provided")
	}
}

func TestMaxLengthHandler_EvalPartial_Pass(t *testing.T) {
	h := &MaxLengthHandler{}
	result, err := h.EvalPartial(context.Background(), "short", map[string]any{"max": 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Error("expected pass: 5 chars <= 100 max")
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Errorf("expected score 1.0, got %v", result.Score)
	}
}

func TestMaxLengthHandler_EvalPartial_Fail(t *testing.T) {
	h := &MaxLengthHandler{}
	result, err := h.EvalPartial(context.Background(), "0123456789", map[string]any{"max": 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail: 10 chars > 5 max")
	}
	if result.Score == nil || *result.Score >= 1.0 {
		t.Errorf("expected score < 1.0, got %v", result.Score)
	}
}

func TestMaxLengthHandler_EvalPartial_NoMax(t *testing.T) {
	h := &MaxLengthHandler{}
	result, err := h.EvalPartial(context.Background(), "anything", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Error("expected pass when no max param")
	}
}

func TestMaxLengthHandler_EvalPartial_MaxTokens(t *testing.T) {
	h := &MaxLengthHandler{}
	// 40 chars = ~10 tokens, max_tokens 5 => fail
	result, err := h.EvalPartial(context.Background(), "This is a test string with forty chars!!", map[string]any{
		"max":        1000,
		"max_tokens": 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsPassed() {
		t.Error("expected fail: ~10 tokens > 5 max_tokens")
	}
}

func TestMinLengthHandler_Basic(t *testing.T) {
	h := &MinLengthHandler{}
	ctx := context.Background()

	result, err := h.Eval(ctx, &evals.EvalContext{
		CurrentOutput: "hello",
	}, map[string]any{"min": 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsPassed() {
		t.Error("expected pass: 5 chars >= 3 min")
	}
}
