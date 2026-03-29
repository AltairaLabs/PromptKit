package claude

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TestClaudeProvider_Contract runs the full provider contract test suite
// against the Claude base provider (no tools).
func TestClaudeProvider_Contract(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping Claude contract tests - ANTHROPIC_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewProvider(
		"claude-test",
		"claude-haiku-4-5-20251001",
		"https://api.anthropic.com/v1",
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
// against the Claude tool provider including tool-calling tests.
func TestToolProvider_Contract(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping Claude tool contract tests - ANTHROPIC_API_KEY not set")
	}

	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"claude-tool-test",
		"claude-haiku-4-5-20251001",
		"https://api.anthropic.com/v1",
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
