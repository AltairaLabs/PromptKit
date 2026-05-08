package tts_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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

// --- PricingFromSpec tests ---

func TestPricingFromSpec_NilConfig(t *testing.T) {
	spec := tts.ProviderSpec{} // no AdditionalConfig
	if p := tts.PricingFromSpec(spec); p != nil {
		t.Errorf("expected nil for spec with no AdditionalConfig, got %v", p)
	}
}

func TestPricingFromSpec_AbsentKey(t *testing.T) {
	spec := tts.ProviderSpec{AdditionalConfig: map[string]any{"ws_url": "wss://example"}}
	if p := tts.PricingFromSpec(spec); p != nil {
		t.Errorf("expected nil when pricing key absent, got %v", p)
	}
}

func TestPricingFromSpec_NilValue(t *testing.T) {
	spec := tts.ProviderSpec{AdditionalConfig: map[string]any{"pricing": nil}}
	if p := tts.PricingFromSpec(spec); p != nil {
		t.Errorf("expected nil for nil pricing value, got %v", p)
	}
}

func TestPricingFromSpec_ValidPricing(t *testing.T) {
	pricingMap := map[string]any{
		"source":   "inline",
		"currency": "usd",
		"items": []any{
			map[string]any{"unit": "character", "rate": 0.000010},
		},
	}
	spec := tts.ProviderSpec{AdditionalConfig: map[string]any{"pricing": pricingMap}}
	p := tts.PricingFromSpec(spec)
	if p == nil {
		t.Fatal("expected non-nil PricingDescriptor")
	}
	if p.Currency != "usd" {
		t.Errorf("Currency = %q, want usd", p.Currency)
	}
	if len(p.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(p.Items))
	}
	if p.Items[0].Unit != "character" {
		t.Errorf("Items[0].Unit = %q, want character", p.Items[0].Unit)
	}
	if p.Items[0].Rate != 0.000010 {
		t.Errorf("Items[0].Rate = %v, want 0.000010", p.Items[0].Rate)
	}
}

func TestPricingFromSpec_InvalidValue(t *testing.T) {
	// A non-map value (e.g. string) should return nil without panicking.
	spec := tts.ProviderSpec{AdditionalConfig: map[string]any{"pricing": "not-a-map"}}
	// Should not panic; may return nil or a zero descriptor (both acceptable).
	_ = tts.PricingFromSpec(spec)
}

// --- YAML-driven pricing through factory ---

func TestTTSRegister_OpenAI_YAMLPricing(t *testing.T) {
	pricingMap := map[string]any{
		"source":   "inline",
		"currency": "usd",
		"items": []any{
			map[string]any{"unit": "character", "rate": 0.000005},
		},
	}
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:             "openai",
		AdditionalConfig: map[string]any{"pricing": pricingMap},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	p, ok := svc.(interface {
		Pricing() *base.PricingDescriptor
	})
	if !ok {
		t.Fatal("service does not implement Pricing()")
	}
	desc := p.Pricing()
	if desc == nil {
		t.Fatal("Pricing() returned nil")
	}
	if len(desc.Items) == 0 || desc.Items[0].Rate != 0.000005 {
		t.Errorf("overridden pricing rate = %v, want 0.000005", desc.Items)
	}
}

func TestTTSRegister_ElevenLabs_YAMLPricing(t *testing.T) {
	pricingMap := map[string]any{
		"source":   "inline",
		"currency": "usd",
		"items": []any{
			map[string]any{"unit": "character", "rate": 0.000100},
		},
	}
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:             "elevenlabs",
		AdditionalConfig: map[string]any{"pricing": pricingMap},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	p, ok := svc.(interface {
		Pricing() *base.PricingDescriptor
	})
	if !ok {
		t.Fatal("service does not implement Pricing()")
	}
	desc := p.Pricing()
	if desc == nil || len(desc.Items) == 0 || desc.Items[0].Rate != 0.000100 {
		t.Errorf("overridden pricing = %v", desc)
	}
}

func TestTTSRegister_Cartesia_YAMLPricing(t *testing.T) {
	pricingMap := map[string]any{
		"source":   "inline",
		"currency": "usd",
		"items": []any{
			map[string]any{"unit": "character", "rate": 0.000012},
		},
	}
	svc, err := tts.CreateFromSpec(tts.ProviderSpec{
		Type:             "cartesia",
		AdditionalConfig: map[string]any{"pricing": pricingMap},
	})
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	p, ok := svc.(interface {
		Pricing() *base.PricingDescriptor
	})
	if !ok {
		t.Fatal("service does not implement Pricing()")
	}
	desc := p.Pricing()
	if desc == nil || len(desc.Items) == 0 || desc.Items[0].Rate != 0.000012 {
		t.Errorf("overridden pricing = %v", desc)
	}
}
