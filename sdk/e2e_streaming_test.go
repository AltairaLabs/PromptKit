//go:build e2e

package sdk

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Streaming E2E Tests
//
// These tests verify streaming functionality across all providers that support
// streaming capability. They test chunk delivery, content accumulation, and
// token tracking during streaming.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Streaming
// =============================================================================

// TestE2E_Streaming_BasicStream tests basic streaming response delivery.
func TestE2E_Streaming_BasicStream(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for real streaming test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var chunks []StreamChunk
		var fullText strings.Builder
		var firstChunkTime time.Time
		startTime := time.Now()

		for chunk := range conv.Stream(ctx, "Say hello in exactly 5 words.") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}

			if firstChunkTime.IsZero() && chunk.Type == ChunkText {
				firstChunkTime = time.Now()
			}

			chunks = append(chunks, chunk)
			if chunk.Type == ChunkText {
				fullText.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		// Verify we got multiple chunks (streaming is working)
		assert.Greater(t, len(chunks), 1, "Should receive multiple chunks for streaming")

		// Verify content was received
		text := fullText.String()
		assert.NotEmpty(t, text, "Should receive text content")

		// Time to first chunk should be reasonable (not waiting for full response)
		if !firstChunkTime.IsZero() {
			ttfc := firstChunkTime.Sub(startTime)
			t.Logf("Provider %s: %d chunks, TTFC: %v, text: %s",
				provider.ID, len(chunks), ttfc, truncate(text, 80))
		}
	})
}

// TestE2E_Streaming_LongResponse tests streaming of longer responses.
func TestE2E_Streaming_LongResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long streaming test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for long streaming test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var chunks []StreamChunk
		var fullText strings.Builder

		for chunk := range conv.Stream(ctx, "Write a haiku about programming, then explain each line briefly.") {
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

		text := fullText.String()

		// Should have substantial content
		assert.Greater(t, len(text), 50, "Long response should have substantial content")

		// Should have many chunks for longer content
		assert.Greater(t, len(chunks), 5, "Long response should have many chunks")

		t.Logf("Provider %s: %d chunks, %d chars", provider.ID, len(chunks), len(text))
	})
}

// TestE2E_Streaming_TokenTracking tests that token counts are tracked during streaming.
func TestE2E_Streaming_TokenTracking(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for token tracking test")
		}

		// Create event bus to capture token info
		bus := events.NewEventBus()
		var completedData *events.ProviderCallCompletedData
		var mu sync.Mutex

		bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
			mu.Lock()
			if data, ok := e.Data.(*events.ProviderCallCompletedData); ok {
				completedData = data
			}
			mu.Unlock()
		})

		conv := NewProviderConversationWithEvents(t, provider, bus)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Stream a response
		var fullText strings.Builder
		for chunk := range conv.Stream(ctx, "What is 2+2? Reply with just the answer.") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			if chunk.Type == ChunkText {
				fullText.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		// Allow time for event processing
		time.Sleep(100 * time.Millisecond)

		// Verify token counts were captured
		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, completedData, "Should receive ProviderCallCompleted event")

		// Token counts should be non-zero for real providers
		totalTokens := completedData.InputTokens + completedData.OutputTokens
		assert.Greater(t, totalTokens, 0, "Should have non-zero token count")

		t.Logf("Provider %s streaming tokens - Input: %d, Output: %d, Cost: $%.6f",
			provider.ID, completedData.InputTokens, completedData.OutputTokens, completedData.Cost)
	})
}

// TestE2E_Streaming_Cancellation tests that streaming can be cancelled mid-stream.
func TestE2E_Streaming_Cancellation(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for cancellation test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		var chunksReceived int

		// Start streaming and cancel after a few chunks
		for chunk := range conv.Stream(ctx, "Write a very long story about a brave knight who goes on many adventures.") {
			chunksReceived++

			// Cancel after receiving some chunks
			if chunksReceived >= 3 {
				cancel()
			}

			if chunk.Error != nil {
				// Expected - context was cancelled
				break
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		// We should have received at least a few chunks before cancellation
		assert.GreaterOrEqual(t, chunksReceived, 1, "Should receive at least one chunk before cancellation")

		t.Logf("Provider %s: received %d chunks before cancellation", provider.ID, chunksReceived)
	})
}

// TestE2E_Streaming_MultiTurn tests streaming in multi-turn conversations.
func TestE2E_Streaming_MultiTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-turn streaming test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for multi-turn streaming test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// First turn - establish context
		var turn1Text strings.Builder
		for chunk := range conv.Stream(ctx, "My name is Alice. Remember this.") {
			if chunk.Error != nil {
				t.Fatalf("Turn 1 stream error: %v", chunk.Error)
			}
			if chunk.Type == ChunkText {
				turn1Text.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}
		t.Logf("Turn 1: %s", truncate(turn1Text.String(), 80))

		// Second turn - verify context is maintained
		var turn2Text strings.Builder
		for chunk := range conv.Stream(ctx, "What is my name?") {
			if chunk.Error != nil {
				t.Fatalf("Turn 2 stream error: %v", chunk.Error)
			}
			if chunk.Type == ChunkText {
				turn2Text.WriteString(chunk.Text)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		text := strings.ToLower(turn2Text.String())
		assert.Contains(t, text, "alice", "Should remember name from previous turn")

		t.Logf("Turn 2: %s", truncate(turn2Text.String(), 80))
	})
}

// TestE2E_Streaming_EmptyPrompt tests streaming behavior with minimal input.
func TestE2E_Streaming_EmptyPrompt(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Skipping mock for empty prompt test")
		}

		conv := NewProviderConversation(t, provider)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		var fullText strings.Builder
		var gotResponse bool

		for chunk := range conv.Stream(ctx, "Hi") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			if chunk.Type == ChunkText {
				fullText.WriteString(chunk.Text)
				gotResponse = true
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		assert.True(t, gotResponse, "Should get a response even for minimal input")
		assert.NotEmpty(t, fullText.String(), "Response should not be empty")

		t.Logf("Provider %s minimal response: %s", provider.ID, truncate(fullText.String(), 80))
	})
}
