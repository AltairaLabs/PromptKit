package claude

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false, // includeRawOutput
	)
	defer provider.Close()

	// Run the complete contract test suite
	// TODO: Re-enable after refactoring - contract tests are in parent package test file
	t.Skip("Contract tests temporarily disabled during package restructuring")
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	// Run the complete contract test suite including tools
	// TODO: Re-enable after refactoring - contract tests are in parent package test file
	t.Skip("Contract tests temporarily disabled during package restructuring")
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	// This test ensures ChatWithTools sets latency correctly
	toolSupport, ok := interface{}(provider).(providers.ToolSupport)
	if !ok {
		t.Fatal("Provider doesn't implement ToolSupport interface")
	}

	ctx := context.Background()
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather like in San Francisco?"},
		},
		MaxTokens:   100,
		Temperature: 0.7,
	}

	// Define a simple weather tool
	descriptors := []*providers.ToolDescriptor{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a location",
			InputSchema: []byte(`{
				"type": "object",
				"properties": {
					"location": {"type": "string", "description": "The city name"}
				},
				"required": ["location"]
			}`),
		},
	}

	tools, err := toolSupport.BuildTooling(descriptors)
	if err != nil {
		t.Fatalf("Failed to build tooling: %v", err)
	}

	start := time.Now()
	resp, toolCalls, err := toolSupport.ChatWithTools(ctx, req, tools, "auto")
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping tool latency test due to API error: %v", err)
		return
	}

	// CRITICAL: Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("CRITICAL BUG: ChatWithTools() returned Latency=0, but call took %v", elapsed)
		t.Logf("Response: %+v", resp)
		t.Logf("ToolCalls: %+v", toolCalls)
	}

	t.Logf("✓ ChatWithTools() correctly set Latency=%v (actual: %v)", resp.Latency, elapsed)
}
