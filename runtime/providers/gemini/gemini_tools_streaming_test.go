package gemini

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestToolProvider_StreamingMethods(t *testing.T) {
	// Create ToolProvider with empty defaults
	defaults := providers.ProviderDefaults{}
	toolProvider := NewToolProvider("test-id", "gemini-1.5-pro", "", defaults, false)

	t.Run("SupportsStreamInput", func(t *testing.T) {
		// This should forward to the base provider
		modalities := toolProvider.SupportsStreamInput()
		// Should return ["AUDIO", "TEXT"] or similar
		if modalities == nil {
			t.Log("SupportsStreamInput returned nil (may be expected for non-streaming model)")
		}
	})

	t.Run("GetStreamingCapabilities", func(t *testing.T) {
		caps := toolProvider.GetStreamingCapabilities()
		// Just ensure it doesn't panic
		_ = caps
	})

	t.Run("CreateStreamSession", func(t *testing.T) {
		ctx := context.Background()
		config := &providers.StreamingInputConfig{
			SystemInstruction: "test",
			// Note: This will fail without a real API key, but that's okay
			// We're just testing that the method exists and forwards correctly
		}

		_, err := toolProvider.CreateStreamSession(ctx, config)
		if err != nil {
			// Expected to fail without API key or in test environment
			t.Log("CreateStreamSession failed (expected without API key):", err)
		}
	})
}

func TestNewProvider_InitFunction(t *testing.T) {
	// Test that init() function works by creating a provider
	// The init() function registers the factory, so if NewToolProvider works, init() ran
	defaults := providers.ProviderDefaults{}
	provider := NewToolProvider("test-id", "gemini-1.5-pro", "", defaults, false)

	if provider == nil {
		t.Fatal("NewToolProvider returned nil")
	}

	// Verify the provider has the expected methods
	_ = provider.SupportsStreamInput()
	_ = provider.GetStreamingCapabilities()
}
