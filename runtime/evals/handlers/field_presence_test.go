package handlers

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestFieldPresenceHandler_Type(t *testing.T) {
	h := &FieldPresenceHandler{}
	if h.Type() != "field_presence" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestFieldPresenceHandler_AllFound(t *testing.T) {
	h := &FieldPresenceHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "Name: Alice\nEmail: alice@example.com"},
		map[string]any{"fields": []any{"Name", "Email"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected score 1.0, got %v", result.Score)
	}
}

func TestFieldPresenceHandler_SomeMissing(t *testing.T) {
	h := &FieldPresenceHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "Name: Alice"},
		map[string]any{"fields": []any{"Name", "Email", "Phone"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil && *result.Score >= 1.0 {
		t.Fatal("expected fail: Email and Phone are missing")
	}
	if result.Score == nil {
		t.Fatal("expected score to be set")
	}
	// 1 out of 3 found
	expected := 1.0 / 3.0
	if *result.Score < expected-0.01 || *result.Score > expected+0.01 {
		t.Fatalf("expected score ~%.3f, got %f", expected, *result.Score)
	}
	missing := result.Details["missing"].([]string)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing fields, got %d", len(missing))
	}
}

func TestFieldPresenceHandler_NoFields(t *testing.T) {
	h := &FieldPresenceHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "anything"},
		map[string]any{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatal("expected pass when no fields to check")
	}
}

func TestFieldPresenceHandler_CaseInsensitive(t *testing.T) {
	h := &FieldPresenceHandler{}
	result, err := h.Eval(
		context.Background(),
		&evals.EvalContext{CurrentOutput: "name: alice\nemail: test@test.com"},
		map[string]any{"fields": []any{"Name", "EMAIL"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !(result.Score != nil && *result.Score >= 1.0) {
		t.Fatalf("expected pass with case-insensitive match: %s", result.Explanation)
	}
}
