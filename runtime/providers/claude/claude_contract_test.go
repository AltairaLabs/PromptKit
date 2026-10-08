package claude

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
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
		"claude-haiku-5-5",
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
		"claude-haiku-5-5",
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

// TestSamplingParams_ThinkingContract sends every sampling parameter alongside
// extended thinking, which the API rejects temperature and top_k with: the
// provider must withhold them.
func TestSamplingParams_ThinkingContract(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	budget := 1024
	provider := NewToolProvider("claude-thinking-sampling", "claude-haiku-5-5",
		"https://api.anthropic.com/v1", providers.ProviderDefaults{MaxTokens: 100}, false)
	provider.thinkingBudget = &budget
	defer provider.Close()
	if provider.claudeThinkingFor() == nil {
		t.Fatal("thinking must be on, or this checks the plain request again")
	}
	providers.ValidateSamplingParamsAccepted(t, provider)
}
