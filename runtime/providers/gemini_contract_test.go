package providers

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// TestGeminiProvider_Contract runs the full provider contract test suite
// against the Gemini provider to ensure it meets all interface requirements.
//
// This test requires GEMINI_API_KEY environment variable to be set.
// It will skip if credentials are not available.
func TestGeminiProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini contract tests - GEMINI_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewGeminiProvider(
		"gemini-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta", // full base URL
		ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false, // includeRawOutput
	)
	defer provider.Close()

	// Run the complete contract test suite
	RunProviderContractTests(t, ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     false, // Tools handled by GeminiToolProvider
		SupportsStreamingExpected: true,
	})
}

// TestGeminiToolProvider_Contract tests the Gemini provider with tool support.
func TestGeminiToolProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini tool contract tests - GEMINI_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewGeminiToolProvider(
		"gemini-tool-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	// Run the complete contract test suite including tools
	RunProviderContractTests(t, ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     true, // Gemini supports tools
		SupportsStreamingExpected: true,
	})
}

// TestGeminiToolProvider_ChatWithToolsLatency verifies the latency bug fix for Gemini.
func TestGeminiToolProvider_ChatWithToolsLatency(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini tool latency test - GEMINI_API_KEY not set")
	}

	// Enable verbose logging for debugging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewGeminiToolProvider(
		"gemini-latency-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	// This test ensures ChatWithTools sets latency correctly
	testChatWithToolsReturnsLatency(t, provider)
}
