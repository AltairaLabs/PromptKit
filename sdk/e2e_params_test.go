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
// System Prompt and Parameter E2E Tests
//
// These tests verify that system prompts and model parameters are properly
// applied and affect model behavior as expected.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Params
// =============================================================================

// TestE2E_Params_SystemPromptBehavior tests that system prompts affect behavior.
func TestE2E_Params_SystemPromptBehavior(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't respect system prompts")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// The test pack has system prompt: "You are a helpful assistant for testing. Keep responses brief."
		resp, err := conv.Send(ctx, "Tell me about the weather")
		require.NoError(t, err)

		text := resp.Text()
		// Response should be reasonably brief due to system prompt
		assert.Less(t, len(text), 500, "Response should be brief due to system prompt")

		t.Logf("Provider %s brief response (%d chars): %s", provider.ID, len(text), truncate(text, 100))
	})
}

// TestE2E_Params_ContextWindow tests that conversation history is maintained.
func TestE2E_Params_ContextWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping context window test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't maintain real context")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		// Build up conversation context
		facts := []string{
			"The capital of France is Paris.",
			"The largest planet is Jupiter.",
			"Water boils at 100 degrees Celsius.",
		}

		for _, fact := range facts {
			_, err := conv.Send(ctx, "Remember this fact: "+fact)
			require.NoError(t, err)
		}

		// Test recall of earlier facts
		resp, err := conv.Send(ctx, "What is the capital of France?")
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		assert.Contains(t, text, "paris", "Should recall fact from conversation history")

		t.Logf("Provider %s context recall: %s", provider.ID, truncate(resp.Text(), 100))
	})
}

// TestE2E_Params_ConsistentResponses tests response consistency.
func TestE2E_Params_ConsistentResponses(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider returns deterministic responses")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Send same question multiple times to different conversations
		question := "What is 2+2? Reply with just the number."
		responses := make([]string, 3)

		for i := 0; i < 3; i++ {
			conv := NewProviderConversation(t, provider)
			resp, err := conv.Send(ctx, question)
			conv.Close()

			require.NoError(t, err)
			responses[i] = resp.Text()
		}

		// All responses should contain "4"
		for i, resp := range responses {
			assert.Contains(t, resp, "4", "Response %d should contain the answer", i+1)
			t.Logf("Provider %s response %d: %s", provider.ID, i+1, truncate(resp, 50))
		}
	})
}

// TestE2E_Params_RoleHandling tests that user/assistant roles are handled correctly.
func TestE2E_Params_RoleHandling(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't simulate roles")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// The model should respond as an assistant, not echo back user input
		resp, err := conv.Send(ctx, "Hello, I am a user.")
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Response should be from assistant perspective
		assert.True(t,
			strings.Contains(text, "hello") ||
				strings.Contains(text, "hi") ||
				strings.Contains(text, "help") ||
				strings.Contains(text, "assist"),
			"Response should be a greeting or offer to help")

		t.Logf("Provider %s role response: %s", provider.ID, truncate(resp.Text(), 100))
	})
}

// TestE2E_Params_LongInput tests handling of longer input messages.
func TestE2E_Params_LongInput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long input test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't process real input")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Create a longer input message
		longInput := "Please summarize the following text: " + strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20) + " What animal was mentioned first?"

		resp, err := conv.Send(ctx, longInput)
		require.NoError(t, err)

		text := strings.ToLower(resp.Text())
		// Should mention the fox
		assert.True(t,
			strings.Contains(text, "fox") ||
				strings.Contains(text, "brown") ||
				strings.Contains(text, "quick"),
			"Should process the long input and identify the fox")

		t.Logf("Provider %s long input response: %s", provider.ID, truncate(resp.Text(), 100))
	})
}

// TestE2E_Params_SpecialCharacters tests handling of special characters in input.
func TestE2E_Params_SpecialCharacters(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't process real input")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Test with special characters, unicode, and emojis
		input := "What does this symbol mean: @? Also, what about these: #, $, %, &?"

		resp, err := conv.Send(ctx, input)
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text, "Should handle special characters in input")

		// Should provide some explanation
		assert.Greater(t, len(text), 10, "Should provide meaningful response about symbols")

		t.Logf("Provider %s special chars response: %s", provider.ID, truncate(text, 100))
	})
}

// TestE2E_Params_Unicode tests handling of unicode text.
func TestE2E_Params_Unicode(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't process real input")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Test with various unicode
		input := "Translate 'hello' to Japanese, French, and Spanish."

		resp, err := conv.Send(ctx, input)
		require.NoError(t, err)

		text := resp.Text()
		assert.NotEmpty(t, text, "Should handle unicode response")

		// Should contain translations (which will be unicode)
		t.Logf("Provider %s unicode response: %s", provider.ID, truncate(text, 150))
	})
}
