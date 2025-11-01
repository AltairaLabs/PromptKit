package providers

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// TestClaudeProvider_Contract runs the full provider contract test suite
// against the Claude provider to ensure it meets all interface requirements.
//
// This test requires ANTHROPIC_API_KEY environment variable to be set.
// It will skip if credentials are not available.
func TestClaudeProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Claude contract tests - ANTHROPIC_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewClaudeProvider(
		"claude-test",
		"claude-3-5-haiku-20241022",
		"https://api.anthropic.com/v1", // full base URL
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
		SupportsToolsExpected:     false, // Tools handled by ClaudeToolProvider
		SupportsStreamingExpected: true,
	})
}

// TestClaudeToolProvider_Contract tests the Claude provider with tool support.
func TestClaudeToolProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Claude tool contract tests - ANTHROPIC_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewClaudeToolProvider(
		"claude-tool-test",
		"claude-3-5-haiku-20241022",
		"https://api.anthropic.com/v1",
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
		SupportsToolsExpected:     true, // Claude supports tools
		SupportsStreamingExpected: true,
	})
}

// TestClaudeToolProvider_ChatWithToolsLatency verifies the latency bug fix for Claude.
func TestClaudeToolProvider_ChatWithToolsLatency(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Claude tool latency test - ANTHROPIC_API_KEY not set")
	}

	// Enable verbose logging for debugging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewClaudeToolProvider(
		"claude-latency-test",
		"claude-3-5-haiku-20241022",
		"https://api.anthropic.com/v1",
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
