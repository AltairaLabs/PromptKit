package openai

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestOpenAIProvider_Contract runs the full provider contract test suite
// against the OpenAI base provider (no tools).
func TestOpenAIProvider_Contract(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping OpenAI contract tests - OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"openai-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
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
// against the OpenAI tool provider including tool-calling tests.
func TestToolProvider_Contract(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping OpenAI tool contract tests - OPENAI_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"openai-tool-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
		nil,
	)
	defer provider.Close()

	providers.RunProviderContractTests(t, providers.ProviderContractTests{
		Provider:                  provider,
		SupportsToolsExpected:     true,
		SupportsStreamingExpected: true,
	})
}
