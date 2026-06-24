package selfplay

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
)

// stubSTTService is a minimal stt.Service used to verify Register/GetForProvider
// resolution without requiring a vendor API key.
type stubSTTService struct{}

func (stubSTTService) Name() string                      { return "stub-stt" }
func (stubSTTService) Type() base.ProviderType           { return base.ProviderTypeSTT }
func (stubSTTService) Pricing() *base.PricingDescriptor  { return nil }
func (stubSTTService) Validate() error                   { return nil }
func (stubSTTService) Init(context.Context) error        { return nil }
func (stubSTTService) HealthCheck(context.Context) error { return nil }
func (stubSTTService) Close() error                      { return nil }
func (stubSTTService) SupportedFormats() []string        { return []string{stt.FormatPCM} }

func (stubSTTService) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: "stub"}, nil
}

func (stubSTTService) TranscribeBytes(context.Context, []byte, stt.TranscriptionConfig) (string, error) {
	return "stub", nil
}

func TestSTTRegistry_Register_TakesPrecedence(t *testing.T) {
	r := NewSTTRegistry()
	r.Register("fake", stubSTTService{})

	svc, err := r.GetForProvider(&config.Provider{ID: "x", Type: "fake", Role: config.RoleSTT})
	if err != nil {
		t.Fatalf("expected registered service, got error: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil registered stt.Service")
	}
	if svc.Name() != "stub-stt" {
		t.Fatalf("expected stub-stt, got %q", svc.Name())
	}
}

func TestSTTRegistry_Register_RejectsWrongRole(t *testing.T) {
	r := NewSTTRegistry()
	r.Register("fake", stubSTTService{})
	// Role gate runs before the preloaded lookup, so a non-stt role still errors.
	if _, err := r.GetForProvider(&config.Provider{ID: "x", Type: "fake", Role: config.RoleLLM}); err == nil {
		t.Fatal("expected role error even for a registered type")
	}
}

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
