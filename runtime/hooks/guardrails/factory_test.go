package guardrails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers" // register default handlers
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestValidatorsToHooks_AlwaysEnforces(t *testing.T) {
	// Guardrails always rewrite on a hit — there is no observe-only mode.
	// If you want pure observation, declare an eval, not a guardrail.
	hs := ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"words": []any{"bad"}}},
	})
	require.Len(t, hs, 1)
	adapter := hs[0].(*GuardrailHookAdapter)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "this is bad"}}
	decision := adapter.AfterCall(context.Background(), nil, resp)
	require.False(t, decision.Allow)
	require.True(t, decision.Enforced)
	require.NotEqual(t, "this is bad", resp.Message.Content,
		"guardrail must rewrite content (default blocked message)")
}

func TestValidatorsToHooks_SkipsDisabled(t *testing.T) {
	disabled := false
	hs := ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "banned_words", Enabled: &disabled, Params: map[string]any{"words": []any{"bad"}}},
		{Type: "length", Params: map[string]any{"max_characters": 100}},
	})
	require.Len(t, hs, 1, "disabled validator should be dropped")
	require.Equal(t, "length", hs[0].Name())
}

func TestValidatorsToHooks_SkipsUnknownTypes(t *testing.T) {
	hs := ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "nonexistent", Params: map[string]any{}},
		{Type: "length", Params: map[string]any{"max_characters": 100}},
	})
	require.Len(t, hs, 1, "unknown validator type should be skipped, not error")
	require.Equal(t, "length", hs[0].Name())
}

func TestValidatorsToHooks_MessagePreferredOverParam(t *testing.T) {
	// Message field takes precedence; params["message"] is the fallback.
	hs := ValidatorsToHooks([]prompt.ValidatorConfig{
		{
			Type:    "banned_words",
			Params:  map[string]any{"words": []any{"bad"}, "message": "from-params"},
			Message: "from-field",
		},
	})
	require.Len(t, hs, 1)
	require.Equal(t, "from-field", hs[0].(*GuardrailHookAdapter).message)

	hs = ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"words": []any{"bad"}, "message": "from-params"}},
	})
	require.Len(t, hs, 1)
	require.Equal(t, "from-params", hs[0].(*GuardrailHookAdapter).message)
}

func TestValidatorsToHooks_EmptyReturnsNil(t *testing.T) {
	require.Nil(t, ValidatorsToHooks(nil))
	require.Nil(t, ValidatorsToHooks([]prompt.ValidatorConfig{}))
}

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

// TestNewGuardrailHook_InvalidDirection_FallsBackToOutput proves that a bad
// direction (unrecognized string, or non-string) degrades to the "output"
// default with a warning rather than erroring. Erroring here would make
// ValidatorsToHooks' warn-and-skip loop (factory.go) drop the guardrail
// entirely on a typo — fail-open on a safety check, strictly worse than the
// pre-Task-5 behavior of silently defaulting to "output".
func TestNewGuardrailHook_InvalidDirection_FallsBackToOutput(t *testing.T) {
	cases := []struct {
		name      string
		direction any
	}{
		{name: "unrecognized string", direction: "inpt"}, // typo for "input"
		{name: "non-string", direction: 42},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, err := NewGuardrailHook("length", map[string]any{
				"max_characters": 5,
				"direction":      tc.direction,
			})
			require.NoError(t, err,
				"an invalid direction must not error — construction must still succeed")

			adapter := h.(*GuardrailHookAdapter)
			require.Equal(t, DirectionOutput, adapter.direction,
				"an invalid direction must fall back to output")

			longContent := "this is way too long for the five-character limit"

			// BeforeCall must NOT gate on the fallback direction: a guardrail
			// that fell back to "output" must not start blocking user input.
			req := &hooks.ProviderRequest{
				Messages: []types.Message{{Role: roleUser, Content: longContent}},
			}
			before := adapter.BeforeCall(context.Background(), req)
			assert.True(t, before.Allow,
				"guardrail with fallback direction must not gate input")

			// AfterCall must still gate output — the guardrail keeps protecting,
			// just in the default direction.
			resp := &hooks.ProviderResponse{
				Message: types.Message{Role: "assistant", Content: longContent},
			}
			after := adapter.AfterCall(context.Background(), req, resp)
			assert.False(t, after.Allow, "guardrail with fallback direction must still gate output")
			assert.True(t, after.Enforced, "guardrail must enforce (truncate) on the output hit")
		})
	}
}

// TestNewGuardrailHook_AcceptsValidDirections pins the accepted direction
// vocabulary behaviorally. Asserting only that construction succeeds would be
// unconditionally true — since the fail-open fix the factory never errors on a
// direction at all — so dropping a case from the switch would silently degrade
// that direction to output-only (for "both", the input-side check vanishes
// entirely) with the whole suite still green.
func TestNewGuardrailHook_AcceptsValidDirections(t *testing.T) {
	// Longer than max_characters below, so any gating direction must fire.
	const longContent = "this is way too long for the five-character limit"

	cases := []struct {
		direction   string
		gatesInput  bool
		gatesOutput bool
	}{
		{direction: DirectionInput, gatesInput: true, gatesOutput: false},
		{direction: DirectionOutput, gatesInput: false, gatesOutput: true},
		{direction: DirectionBoth, gatesInput: true, gatesOutput: true},
	}

	for _, tc := range cases {
		t.Run(tc.direction, func(t *testing.T) {
			h, err := NewGuardrailHook("length", map[string]any{
				"max_characters": 5,
				"direction":      tc.direction,
			})
			require.NoError(t, err)

			adapter := h.(*GuardrailHookAdapter)
			require.Equal(t, tc.direction, adapter.direction,
				"the accepted direction must be the one the adapter gates on")

			req := &hooks.ProviderRequest{
				Messages: []types.Message{{Role: roleUser, Content: longContent}},
			}
			before := adapter.BeforeCall(context.Background(), req)
			assert.Equal(t, tc.gatesInput, !before.Allow,
				"BeforeCall gating must match the declared direction")

			resp := &hooks.ProviderResponse{
				Message: types.Message{Role: "assistant", Content: longContent},
			}
			after := adapter.AfterCall(context.Background(), req, resp)
			assert.Equal(t, tc.gatesOutput, !after.Allow,
				"AfterCall gating must match the declared direction")
		})
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
