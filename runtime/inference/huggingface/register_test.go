package huggingface

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
)

func TestCreateFromSpec_Registered(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:         "hf",
		Type:       providerType,
		Credential: credentials.NewAPIKeyCredential("tok"),
	})
	if err != nil {
		t.Fatalf("CreateFromSpec: %v", err)
	}
	p, ok := got.(*Provider)
	if !ok {
		t.Fatalf("got %T, want *Provider", got)
	}
	if p.apiKey != "tok" {
		t.Errorf("apiKey = %q, want %q", p.apiKey, "tok")
	}
}

func TestCreateFromSpec_DedicatedFlag(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:               "hf",
		Type:             providerType,
		BaseURL:          "https://x.endpoints.huggingface.cloud",
		Credential:       credentials.NewAPIKeyCredential("tok"),
		AdditionalConfig: map[string]any{"dedicated": true},
	})
	if err != nil {
		t.Fatalf("CreateFromSpec with dedicated: %v", err)
	}
	p, ok := got.(*Provider)
	if !ok {
		t.Fatalf("got %T, want *Provider", got)
	}
	if !p.dedicated {
		t.Error("dedicated flag was not honored")
	}
}

func TestCreateFromSpec_UsesModelAsConfiguredDefault(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:         "hf",
		Type:       providerType,
		Model:      "spec-model",
		Credential: credentials.NewAPIKeyCredential("tok"),
	})
	if err != nil {
		t.Fatalf("CreateFromSpec: %v", err)
	}
	p, ok := got.(*Provider)
	if !ok {
		t.Fatalf("got %T, want *Provider", got)
	}
	if p.model != "spec-model" {
		t.Errorf("model = %q, want %q (the old factory ignored spec.Model; this one must use it)", p.model, "spec-model")
	}
}

func TestCreateFromSpec_RequiresAPIKey(t *testing.T) {
	_, err := inference.CreateFromSpec(inference.ProviderSpec{ID: "hf", Type: providerType})
	if err == nil {
		t.Fatal("missing credential must be rejected")
	}
}
