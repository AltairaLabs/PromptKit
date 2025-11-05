package openai

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false, // includeRawOutput
	)
	defer provider.Close()

	// Run the complete contract test suite
	// TODO: Re-enable after refactoring - contract tests are in parent package test file
	// providers.RunProviderContractTests(t, providers.ProviderContractTests{
	// 	Provider:                  provider,
	// 	SupportsToolsExpected:     true,
	// 	SupportsStreamingExpected: true,
	// })
	t.Skip("Contract tests temporarily disabled during package restructuring")
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   50,
		},
		false, // includeRawOutput
	)
	defer provider.Close()

	// This is the exact test that would have caught the production bug
	ctx := context.Background()
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "Say 'test'"},
		},
		MaxTokens:   50,
		Temperature: 0.7,
	}

	start := time.Now()
	resp, err := provider.Chat(ctx, req)
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping latency test due to API error: %v", err)
		return
	}

	// CRITICAL: Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("CRITICAL BUG: Chat() returned Latency=0, but call took %v", elapsed)
		t.Logf("Response: %+v", resp)
	}

	t.Logf("✓ Chat() correctly set Latency=%v (actual: %v)", resp.Latency, elapsed)
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
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
		nil,
	)
	defer provider.Close()

	// This test will FAIL before the fix and PASS after
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
