package vllm

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestFactoryRegistration(t *testing.T) {
	// Test that the factory is registered during init
	spec := providers.ProviderSpec{
		ID:      "test-vllm",
		Type:    "vllm",
		Model:   "test-model",
		BaseURL: "http://localhost:8000",
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("Failed to create vllm provider from spec: %v", err)
	}

	if provider == nil {
		t.Fatal("Expected provider but got nil")
	}

	// Verify it's actually a vLLM provider
	if provider.Model() != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", provider.Model())
	}
}

func TestFactoryCreatesValidProvider(t *testing.T) {
	// Test the factory function directly by calling it through the registry
	spec := providers.ProviderSpec{
		ID:      "direct-test",
		Type:    "vllm",
		Model:   "llama-3",
		BaseURL: "http://localhost:8000",
		Defaults: providers.ProviderDefaults{
			Temperature: 0.7,
			TopP:        0.9,
			MaxTokens:   1024,
		},
	}

	provider, err := providers.CreateProviderFromSpec(spec)
	if err != nil {
		t.Fatalf("CreateProviderFromSpec failed: %v", err)
	}

	if provider == nil {
		t.Fatal("Expected provider but got nil")
	}

	// Verify it's actually a vLLM provider by checking the model
	if provider.Model() != "llama-3" {
		t.Errorf("Expected model 'llama-3', got '%s'", provider.Model())
	}
}
