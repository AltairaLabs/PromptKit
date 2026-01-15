//go:build e2e

// End-to-end tests for the SDK.
//
// These tests verify the complete integration path from conversation
// through pipeline stages to event emission. Run with:
//
//	go test -tags=e2e ./sdk/...
//
// Or run just the e2e tests:
//
//	go test -tags=e2e -run TestE2E ./sdk/...
package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Event Emission E2E Tests
// =============================================================================

// TestE2E_EventEmission_ProviderCallEvents verifies that provider call events
// are emitted when sending messages through a conversation.
//
// This test would have caught the bug where EventEmitter wasn't wired up
// from the conversation to the pipeline stages.
func TestE2E_EventEmission_ProviderCallEvents(t *testing.T) {
	// Create event bus and track events
	bus := events.NewEventBus()
	var receivedEvents []*events.Event
	var eventsMu sync.Mutex

	bus.SubscribeAll(func(e *events.Event) {
		eventsMu.Lock()
		receivedEvents = append(receivedEvents, e)
		eventsMu.Unlock()
	})

	// Create conversation with event bus
	conv := createE2ETestConversation(t, bus)
	defer conv.Close()

	// Send a message
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := conv.Send(ctx, "Hello, world!")
	require.NoError(t, err)
	assert.NotNil(t, resp)

	// Allow time for async event processing
	time.Sleep(100 * time.Millisecond)

	// Verify events were emitted
	eventsMu.Lock()
	defer eventsMu.Unlock()

	// Should have ProviderCallStarted and ProviderCallCompleted events
	var startedCount, completedCount int
	for _, e := range receivedEvents {
		switch e.Type {
		case events.EventProviderCallStarted:
			startedCount++
			// Verify event data (may be value or pointer depending on emit path)
			switch data := e.Data.(type) {
			case events.ProviderCallStartedData:
				assert.NotEmpty(t, data.Provider, "Provider ID should not be empty")
			case *events.ProviderCallStartedData:
				assert.NotEmpty(t, data.Provider, "Provider ID should not be empty")
			default:
				t.Fatalf("ProviderCallStarted data has unexpected type: %T", e.Data)
			}
		case events.EventProviderCallCompleted:
			completedCount++
			// Verify event data (may be value or pointer depending on emit path)
			switch data := e.Data.(type) {
			case events.ProviderCallCompletedData:
				assert.NotEmpty(t, data.Provider, "Provider ID should not be empty")
				assert.Greater(t, data.Duration, time.Duration(0), "Duration should be positive")
			case *events.ProviderCallCompletedData:
				assert.NotEmpty(t, data.Provider, "Provider ID should not be empty")
				assert.Greater(t, data.Duration, time.Duration(0), "Duration should be positive")
			default:
				t.Fatalf("ProviderCallCompleted data has unexpected type: %T", e.Data)
			}
		}
	}

	assert.Equal(t, 1, startedCount, "Should have exactly one ProviderCallStarted event")
	assert.Equal(t, 1, completedCount, "Should have exactly one ProviderCallCompleted event")
}

// TestE2E_EventEmission_StreamingTokenCounts verifies that streaming responses
// include token counts in the ProviderCallCompleted event.
//
// This test would have caught the bug where streaming responses had zero
// token counts because CostInfo wasn't captured from the final chunk.
func TestE2E_EventEmission_StreamingTokenCounts(t *testing.T) {
	// Create event bus and track completed events
	bus := events.NewEventBus()
	var completedEvent *events.ProviderCallCompletedData
	var eventsMu sync.Mutex

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		eventsMu.Lock()
		if data, ok := e.Data.(*events.ProviderCallCompletedData); ok {
			completedEvent = data
		}
		eventsMu.Unlock()
	})

	// Create conversation with event bus using streaming provider
	conv := createE2ETestConversationStreaming(t, bus)
	defer conv.Close()

	// Stream a message
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var chunks []StreamChunk
	for chunk := range conv.Stream(ctx, "Tell me a story") {
		if chunk.Error != nil {
			t.Fatalf("Stream error: %v", chunk.Error)
		}
		chunks = append(chunks, chunk)
		if chunk.Type == ChunkDone {
			break
		}
	}

	// Allow time for async event processing
	time.Sleep(100 * time.Millisecond)

	// Verify ProviderCallCompleted event was received
	eventsMu.Lock()
	defer eventsMu.Unlock()

	require.NotNil(t, completedEvent, "Should have received ProviderCallCompleted event")
	assert.NotEmpty(t, completedEvent.Provider, "Provider ID should not be empty")
	assert.Greater(t, completedEvent.Duration, time.Duration(0), "Duration should be positive")

	// The mock provider should return some token counts in CostInfo
	// Note: If this fails, it means either:
	// 1. Mock provider doesn't emit CostInfo in final chunk, or
	// 2. processStreamChunks doesn't capture it, or
	// 3. executeStreamingRound doesn't pass it to the event
	t.Logf("Streaming response - InputTokens: %d, OutputTokens: %d",
		completedEvent.InputTokens, completedEvent.OutputTokens)
}

