//go:build e2e

package sdk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Tool Execution E2E Tests
//
// These tests verify tool/function calling capabilities across providers.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Tools
// =============================================================================

// TestE2E_Tools_BasicToolCall tests basic tool invocation.
func TestE2E_Tools_BasicToolCall(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapTools, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider uses different tool setup")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		// Register a simple calculator tool
		calculatorCalled := false
		conv.OnTool("calculator", func(args map[string]any) (any, error) {
			calculatorCalled = true
			expr, _ := args["expression"].(string)
			t.Logf("Calculator called with: %s", expr)

			// Simple evaluation for testing
			if strings.Contains(expr, "+") {
				return map[string]any{"result": 4}, nil
			}
			return map[string]any{"result": "evaluated"}, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Use the calculator tool to compute 2+2")
		require.NoError(t, err)

		// The tool should have been called
		assert.True(t, calculatorCalled, "Calculator tool should have been called")

		// Response should mention the result
		text := resp.Text()
		assert.NotEmpty(t, text)
		t.Logf("Provider %s tool response: %s", provider.ID, truncate(text, 150))
	})
}

// TestE2E_Tools_ToolWithComplexArgs tests tools with complex arguments.
func TestE2E_Tools_ToolWithComplexArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping complex args test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapTools, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider uses different tool setup")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		var receivedArgs map[string]any
		conv.OnTool("weather", func(args map[string]any) (any, error) {
			receivedArgs = args
			location, _ := args["location"].(string)
			t.Logf("Weather called for: %s", location)

			return map[string]any{
				"location":    location,
				"temperature": 72,
				"conditions":  "sunny",
				"humidity":    45,
			}, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "What's the weather like in San Francisco?")
		require.NoError(t, err)

		// Tool should have been called with location
		require.NotNil(t, receivedArgs, "Weather tool should have been called")

		location, ok := receivedArgs["location"].(string)
		if ok {
			assert.True(t,
				strings.Contains(strings.ToLower(location), "san francisco") ||
					strings.Contains(strings.ToLower(location), "sf"),
				"Should pass San Francisco as location")
		}

		// Response should include weather info
		text := strings.ToLower(resp.Text())
		assert.True(t,
			strings.Contains(text, "72") ||
				strings.Contains(text, "sunny") ||
				strings.Contains(text, "weather"),
			"Response should include weather information")

		t.Logf("Provider %s weather response: %s", provider.ID, truncate(resp.Text(), 150))
	})
}

// TestE2E_Tools_MultipleToolCalls tests multiple tool invocations.
func TestE2E_Tools_MultipleToolCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multiple tools test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapTools, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider uses different tool setup")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		calculatorCalls := 0
		conv.OnTool("calculator", func(args map[string]any) (any, error) {
			calculatorCalls++
			return map[string]any{"result": 10}, nil
		})

		weatherCalls := 0
		conv.OnTool("weather", func(args map[string]any) (any, error) {
			weatherCalls++
			return map[string]any{"temperature": 65, "conditions": "cloudy"}, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "What's 5+5, and what's the weather in NYC?")
		require.NoError(t, err)

		// At least one tool should have been called
		totalCalls := calculatorCalls + weatherCalls
		assert.GreaterOrEqual(t, totalCalls, 1, "At least one tool should be called")

		t.Logf("Provider %s: calculator=%d, weather=%d calls. Response: %s",
			provider.ID, calculatorCalls, weatherCalls, truncate(resp.Text(), 150))
	})
}

// TestE2E_Tools_ToolError tests handling of tool execution errors.
func TestE2E_Tools_ToolError(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapTools, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider uses different tool setup")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		toolCalled := false
		conv.OnTool("calculator", func(args map[string]any) (any, error) {
			toolCalled = true
			// Return an error result (not a Go error, but an error in the response)
			return map[string]any{
				"error":   true,
				"message": "Division by zero is not allowed",
			}, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Use the calculator to divide 10 by 0")
		require.NoError(t, err)

		// Tool should have been called
		if toolCalled {
			// Response should handle the error gracefully
			text := strings.ToLower(resp.Text())
			t.Logf("Provider %s error handling: %s", provider.ID, truncate(resp.Text(), 150))

			// Model should acknowledge the error somehow
			assert.True(t,
				strings.Contains(text, "error") ||
					strings.Contains(text, "cannot") ||
					strings.Contains(text, "not possible") ||
					strings.Contains(text, "division") ||
					strings.Contains(text, "zero"),
				"Response should acknowledge the error")
		}
	})
}

// TestE2E_Tools_JSONMode tests JSON output mode.
// TODO: Implement when WithResponseFormat option is available
func TestE2E_Tools_JSONMode(t *testing.T) {
	t.Skip("JSON mode test requires WithResponseFormat option - implement when available")
}

// =============================================================================
// Mock Tool Tests
// =============================================================================

// TestE2E_Tools_MockProvider tests tool handling with mock provider.
func TestE2E_Tools_MockProvider(t *testing.T) {
	// Use the mock-based helpers that have tool support built in
	conv := MustNewE2ETestConversation(t, nil)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Mock provider should handle basic requests
	resp, err := conv.Send(ctx, "Hello!")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}
