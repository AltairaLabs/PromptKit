package selfplay

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestSTTRegistry_GetForProvider_RejectsNonSTTRole(t *testing.T) {
	r := NewSTTRegistry()
	_, err := r.GetForProvider(&config.Provider{ID: "x", Type: "openai", Role: config.RoleLLM})
	if err == nil {
		t.Fatal("expected error for non-stt role, got nil")
	}
}

func TestSTTRegistry_GetForProvider_NilProvider(t *testing.T) {
	r := NewSTTRegistry()
	if _, err := r.GetForProvider(nil); err == nil {
		t.Fatal("expected error for nil provider")
	}
}

func TestSTTRegistry_GetForProvider_UnsupportedType(t *testing.T) {
	r := NewSTTRegistry()
	_, err := r.GetForProvider(&config.Provider{ID: "x", Type: "nope", Role: config.RoleSTT})
	if err == nil {
		t.Fatal("expected error for unsupported stt type")
	}
}

func TestSTTRegistry_GetForProvider_OpenAI_NoAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	r := NewSTTRegistry()
	_, err := r.GetForProvider(&config.Provider{ID: "x", Type: "openai", Role: config.RoleSTT})
	if err == nil {
		t.Fatal("expected error when OPENAI_API_KEY is unset")
	}
}

func TestSTTRegistry_GetForProvider_OpenAI_WithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	r := NewSTTRegistry()
	svc, err := r.GetForProvider(&config.Provider{ID: "x", Type: "openai", Role: config.RoleSTT})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil stt.Service")
	}
}

func TestSTTRegistry_GetForProvider_OpenAI_WithModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	r := NewSTTRegistry()
	svc, err := r.GetForProvider(&config.Provider{ID: "x", Type: "openai", Role: config.RoleSTT, Model: "whisper-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil stt.Service")
	}
}
