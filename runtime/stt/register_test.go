package stt_test

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
)

func TestSTTRegister_OpenAI_AllOptions(t *testing.T) {
	cred := credentials.NewAPIKeyCredential("sk-stub")
	svc, err := stt.CreateFromSpec(stt.ProviderSpec{
		Type:       "openai",
		Model:      "whisper-1",
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

func TestSTTRegister_OpenAI_Defaults(t *testing.T) {
	if _, err := stt.CreateFromSpec(stt.ProviderSpec{Type: "openai"}); err != nil {
		t.Fatalf("factory: %v", err)
	}
}

func TestSTTRegister_UnknownType(t *testing.T) {
	if _, err := stt.CreateFromSpec(stt.ProviderSpec{Type: "no-such"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIKeyFromCredential_STT(t *testing.T) {
	if got := stt.APIKeyFromCredential(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	if got := stt.APIKeyFromCredential(&credentials.NoOpCredential{}); got != "" {
		t.Errorf("noop = %q", got)
	}
	cred := credentials.NewAPIKeyCredential("k")
	if got := stt.APIKeyFromCredential(cred); got != "k" {
		t.Errorf("apikey = %q", got)
	}
}

func TestResolveCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-env")
	cred, err := stt.ResolveCredential(context.Background(), "openai", "", nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := stt.APIKeyFromCredential(cred); got != "sk-env" {
		t.Errorf("got %q want sk-env", got)
	}
}

func TestPricingFromSpec_NoPricing(t *testing.T) {
	spec := stt.ProviderSpec{Type: "openai"}
	if p := stt.PricingFromSpec(spec); p != nil {
		t.Errorf("expected nil pricing, got %+v", p)
	}
}

func TestPricingFromSpec_WithPricing(t *testing.T) {
	spec := stt.ProviderSpec{
		Type: "openai",
		AdditionalConfig: map[string]any{
			"pricing": map[string]any{
				"source":   "inline",
				"currency": "usd",
				"items": []any{
					map[string]any{"unit": "second", "rate": 0.0002},
				},
			},
		},
	}
	p := stt.PricingFromSpec(spec)
	if p == nil {
		t.Fatal("expected non-nil pricing from spec")
	}
	if p.Currency != "usd" {
		t.Errorf("Currency = %q, want %q", p.Currency, "usd")
	}
	if len(p.Items) == 0 {
		t.Fatal("expected at least one price item")
	}
	if p.Items[0].Unit != "second" {
		t.Errorf("Items[0].Unit = %q, want %q", p.Items[0].Unit, "second")
	}
	if p.Items[0].Rate != 0.0002 {
		t.Errorf("Items[0].Rate = %f, want 0.0002", p.Items[0].Rate)
	}
}

func TestSTTRegister_OpenAI_WithYAMLPricing(t *testing.T) {
	spec := stt.ProviderSpec{
		Type:       "openai",
		Credential: credentials.NewAPIKeyCredential("sk-stub"),
		AdditionalConfig: map[string]any{
			"pricing": map[string]any{
				"source":   "inline",
				"currency": "usd",
				"items": []any{
					map[string]any{"unit": "second", "rate": 0.0002},
				},
			},
		},
	}
	svc, err := stt.CreateFromSpec(spec)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// The returned Service must satisfy base.STTProvider.
	p, ok := svc.(base.STTProvider)
	if !ok {
		t.Fatal("created service does not satisfy base.STTProvider")
	}
	pricing := p.Pricing()
	if pricing == nil {
		t.Fatal("expected YAML pricing, got nil")
	}
	if len(pricing.Items) == 0 || pricing.Items[0].Rate != 0.0002 {
		t.Errorf("unexpected pricing: %+v", pricing)
	}
}
