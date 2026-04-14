package tts_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

func TestResolveCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-env")
	cred, err := tts.ResolveCredential(context.Background(), "openai", "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := tts.APIKeyFromCredential(cred); got != "sk-env" {
		t.Errorf("got %q want sk-env", got)
	}
}

// Each test exercises every conditional branch of one provider's
// registered factory closure (model, base_url, credential, and any
// provider-specific extras). Coverage is measured per-package, so
// these need to live alongside the providers they cover.

func TestTTSRegister_OpenAI_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-stub")
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:       "openai",
		Model:      "tts-1",
		BaseURL:    "https://api.openai.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestTTSRegister_OpenAI_Defaults(t *testing.T) {
	if _, err := tts.CreateFromSpec(tts.ProviderSpec{Type: "openai"}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestTTSRegister_ElevenLabs_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("eleven-stub")
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:       "elevenlabs",
		Model:      "eleven_turbo_v2",
		BaseURL:    "https://api.elevenlabs.test",
		Credential: cred,
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestTTSRegister_ElevenLabs_Defaults(t *testing.T) {
	if _, err := tts.CreateFromSpec(tts.ProviderSpec{Type: "elevenlabs"}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestTTSRegister_Cartesia_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("cart-stub")
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:       "cartesia",
		Model:      "sonic",
		BaseURL:    "https://api.cartesia.test",
		Credential: cred,
		AdditionalConfig: map[string]any{
			"ws_url": "wss://api.cartesia.test/ws",
		},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if svc == nil {
		t.Fatal("nil service")
	}
}

func TestTTSRegister_Cartesia_Defaults(t *testing.T) {
	if _, err := tts.CreateFromSpec(tts.ProviderSpec{Type: "cartesia"}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestTTSRegister_UnknownType(t *testing.T) {
	if _, err := tts.CreateFromSpec(tts.ProviderSpec{Type: "no-such"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIKeyFromCredential_TTS(t *testing.T) {
	if got := tts.APIKeyFromCredential(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := tts.APIKeyFromCredential(&credentials.NoOpCredential{}); got != "" {
		t.Errorf("noop = %q", got)
	}
	cred := credentials.NewAPIKeyCredential("k")
	if got := tts.APIKeyFromCredential(cred); got != "k" {
		t.Errorf("apikey = %q", got)
	}
}
