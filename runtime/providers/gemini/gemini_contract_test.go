package gemini

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestGeminiProvider_Contract runs the full provider contract test suite
// against the Gemini base provider (no tools).
func TestGeminiProvider_Contract(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("Skipping Gemini contract tests - GEMINI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"gemini-test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	providers.RunProviderContractTests(t, providers.ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     false,
		SupportsStreamingExpected: true,
	})
}

// TestToolProvider_Contract runs the full provider contract test suite
// against the Gemini tool provider including tool-calling tests.
func TestToolProvider_Contract(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("Skipping Gemini tool contract tests - GEMINI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"gemini-tool-test",
		"gemini-2.0-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	providers.RunProviderContractTests(t, providers.ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     true,
		SupportsStreamingExpected: true,
	})
}
