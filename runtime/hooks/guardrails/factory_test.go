package guardrails

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

func TestNewGuardrailHook_AllTypes(t *testing.T) {
	tests := []struct {
		typeName string
		params   map[string]any
		wantName string
	}{
		{"banned_words", map[string]any{"words": []any{"bad"}}, "banned_words"},
		{"length", map[string]any{"max_characters": 100}, "length"},
		{"max_length", map[string]any{"max_tokens": 50}, "length"},
		{"max_sentences", map[string]any{"max_sentences": 3}, "max_sentences"},
		{"required_fields", map[string]any{"required_fields": []any{"name"}}, "required_fields"},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			h, err := NewGuardrailHook(tt.typeName, tt.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h.Name() != tt.wantName {
				t.Errorf("Name() = %q, want %q", h.Name(), tt.wantName)
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

func TestNewGuardrailHook_BannedWords_ParamTypes(t *testing.T) {
	// []any (from JSON unmarshal)
	h1, _ := NewGuardrailHook("banned_words", map[string]any{
		"words": []any{"bad", "evil"},
	})
	bw1 := h1.(*BannedWordsHook)
	if len(bw1.words) != 2 {
		t.Errorf("expected 2 words from []any, got %d", len(bw1.words))
	}

	// []string (from direct construction)
	h2, _ := NewGuardrailHook("banned_words", map[string]any{
		"words": []string{"bad", "evil"},
	})
	bw2 := h2.(*BannedWordsHook)
	if len(bw2.words) != 2 {
		t.Errorf("expected 2 words from []string, got %d", len(bw2.words))
	}

	// No words param
	h3, _ := NewGuardrailHook("banned_words", map[string]any{})
	bw3 := h3.(*BannedWordsHook)
	if len(bw3.words) != 0 {
		t.Errorf("expected 0 words from empty params, got %d", len(bw3.words))
	}
}

func TestNewGuardrailHook_Length_ParamTypes(t *testing.T) {
	// int params
	h1, _ := NewGuardrailHook("length", map[string]any{
		"max_characters": 100,
		"max_tokens":     50,
	})
	lh1 := h1.(*LengthHook)
	if lh1.maxCharacters != 100 || lh1.maxTokens != 50 {
		t.Errorf("int params: maxChars=%d maxTokens=%d, want 100/50", lh1.maxCharacters, lh1.maxTokens)
	}

	// float64 params (from JSON unmarshal)
	h2, _ := NewGuardrailHook("length", map[string]any{
		"max_characters": float64(200),
		"max_tokens":     float64(75),
	})
	lh2 := h2.(*LengthHook)
	if lh2.maxCharacters != 200 || lh2.maxTokens != 75 {
		t.Errorf("float64 params: maxChars=%d maxTokens=%d, want 200/75", lh2.maxCharacters, lh2.maxTokens)
	}
}

func TestNewGuardrailHook_RequiredFields_ParamTypes(t *testing.T) {
	// []any
	h1, _ := NewGuardrailHook("required_fields", map[string]any{
		"required_fields": []any{"name", "email"},
	})
	rf1 := h1.(*RequiredFieldsHook)
	if len(rf1.requiredFields) != 2 {
		t.Errorf("expected 2 fields from []any, got %d", len(rf1.requiredFields))
	}

	// []string
	h2, _ := NewGuardrailHook("required_fields", map[string]any{
		"required_fields": []string{"name"},
	})
	rf2 := h2.(*RequiredFieldsHook)
	if len(rf2.requiredFields) != 1 {
		t.Errorf("expected 1 field from []string, got %d", len(rf2.requiredFields))
	}
}

func TestNewGuardrailHook_StreamingInterfaces(t *testing.T) {
	streaming := []string{"banned_words", "length", "max_length"}
	for _, name := range streaming {
		h, err := NewGuardrailHook(name, map[string]any{})
		if err != nil {
			t.Fatalf("type %q: %v", name, err)
		}
		if _, ok := h.(hooks.ChunkInterceptor); !ok {
			t.Errorf("type %q should implement ChunkInterceptor", name)
		}
	}

	nonStreaming := []string{"max_sentences", "required_fields"}
	for _, name := range nonStreaming {
		h, err := NewGuardrailHook(name, map[string]any{})
		if err != nil {
			t.Fatalf("type %q: %v", name, err)
		}
		if _, ok := h.(hooks.ChunkInterceptor); ok {
			t.Errorf("type %q should NOT implement ChunkInterceptor", name)
		}
	}
}
