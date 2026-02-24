package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGuardrailTriggeredHandler_Type(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	if h.Type() != "guardrail_triggered" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestGuardrailTriggeredHandler_TriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{
				Role: "assistant",
				Validations: []types.ValidationResult{
					{ValidatorType: "banned_words", Passed: false},
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggeredHandler_NotTriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{
				Role: "assistant",
				Validations: []types.ValidationResult{
					{ValidatorType: "banned_words", Passed: true},
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggeredHandler_ExpectedTriggerButPassed(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{
				Role: "assistant",
				Validations: []types.ValidationResult{
					{ValidatorType: "banned_words", Passed: true},
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when trigger expected but validator passed")
	}
}

func TestGuardrailTriggeredHandler_ValidatorNotFound(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant"},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail when validator not found but expected to trigger")
	}
}

func TestGuardrailTriggeredHandler_FriendlyNameMatch(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{
				Role: "assistant",
				Validations: []types.ValidationResult{
					{ValidatorType: "*validators.BannedWordsValidator", Passed: false},
				},
			},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("expected pass with friendly name match: %s", result.Explanation)
	}
}

func TestGuardrailTriggeredHandler_NoValidatorType(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("expected fail with no validator_type")
	}
}

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"banned_words", "BannedWords"},
		{"max_length", "MaxLength"},
		{"simple", "Simple"},
		{"", ""},
	}
	for _, tt := range tests {
		got := snakeToPascal(tt.input)
		if got != tt.expected {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
