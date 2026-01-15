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
// Text Conversation E2E Tests
//
// These tests verify basic text conversation functionality across all
// providers that support text capability.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Text
// =============================================================================

// TestE2E_Text_BasicConversation tests basic send/receive with all text providers.
func TestE2E_Text_BasicConversation(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		// Skip mock for real provider tests
		if provider.ID == "mock" {
			t.Skip("Skipping mock for real provider test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "What is 2 + 2? Reply with just the number.")
		require.NoError(t, err)
		require.NotNil(t, resp)

		text := resp.Text()
		assert.NotEmpty(t, text)
		// Most providers should return "4" somewhere in the response
		assert.Contains(t, text, "4", "Response should contain the answer")

		t.Logf("Provider %s response: %s", provider.ID, truncate(text, 100))
	})
}

// TestE2E_Text_MultiTurn tests multi-turn conversation context.
func TestE2E_Text_MultiTurn(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for context test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// First message - establish context
		resp1, err := conv.Send(ctx, "My favorite color is blue. Remember this.")
		require.NoError(t, err)
		t.Logf("Turn 1: %s", truncate(resp1.Text(), 100))

		// Second message - test context retention
		resp2, err := conv.Send(ctx, "What is my favorite color?")
		require.NoError(t, err)

		text := strings.ToLower(resp2.Text())
		assert.Contains(t, text, "blue", "Should remember the color from previous turn")

		t.Logf("Turn 2: %s", truncate(resp2.Text(), 100))
	})
}

// TestE2E_Text_Streaming tests streaming responses.
func TestE2E_Text_Streaming(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for streaming test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var chunks []StreamChunk
		var fullText strings.Builder

		for chunk := range conv.Stream(ctx, "Count from 1 to 5, one number per line.") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			chunks = append(chunks, chunk)
			if chunk.Type == ChunkText {
				fullText.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		assert.Greater(t, len(chunks), 1, "Should receive multiple chunks")
		text := fullText.String()
		assert.NotEmpty(t, text)

		// Should contain at least some of the numbers
		numbersFound := 0
		for _, n := range []string{"1", "2", "3", "4", "5"} {
			if strings.Contains(text, n) {
				numbersFound++
			}
		}
		assert.GreaterOrEqual(t, numbersFound, 3, "Should contain at least 3 of the numbers 1-5")

		t.Logf("Provider %s streamed %d chunks: %s", provider.ID, len(chunks), truncate(text, 100))
	})
}

// TestE2E_Text_ErrorHandling tests graceful error handling.
func TestE2E_Text_ErrorHandling(t *testing.T) {
	EnsureTestPacks(t)

	t.Run("cancelled_context", func(t *testing.T) {
		providers := RequireCapability(t, CapText)
		if len(providers) == 0 {
			t.Skip("No providers available")
		}

		// Use first available non-mock provider
		var provider ProviderConfig
		for _, p := range providers {
			if p.ID != "mock" {
				provider = p
				break
			}
		}
		if provider.ID == "" {
			t.Skip("No non-mock providers available")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		// Cancel context immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := conv.Send(ctx, "This should fail")
		assert.Error(t, err, "Should error with cancelled context")
	})
}

// TestE2E_Text_LongResponse tests handling of longer responses.
func TestE2E_Text_LongResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long response test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for long response test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Write a short paragraph (3-4 sentences) about the benefits of testing software.")
		require.NoError(t, err)

		text := resp.Text()
		// Should have a reasonable length response
		assert.Greater(t, len(text), 100, "Response should be at least 100 characters")

		t.Logf("Provider %s long response (%d chars): %s", provider.ID, len(text), truncate(text, 150))
	})
}

// =============================================================================
// Mock Provider Tests (Always Available)
// =============================================================================

// TestE2E_Text_Mock tests with the mock provider (no API key needed).
func TestE2E_Text_Mock(t *testing.T) {
	// These tests use the mock-based helpers from e2e_helpers_test.go
	t.Run("basic_send", func(t *testing.T) {
		conv := MustNewE2ETestConversation(t, nil)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := conv.Send(ctx, "Hello!")
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Text())
	})

	t.Run("streaming", func(t *testing.T) {
		conv := MustNewE2EStreamingConversation(t, nil)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var chunks []StreamChunk
		for chunk := range conv.Stream(ctx, "Hello!") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			chunks = append(chunks, chunk)
			if chunk.Type == ChunkDone {
				break
			}
		}
		assert.Greater(t, len(chunks), 0, "Should receive chunks")
	})
}

// =============================================================================
// Helpers
// =============================================================================

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
