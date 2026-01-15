//go:build e2e

package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Cost and Token Tracking E2E Tests
//
// These tests verify that token counts and costs are properly tracked across
// all providers for both streaming and non-streaming responses.
//
// Run with: go test -tags=e2e ./sdk/... -run TestE2E_Costs
// =============================================================================

// TestE2E_Costs_NonStreamingTokens tests token tracking for non-streaming requests.
func TestE2E_Costs_NonStreamingTokens(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't track real tokens")
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

		// Send a simple message
		resp, err := conv.Send(ctx, "Say hello")
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Allow time for event processing
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, completedData, "Should receive ProviderCallCompleted event")

		// Verify provider and model are set
		assert.NotEmpty(t, completedData.Provider, "Provider should be set")
		assert.NotEmpty(t, completedData.Model, "Model should be set")

		// Input tokens should be non-zero (we sent a message)
		assert.Greater(t, completedData.InputTokens, 0, "Input tokens should be > 0")

		// Output tokens should be non-zero (model responded)
		assert.Greater(t, completedData.OutputTokens, 0, "Output tokens should be > 0")

		// Duration should be positive
		assert.Greater(t, completedData.Duration, time.Duration(0), "Duration should be > 0")

		t.Logf("Provider %s - Model: %s, Input: %d, Output: %d, Duration: %v, Cost: $%.6f",
			provider.ID,
			completedData.Model,
			completedData.InputTokens,
			completedData.OutputTokens,
			completedData.Duration,
			completedData.Cost)
	})
}

// TestE2E_Costs_StreamingTokens tests token tracking for streaming requests.
func TestE2E_Costs_StreamingTokens(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapStreaming, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't track real tokens")
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
		for chunk := range conv.Stream(ctx, "Say hello in one word") {
			if chunk.Error != nil {
				t.Fatalf("Stream error: %v", chunk.Error)
			}
			if chunk.Type == ChunkDone {
				break
			}
		}

		// Allow time for event processing
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, completedData, "Should receive ProviderCallCompleted event")

		// For streaming, we should still get token counts
		totalTokens := completedData.InputTokens + completedData.OutputTokens
		assert.Greater(t, totalTokens, 0, "Total tokens should be > 0 for streaming")

		t.Logf("Provider %s streaming - Input: %d, Output: %d, Cost: $%.6f",
			provider.ID,
			completedData.InputTokens,
			completedData.OutputTokens,
			completedData.Cost)
	})
}

// TestE2E_Costs_MultiTurnAccumulation tests that costs accumulate across turns.
func TestE2E_Costs_MultiTurnAccumulation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping multi-turn cost test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't track real tokens")
		}

		// Create event bus to capture all token events
		bus := events.NewEventBus()
		var allEvents []*events.ProviderCallCompletedData
		var mu sync.Mutex

		bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
			mu.Lock()
			if data, ok := e.Data.(*events.ProviderCallCompletedData); ok {
				allEvents = append(allEvents, data)
			}
			mu.Unlock()
		})

		conv := NewProviderConversationWithEvents(t, provider, bus)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Send multiple messages
		messages := []string{"Hello", "How are you?", "Goodbye"}
		for _, msg := range messages {
			_, err := conv.Send(ctx, msg)
			require.NoError(t, err)
		}

		// Allow time for event processing
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		// Should have one event per message
		assert.Len(t, allEvents, len(messages), "Should have one event per message")

		// Calculate totals
		var totalInput, totalOutput int
		var totalCost float64
		for i, ev := range allEvents {
			totalInput += ev.InputTokens
			totalOutput += ev.OutputTokens
			totalCost += ev.Cost
			t.Logf("Turn %d - Input: %d, Output: %d, Cost: $%.6f",
				i+1, ev.InputTokens, ev.OutputTokens, ev.Cost)
		}

		// Input tokens should grow as conversation history accumulates
		// (later turns include all previous messages)
		if len(allEvents) >= 2 {
			assert.Greater(t, allEvents[len(allEvents)-1].InputTokens, allEvents[0].InputTokens,
				"Later turns should have more input tokens due to history")
		}

		t.Logf("Provider %s totals - Input: %d, Output: %d, Cost: $%.6f",
			provider.ID, totalInput, totalOutput, totalCost)
	})
}

// TestE2E_Costs_CostCalculation tests that cost calculation is reasonable.
func TestE2E_Costs_CostCalculation(t *testing.T) {
	EnsureTestPacks(t)

	RunForProviders(t, CapText, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't track real tokens")
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

		_, err := conv.Send(ctx, "Hello")
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, completedData)

		// If we have tokens, cost should be non-zero
		if completedData.InputTokens > 0 || completedData.OutputTokens > 0 {
			assert.Greater(t, completedData.Cost, 0.0, "Cost should be > 0 when tokens are used")
		}

		// Cost should be reasonable (less than $1 for a simple hello)
		assert.Less(t, completedData.Cost, 1.0, "Cost should be reasonable for simple message")

		t.Logf("Provider %s cost verification - Tokens: %d+%d, Cost: $%.8f",
			provider.ID,
			completedData.InputTokens,
			completedData.OutputTokens,
			completedData.Cost)
	})
}

// TestE2E_Costs_VisionTokens tests token tracking for vision requests.
func TestE2E_Costs_VisionTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping vision token test in short mode")
	}

	EnsureTestPacks(t)

	RunForProviders(t, CapVision, func(t *testing.T, provider ProviderConfig) {
		if provider.ID == "mock" {
			t.Skip("Mock provider doesn't track real tokens")
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

		conv := NewVisionConversationWithEvents(t, provider, bus)
		defer conv.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		// Send image with text
		imageData := createColoredPNG(255, 0, 0)
		_, err := conv.Send(ctx, "What color?", WithImageData(imageData, "image/png"))
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, completedData, "Should receive event for vision request")

		// Vision requests should have tokens tracked
		totalTokens := completedData.InputTokens + completedData.OutputTokens
		assert.Greater(t, totalTokens, 0, "Vision request should track tokens")

		// Image tokens typically add significant input token count
		t.Logf("Provider %s vision tokens - Input: %d, Output: %d, Cost: $%.6f",
			provider.ID,
			completedData.InputTokens,
			completedData.OutputTokens,
			completedData.Cost)
	})
}
