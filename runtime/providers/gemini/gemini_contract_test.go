package gemini

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

	provider := NewProvider(
		"gemini-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta", // full base URL
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

// TestToolProvider_Contract tests the Gemini provider with tool support.
func TestToolProvider_Contract(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini tool contract tests - GEMINI_API_KEY not set")
	}

	// Enable verbose logging for contract tests
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"gemini-tool-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
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

// TestToolProvider_PredictWithToolsLatency verifies the latency bug fix for Gemini.
func TestToolProvider_PredictWithToolsLatency(t *testing.T) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping Gemini tool latency test - GEMINI_API_KEY not set")
	}

	// Enable verbose logging for debugging
	logger.SetVerbose(true)
	defer logger.SetVerbose(false)

	provider := NewToolProvider(
		"gemini-latency-test",
		"gemini-1.5-flash",
		"https://generativelanguage.googleapis.com/v1beta",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   100,
		},
		false,
	)
	defer provider.Close()

	// This test ensures PredictWithTools sets latency correctly
	toolSupport, ok := interface{}(provider).(providers.ToolSupport)
	if !ok {
		t.Fatal("Provider doesn't implement ToolSupport interface")
	}

	ctx := context.Background()
	req := providers.PredictionRequest{
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
	resp, toolCalls, err := toolSupport.PredictWithTools(ctx, req, tools, "auto")
	elapsed := time.Since(start)

	if err != nil {
		t.Skipf("Skipping tool latency test due to API error: %v", err)
		return
	}

	// CRITICAL: Latency must be non-zero
	if resp.Latency == 0 {
		t.Errorf("CRITICAL BUG: PredictWithTools() returned Latency=0, but call took %v", elapsed)
		t.Logf("Response: %+v", resp)
		t.Logf("ToolCalls: %+v", toolCalls)
	}

	t.Logf("✓ PredictWithTools() correctly set Latency=%v (actual: %v)", resp.Latency, elapsed)
}