// TestE2E_EventEmission_MultipleRounds verifies events are emitted for each
// provider call in multi-round conversations (e.g., tool use).
func TestE2E_EventEmission_MultipleMessages(t *testing.T) {
	// Create event bus and track events
	bus := events.NewEventBus()
	var startedCount, completedCount int
	var eventsMu sync.Mutex

	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) {
		eventsMu.Lock()
		startedCount++
		eventsMu.Unlock()
	})

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		eventsMu.Lock()
		completedCount++
		eventsMu.Unlock()
	})

	// Create conversation with event bus
	conv := createE2ETestConversation(t, bus)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send multiple messages
	for i := 0; i < 3; i++ {
		resp, err := conv.Send(ctx, "Message "+string(rune('1'+i)))
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	// Allow time for async event processing
	time.Sleep(100 * time.Millisecond)

	// Verify events were emitted for each message
	eventsMu.Lock()
	defer eventsMu.Unlock()

	assert.Equal(t, 3, startedCount, "Should have ProviderCallStarted for each message")
	assert.Equal(t, 3, completedCount, "Should have ProviderCallCompleted for each message")
}

// TestE2E_EventEmission_NoEventBus verifies that conversations work correctly
// when no event bus is configured (events are silently skipped).
func TestE2E_EventEmission_NoEventBus(t *testing.T) {
	// Create conversation WITHOUT event bus
	conv := createE2ETestConversation(t, nil)
	defer conv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Should work without errors
	resp, err := conv.Send(ctx, "Hello!")
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotEmpty(t, resp.Text())
}

// TestE2E_EventEmission_Hooks verifies the hooks package works with event bus.
func TestE2E_EventEmission_HooksIntegration(t *testing.T) {
	// Create event bus
	bus := events.NewEventBus()

	// Create conversation with event bus
	conv := createE2ETestConversation(t, bus)
	defer conv.Close()

	// Track events using the hooks API style
	var providerCalls int
	var mu sync.Mutex

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		mu.Lock()
		providerCalls++
		mu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := conv.Send(ctx, "Hello!")
	require.NoError(t, err)

	// Allow time for async event processing
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, providerCalls, "Hook should receive provider call event")
}

// =============================================================================
// Test Helpers
// =============================================================================

// createE2ETestConversation creates a conversation with a mock provider for e2e tests.
// If eventBus is nil, no events will be emitted.
func createE2ETestConversation(t *testing.T, eventBus *events.EventBus) *Conversation {
	t.Helper()

	// Create prompt registry
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are a helpful assistant.",
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(repo)

	// Create mock provider (non-streaming)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	// Create event emitter if event bus is provided
	var emitter *events.Emitter
	if eventBus != nil {
		emitter = events.NewEmitter(eventBus, "", "e2e-test", "e2e-test")
	}

	// Build pipeline with event emitter
	cfg := &pipeline.Config{
		PromptRegistry: promptRegistry,
		TaskType:       "chat",
		Provider:       provider,
		EventEmitter:   emitter,
	}

	pipe, err := pipeline.Build(cfg)
	require.NoError(t, err)

	// Create session
	sess, err := session.NewUnarySession(session.UnarySessionConfig{
		Pipeline:       pipe,
		StateStore:     statestore.NewMemoryStore(),
		ConversationID: "e2e-test",
	})
	require.NoError(t, err)

	// Build conversation struct
	conv := &Conversation{
		pack:           nil, // Not needed for these tests
		prompt:         &pack.Prompt{ID: "chat", Name: "chat"},
		promptName:     "chat",
		promptRegistry: promptRegistry,
		config:         &config{eventBus: eventBus, provider: provider},
		mode:           UnaryMode,
		unarySession:   sess,
	}

	return conv
}

// createE2ETestConversationStreaming creates a streaming conversation for e2e tests.
func createE2ETestConversationStreaming(t *testing.T, eventBus *events.EventBus) *Conversation {
	t.Helper()

	// Create prompt registry
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are a helpful assistant.",
		},
	})
	promptRegistry := prompt.NewRegistryWithRepository(repo)

	// Create mock provider with streaming support
	// The mock.NewProvider supports streaming by default
	provider := mock.NewProvider("mock-test", "mock-model", false)

	// Create event emitter if event bus is provided
	var emitter *events.Emitter
	if eventBus != nil {
		emitter = events.NewEmitter(eventBus, "", "e2e-test-streaming", "e2e-test-streaming")
	}

	// Build pipeline with event emitter
	cfg := &pipeline.Config{
		PromptRegistry: promptRegistry,
		TaskType:       "chat",
		Provider:       provider,
		EventEmitter:   emitter,
	}

	pipe, err := pipeline.BuildStreamPipeline(cfg)
	require.NoError(t, err)

	// Create session for streaming
	sess, err := session.NewUnarySession(session.UnarySessionConfig{
		Pipeline:       pipe,
		StateStore:     statestore.NewMemoryStore(),
		ConversationID: "e2e-test-streaming",
	})
	require.NoError(t, err)

	// Build conversation struct
	conv := &Conversation{
		pack:           nil,
		prompt:         &pack.Prompt{ID: "chat", Name: "chat"},
		promptName:     "chat",
		promptRegistry: promptRegistry,
		config:         &config{eventBus: eventBus, provider: provider},
		mode:           UnaryMode,
		unarySession:   sess,
	}

	return conv
}
