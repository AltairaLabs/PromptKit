package guardrails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default handlers
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewGuardrailHookRejectsMissingRequiredParams(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()

	// max_length with no params — the handler's ParamValidator must reject.
	_, err := NewGuardrailHookFromRegistry("max_length", map[string]any{}, reg)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "max",
		"error should name the missing key")
}

func TestNewGuardrailHookAcceptsValidParams(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()

	hook, err := NewGuardrailHookFromRegistry(
		"max_length", map[string]any{"max_characters": 2000}, reg,
	)
	require.NoError(t, err)
	require.NotNil(t, hook)
}

func TestNewGuardrailHookAcceptsHandlersWithoutRequiredParams(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()

	// banned_words maps to content_excludes, which has no required params
	// (patterns is optional). Must succeed even with empty params.
	hook, err := NewGuardrailHookFromRegistry("banned_words", map[string]any{}, reg)
	require.NoError(t, err)
	require.NotNil(t, hook)
}

func TestNewGuardrailHookStillRejectsUnknownType(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()

	_, err := NewGuardrailHookFromRegistry("nonexistent", map[string]any{}, reg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown guardrail type")
}

func TestNewGuardrailHook_AllTypes(t *testing.T) {
	tests := []struct {
		typeName string
		params   map[string]any
	}{
		{"banned_words", map[string]any{"words": []any{"bad"}}},
		{"length", map[string]any{"max_characters": 100}},
		{"max_length", map[string]any{"max_characters": 200, "max_tokens": 50}},
		{"max_sentences", map[string]any{"max_sentences": 3}},
		{"required_fields", map[string]any{"required_fields": []any{"name"}}},
		{"content_excludes", map[string]any{"patterns": []any{"bad"}}},
		{"sentence_count", map[string]any{"max": 5}},
		{"field_presence", map[string]any{"fields": []any{"name"}}},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			h, err := NewGuardrailHook(tt.typeName, tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.Name() != tt.typeName {
				t.Errorf("Name() = %q, want %q", h.Name(), tt.typeName)
			}
			// All types should return an adapter
			if _, ok := h.(*GuardrailHookAdapter); !ok {
				t.Error("expected *GuardrailHookAdapter")
			}
		})
	}
}

func TestNewGuardrailHook_UnknownType(t *testing.T) {
	_, err := NewGuardrailHook("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestNewGuardrailHookFromRegistry_UsesRegisteredHandler(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	handler := &stubHandler{
		typeName: "custom_eval",
		result: &evals.EvalResult{
			Score: floatPtr(1.0),
		},
	}
	registry.Register(handler)

	h, err := NewGuardrailHookFromRegistry("custom_eval", map[string]any{}, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Name() != "custom_eval" {
		t.Errorf("Name() = %q, want %q", h.Name(), "custom_eval")
	}

	adapter, ok := h.(*GuardrailHookAdapter)
	if !ok {
		t.Fatal("expected *GuardrailHookAdapter")
	}

	resp := &hooks.ProviderResponse{
		Message: types.Message{Content: "test output"},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if !decision.Allow {
		t.Errorf("expected Allow, got Deny: %s", decision.Reason)
	}
}

func TestNewGuardrailHookFromRegistry_UnknownType(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()

	_, err := NewGuardrailHookFromRegistry("nonexistent", nil, registry)
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
}

func TestNewGuardrailHookFromRegistry_DirectionParam(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	handler := &stubHandler{
		typeName: "dir_test",
		result:   &evals.EvalResult{Score: floatPtr(1.0)},
	}
	registry.Register(handler)

	h, err := NewGuardrailHookFromRegistry(
		"dir_test", map[string]any{"direction": "input"}, registry,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	adapter := h.(*GuardrailHookAdapter)
	if adapter.direction != "input" {
		t.Errorf("direction = %q, want %q", adapter.direction, "input")
	}
}

func TestNewGuardrailHookFromRegistry_DefaultDirection(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	handler := &stubHandler{
		typeName: "default_dir",
		result:   &evals.EvalResult{Score: floatPtr(1.0)},
	}
	registry.Register(handler)

	h, err := NewGuardrailHookFromRegistry("default_dir", map[string]any{}, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	adapter := h.(*GuardrailHookAdapter)
	if adapter.direction != "output" {
		t.Errorf("direction = %q, want %q", adapter.direction, "output")
	}
}

func TestNewGuardrailHook_BannedWords_WordBoundaryMode(t *testing.T) {
	// banned_words alias should apply word_boundary match_mode default
	h, err := NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"bad"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	adapter := h.(*GuardrailHookAdapter)

	// Test with output containing the banned word
	resp := &hooks.ProviderResponse{
		Message: types.Message{
			Role:    "assistant",
			Content: "this is bad content",
		},
	}
	req := &hooks.ProviderRequest{
		Messages: []types.Message{resp.Message},
	}
	decision := adapter.AfterCall(context.Background(), req, resp)
	if decision.Allow {
		t.Error("expected Deny for content with banned word")
	}

	// Test with output NOT containing the banned word as a whole word
	resp2 := &hooks.ProviderResponse{
		Message: types.Message{
			Role:    "assistant",
			Content: "this is badge content",
		},
	}
	req2 := &hooks.ProviderRequest{
		Messages: []types.Message{resp2.Message},
	}
	decision2 := adapter.AfterCall(context.Background(), req2, resp2)
	if !decision2.Allow {
		t.Error("expected Allow for content without banned word as whole word")
	}
}

func TestWithMessage(t *testing.T) {
	h, err := NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"bad"},
	}, WithMessage("Custom blocked message"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	adapter := h.(*GuardrailHookAdapter)
	if adapter.message != "Custom blocked message" {
		t.Errorf("message = %q, want %q", adapter.message, "Custom blocked message")
	}
}

func TestWithMonitorOnly(t *testing.T) {
	h, err := NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"bad"},
	}, WithMonitorOnly())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	adapter := h.(*GuardrailHookAdapter)
	if !adapter.monitorOnly {
		t.Error("expected monitorOnly=true")
	}

	// Monitor-only should not modify content
	resp := &hooks.ProviderResponse{
		Message: types.Message{
			Role:    "assistant",
			Content: "this is bad content",
		},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if decision.Allow {
		t.Error("expected non-Allow decision")
	}
	if !decision.Enforced {
		t.Error("expected Enforced=true")
	}
	if resp.Message.Content != "this is bad content" {
		t.Errorf("monitor-only should not modify content, got %q", resp.Message.Content)
	}
}

func TestNewGuardrailHook_MaxLength_WithTokens(t *testing.T) {
	h, err := NewGuardrailHook("length", map[string]any{
		"max_characters": 1000,
		"max_tokens":     5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	adapter := h.(*GuardrailHookAdapter)

	// 100 chars = ~25 tokens, exceeds max_tokens of 5
	longContent := "This is a long response that has many characters and should exceed the token limit easily enough."
	resp := &hooks.ProviderResponse{
		Message: types.Message{
			Role:    "assistant",
			Content: longContent,
		},
	}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	if decision.Allow {
		t.Error("expected Deny for content exceeding token limit")
	}
}
