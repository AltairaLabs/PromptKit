package providers

import (
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// TestOpenAIProvider_Contract runs the full provider contract test suite
// against the OpenAI provider to ensure it meets all interface requirements.
//
// This test requires OPENAI_API_KEY environment variable to be set.
// It will skip if credentials are not available.
func TestOpenAIProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI contract tests - OPENAI_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewOpenAIProvider(
		"openai-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1", // full base URL
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
		SupportsToolsExpected:     true, // OpenAI supports tools via ToolSupport interface
		SupportsStreamingExpected: true,
	})
}

// TestOpenAIProvider_LatencyBugFix is a specific regression test for the latency bug.
// This test documents the exact issue we found in production.
func TestOpenAIProvider_LatencyBugFix(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI latency test - OPENAI_API_KEY not set")
	}

	// Enable verbose logging for debugging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewOpenAIProvider(
		"openai-latency-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1", // full base URL
		ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   50,
		},
		false, // includeRawOutput
	)
	defer provider.Close()

	// This is the exact test that would have caught the production bug
	testChatReturnsLatency(t, provider)
}

// TestOpenAIToolProvider_ChatWithToolsLatency is the CRITICAL test that proves
// the production bug exists: ChatWithTools() returns Latency=0.
func TestOpenAIToolProvider_ChatWithToolsLatency(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping OpenAI tool latency test - OPENAI_API_KEY not set")
	}

	provider := NewOpenAIToolProvider(
		"openai-tool-test",
		"gpt-4o-mini",
		"https://api.openai.com/v1",
		ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
		nil,
	)
	defer provider.Close()

	// This test will FAIL before the fix and PASS after
	testChatWithToolsReturnsLatency(t, provider)
}
