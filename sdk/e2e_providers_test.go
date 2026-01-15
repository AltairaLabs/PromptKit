//go:build e2e

package sdk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Provider-Specific Test Setup
//
// Creates real conversations using the SDK with actual providers.
// For mock-based tests, use the helpers in e2e_helpers_test.go.
// =============================================================================

// E2EPackDir is the directory containing test pack files.
// Set E2E_PACK_DIR to override.
func E2EPackDir() string {
	if dir := os.Getenv("E2E_PACK_DIR"); dir != "" {
		return dir
	}
	// Default to testdata directory relative to SDK
	return filepath.Join("testdata", "packs")
}

// NewProviderConversation creates a conversation with a real provider.
// Uses a minimal test pack and configures the provider based on ProviderConfig.
func NewProviderConversation(t *testing.T, provider ProviderConfig, opts ...Option) *Conversation {
	t.Helper()

	// Build pack path
	packPath := filepath.Join(E2EPackDir(), "e2e-test.pack.json")

	// Ensure pack file exists
	if _, err := os.Stat(packPath); os.IsNotExist(err) {
		t.Skipf("Test pack file not found: %s (create it or set E2E_PACK_DIR)", packPath)
	}

	// Build options based on provider
	// SDK auto-detects provider from API key and model name
	allOpts := []Option{
		WithModel(provider.DefaultModel),
	}

	// Add user-provided options
	allOpts = append(allOpts, opts...)

	conv, err := Open(packPath, "chat", allOpts...)
	require.NoError(t, err, "Failed to open conversation with provider %s", provider.ID)

	return conv
}

// NewProviderConversationWithEvents creates a conversation with event tracking.
func NewProviderConversationWithEvents(t *testing.T, provider ProviderConfig, bus *events.EventBus) *Conversation {
	t.Helper()
	return NewProviderConversation(t, provider, WithEventBus(bus))
}

// NewVisionConversation creates a conversation configured for vision tests.
func NewVisionConversation(t *testing.T, provider ProviderConfig, opts ...Option) *Conversation {
	t.Helper()

	if !provider.HasCapability(CapVision) {
		t.Skipf("Provider %s does not support vision", provider.ID)
	}

	model := provider.VisionModel
	if model == "" {
		model = provider.DefaultModel
	}

	allOpts := []Option{WithModel(model)}
	allOpts = append(allOpts, opts...)

	return NewProviderConversation(t, provider, allOpts...)
}

// =============================================================================
// Test Pack Management
// =============================================================================

// EnsureTestPacks creates minimal test pack files if they don't exist.
// This is called automatically by tests that need pack files.
func EnsureTestPacks(t *testing.T) {
	t.Helper()

	packDir := E2EPackDir()
	if err := os.MkdirAll(packDir, 0755); err != nil {
		t.Fatalf("Failed to create test pack directory: %v", err)
	}

	// Create minimal e2e test pack
	packPath := filepath.Join(packDir, "e2e-test.pack.json")
	if _, err := os.Stat(packPath); os.IsNotExist(err) {
		packContent := `{
  "version": "1.0",
  "name": "e2e-test",
  "description": "Minimal pack for e2e testing",
  "prompts": {
    "chat": {
      "system_template": "You are a helpful assistant for testing. Keep responses brief.",
      "parameters": {
        "max_tokens": 256,
        "temperature": 0.7
      }
    },
    "vision": {
      "system_template": "You are a vision assistant. Describe images briefly and accurately.",
      "parameters": {
        "max_tokens": 512,
        "temperature": 0.5
      }
    },
    "tools": {
      "system_template": "You are an assistant that uses tools when helpful.",
      "tools": ["calculator", "weather"],
      "parameters": {
        "max_tokens": 512
      }
    }
  },
  "tools": {
    "calculator": {
      "name": "calculator",
      "description": "Perform basic math calculations",
      "parameters": {
        "type": "object",
        "properties": {
          "expression": {
            "type": "string",
            "description": "Math expression to evaluate"
          }
        },
        "required": ["expression"]
      }
    },
    "weather": {
      "name": "weather",
      "description": "Get current weather for a location",
      "parameters": {
        "type": "object",
        "properties": {
          "location": {
            "type": "string",
            "description": "City name"
          }
        },
        "required": ["location"]
      }
    }
  }
}`
		if err := os.WriteFile(packPath, []byte(packContent), 0644); err != nil {
			t.Fatalf("Failed to create test pack: %v", err)
		}
		t.Logf("Created test pack: %s", packPath)
	}
}

// =============================================================================
// Provider Availability Checks
// =============================================================================

// AvailableProviders returns a summary of which providers are available.
func AvailableProviders(t *testing.T) {
	t.Helper()

	cfg := LoadE2EConfig()
	t.Logf("Available providers for e2e tests:")
	for _, p := range cfg.Providers {
		caps := make([]string, len(p.Capabilities))
		for i, c := range p.Capabilities {
			caps[i] = string(c)
		}
		t.Logf("  - %s: %v", p.ID, caps)
	}

	if len(cfg.Providers) == 0 {
		t.Log("  (no providers available - set API keys or use mock)")
	}
}
