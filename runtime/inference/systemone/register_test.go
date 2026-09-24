package systemone

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
)

func TestCreateFromSpec_Registered(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:         "jev",
		Type:       providerType,
		BaseURL:    "https://api.typesafe.ai/v1",
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

func TestCreateFromSpec_UsesModelAsConfiguredDefault(t *testing.T) {
	got, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:         "jev",
		Type:       providerType,
		BaseURL:    "https://api.typesafe.ai/v1",
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
		t.Errorf("model = %q, want %q", p.model, "spec-model")
	}
}

func TestCreateFromSpec_NonLoopbackWithoutCredential_Errors(t *testing.T) {
	_, err := inference.CreateFromSpec(inference.ProviderSpec{
		ID:      "jev",
		Type:    providerType,
		BaseURL: "https://api.typesafe.ai/v1",
	})
	if err == nil {
		t.Fatal("non-loopback base_url with no credential must be rejected")
	}
}

func TestCreateFromSpec_LoopbackWithoutCredential_OK(t *testing.T) {
	for _, baseURL := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://[::1]:8080",
	} {
		got, err := inference.CreateFromSpec(inference.ProviderSpec{
			ID:      "simple-jev",
			Type:    providerType,
			BaseURL: baseURL,
		})
		if err != nil {
			t.Fatalf("CreateFromSpec(%q): %v", baseURL, err)
		}
		if _, ok := got.(*Provider); !ok {
			t.Fatalf("got %T, want *Provider", got)
		}
	}
}
