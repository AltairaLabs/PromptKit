package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonStreamingProvider wraps a provider and disables streaming support.
// Used for testing the non-streaming code path (executeRound).
type nonStreamingProvider struct {
	providers.Provider
}

func (p *nonStreamingProvider) SupportsStreaming() bool {
	return false
}

// TestProviderStage_StreamingMode tests that streaming providers use the streaming execution path
func TestProviderStage_StreamingMode(t *testing.T) {
	// Create mock provider with streaming support (enabled by default)
	provider := mock.NewProvider("test-provider", "test-model", false)

	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	// Create input with system prompt in metadata
	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect output
	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}

	// Should have received streaming chunks + final message
	require.Greater(t, len(elements), 0, "should receive at least one element")

	// Check for streaming text elements (deltas)
	foundTextElement := false
	for _, elem := range elements {
		if elem.Text != nil && *elem.Text != "" {
			foundTextElement = true
			break
		}
	}
	assert.True(t, foundTextElement, "should have streaming text elements")

	// Last element should be complete message
	lastElem := elements[len(elements)-1]
	assert.NotNil(t, lastElem.Message, "last element should have message")
	assert.Equal(t, "assistant", lastElem.Message.Role)
}

// TestProviderStage_MetadataExtraction tests extraction of system prompt and allowed tools
func TestProviderStage_MetadataExtraction(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)

	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	// Create input with various metadata
	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "Custom system prompt"
	elem.Metadata["allowed_tools"] = []string{"tool1", "tool2"}
	elem.Metadata["custom_key"] = "custom_value"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// The stage should successfully extract and use the metadata
	// (verification is implicit - no errors means metadata was handled correctly)
	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}
	assert.Greater(t, len(elements), 0, "should receive output")
}

// TestProviderStage_NoProvider tests error when provider is nil
func TestProviderStage_NoProvider(t *testing.T) {
	stage := NewProviderStage(nil, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "Test"})
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no provider configured")
}

// TestProviderStage_EmptyInput tests handling of empty input channel
func TestProviderStage_EmptyInput(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	// Empty input
	input := make(chan StreamElement)
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	// Should complete without error (no messages to process)
	require.NoError(t, err)

	// Stage may pass through non-message elements, just ensure no errors
	var count int
	for range output {
		count++
	}
	// No strict assertion - stage may forward elements
	t.Logf("Received %d elements for empty input", count)
}

// TestProviderStage_MultipleMessages tests processing multiple messages
func TestProviderStage_MultipleMessages(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	// Multiple input messages
	input := make(chan StreamElement, 3)
	for i := 0; i < 3; i++ {
		msg := types.Message{
			Role:    "user",
			Content: "Test message",
		}
		elem := NewMessageElement(&msg)
		elem.Metadata["system_prompt"] = "System"
		input <- elem
	}
	close(input)

	output := make(chan StreamElement, 20)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect all assistant messages from output
	var assistantMessages []*types.Message
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == "assistant" {
			assistantMessages = append(assistantMessages, elem.Message)
		}
	}

	// ProviderStage processes all inputs together, should have at least one response
	assert.Greater(t, len(assistantMessages), 0, "should have at least one assistant response")
}

// TestProviderStage_ContextCancellation tests context cancellation
func TestProviderStage_ContextCancellation(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "System"
	input <- elem
	close(input)

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Execute stage in goroutine
	output := make(chan StreamElement, 10)
	errChan := make(chan error, 1)
	go func() {
		errChan <- stage.Process(ctx, input, output)
	}()

	// Cancel immediately
	cancel()

	// Wait for completion
	err := <-errChan

	// Should receive cancellation error or complete successfully
	// (depends on timing - cancellation may happen before or after processing)
	if err != nil {
		assert.Contains(t, err.Error(), "context canceled")
	}
}

// TestProviderStage_StreamingChunkMetadata tests that streaming chunks have proper metadata
func TestProviderStage_StreamingChunkMetadata(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Tell me a story",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a storyteller"
	input <- elem
	close(input)

	output := make(chan StreamElement, 20)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect all elements
	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}

	// Check that streaming elements have timestamp and priority
	for _, elem := range elements {
		if elem.Metadata != nil {
			if _, hasDelta := elem.Metadata["delta"]; hasDelta {
				// This is a streaming chunk
				assert.False(t, elem.Timestamp.IsZero(), "streaming chunk should have timestamp")
				assert.NotEqual(t, Priority(0), elem.Priority, "streaming chunk should have priority")
			}
		}
	}
}

// TestProviderStage_MessageTimestamps tests that messages have timestamps
func TestProviderStage_MessageTimestamps(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "System"
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Check that assistant messages have timestamps
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == "assistant" {
			assert.False(t, elem.Message.Timestamp.IsZero(),
				"assistant message should have timestamp")
		}
	}
}

// TestProviderStage_MessageLatency tests that messages have latency tracking
func TestProviderStage_MessageLatency(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "System"
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Check that assistant messages have latency set (may be 0 for fast mock)
	foundAssistant := false
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == "assistant" {
			foundAssistant = true
			// Latency should be non-negative (mock may return instantly)
			assert.GreaterOrEqual(t, elem.Message.LatencyMs, int64(0),
				"assistant message should have non-negative latency")
		}
	}
	assert.True(t, foundAssistant, "should have at least one assistant message")
}

// TestProviderStage_StreamingLatency tests latency tracking in streaming mode
func TestProviderStage_StreamingLatency(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", true) // streaming = true
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test streaming message",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "System"
	input <- elem
	close(input)

	output := make(chan StreamElement, 100)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect all output elements
	var assistantMessages []types.Message
	for elem := range output {
		if elem.Message != nil && elem.Message.Role == "assistant" {
			assistantMessages = append(assistantMessages, *elem.Message)
		}
	}

	// Should have at least one assistant message with latency
	assert.NotEmpty(t, assistantMessages, "should have assistant messages")
	for _, msg := range assistantMessages {
		assert.GreaterOrEqual(t, msg.LatencyMs, int64(0),
			"streaming assistant message should have non-negative latency")
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestIsToolBlocked(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		blocklist []string
		expected  bool
	}{
		{
			name:      "empty blocklist",
			toolName:  "some_tool",
			blocklist: []string{},
			expected:  false,
		},
		{
			name:      "tool not in blocklist",
			toolName:  "allowed_tool",
			blocklist: []string{"blocked_tool"},
			expected:  false,
		},
		{
			name:      "tool in blocklist",
			toolName:  "blocked_tool",
			blocklist: []string{"blocked_tool"},
			expected:  true,
		},
		{
			name:      "tool in middle of blocklist",
			toolName:  "tool2",
			blocklist: []string{"tool1", "tool2", "tool3"},
			expected:  true,
		},
		{
			name:      "nil blocklist",
			toolName:  "some_tool",
			blocklist: nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isToolBlocked(tt.toolName, tt.blocklist)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatToolResult(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "string value",
			input:    "simple string",
			expected: "simple string",
		},
		{
			name:     "map value",
			input:    map[string]interface{}{"key": "value"},
			expected: "{\n  \"key\": \"value\"\n}",
		},
		{
			name:     "array value",
			input:    []interface{}{"a", "b"},
			expected: "[\n  \"a\",\n  \"b\"\n]",
		},
		{
			name:     "number value",
			input:    42,
			expected: "42",
		},
		{
			name:     "boolean value",
			input:    true,
			expected: "true",
		},
		{
			name:     "nil value",
			input:    nil,
			expected: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolResult(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewProviderStage_NilConfig(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)

	// Should not panic with nil config
	stage := NewProviderStage(provider, nil, nil, nil)

	assert.NotNil(t, stage)
	assert.NotNil(t, stage.config) // Should create default config
}

func TestNewProviderStage_WithConfig(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	seed := 42

	config := &ProviderConfig{
		MaxTokens:   500,
		Temperature: 0.9,
		Seed:        &seed,
	}

	stage := NewProviderStage(provider, nil, nil, config)

	assert.NotNil(t, stage)
	assert.Equal(t, 500, stage.config.MaxTokens)
	assert.Equal(t, float32(0.9), stage.config.Temperature)
	assert.Equal(t, &seed, stage.config.Seed)
}

func TestProviderStage_NonStreamingMode(t *testing.T) {
	// Create mock provider WITHOUT streaming support using wrapper
	baseProvider := mock.NewProvider("test-provider", "test-model", false)
	provider := &nonStreamingProvider{Provider: baseProvider}

	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test message",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect output
	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}

	// Should have at least one response
	require.Greater(t, len(elements), 0)

	// Verify that we got a final message with assistant role
	foundAssistant := false
	for _, elem := range elements {
		if elem.Message != nil && elem.Message.Role == "assistant" {
			foundAssistant = true
			break
		}
	}
	assert.True(t, foundAssistant, "should have assistant message in output")
}

func TestProviderStage_MockScenarioMetadata(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	input := make(chan StreamElement, 1)
	userMsg := types.Message{
		Role:    "user",
		Content: "Test",
	}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "System"
	elem.Metadata["mock_scenario_id"] = "test-scenario"
	elem.Metadata["mock_turn_number"] = 1
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)

	require.NoError(t, err)

	// Collect output
	var elements []StreamElement
	for elem := range output {
		elements = append(elements, elem)
	}
	assert.Greater(t, len(elements), 0)
}

func TestProviderStage_BaseStageProperties(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	assert.Equal(t, "provider", stage.Name())
	assert.Equal(t, StageTypeGenerate, stage.Type())
}

// =============================================================================
// Helper Function Unit Tests (for coverage)
// =============================================================================

func TestProviderStage_AccumulateInput(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	t.Run("accumulates messages and metadata", func(t *testing.T) {
		input := make(chan StreamElement, 3)

		// Send multiple messages with metadata
		msg1 := types.Message{Role: "user", Content: "Hello"}
		elem1 := NewMessageElement(&msg1)
		elem1.Metadata["system_prompt"] = "You are helpful"
		input <- elem1

		msg2 := types.Message{Role: "assistant", Content: "Hi"}
		elem2 := NewMessageElement(&msg2)
		elem2.Metadata["allowed_tools"] = []string{"tool1", "tool2"}
		input <- elem2

		msg3 := types.Message{Role: "user", Content: "Thanks"}
		elem3 := NewMessageElement(&msg3)
		elem3.Metadata["custom_key"] = "custom_value"
		input <- elem3

		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 3)
		assert.Equal(t, "You are helpful", acc.systemPrompt)
		assert.Equal(t, []string{"tool1", "tool2"}, acc.allowedTools)
		assert.Equal(t, "custom_value", acc.metadata["custom_key"])
	})

	t.Run("handles empty input", func(t *testing.T) {
		input := make(chan StreamElement)
		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 0)
		assert.Empty(t, acc.systemPrompt)
		assert.Nil(t, acc.allowedTools)
		assert.NotNil(t, acc.metadata)
	})

	t.Run("handles elements without messages", func(t *testing.T) {
		input := make(chan StreamElement, 1)
		text := "some text"
		elem := StreamElement{Text: &text, Metadata: map[string]interface{}{"key": "value"}}
		input <- elem
		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 0)
		assert.Equal(t, "value", acc.metadata["key"])
	})
}

func TestProviderStage_ExtractMetadata(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	t.Run("extracts system prompt", func(t *testing.T) {
		acc := &providerInput{metadata: make(map[string]interface{})}
		elem := &StreamElement{Metadata: map[string]interface{}{"system_prompt": "test prompt"}}

		stage.extractMetadata(elem, acc)

		assert.Equal(t, "test prompt", acc.systemPrompt)
	})

	t.Run("extracts allowed tools", func(t *testing.T) {
		acc := &providerInput{metadata: make(map[string]interface{})}
		elem := &StreamElement{Metadata: map[string]interface{}{"allowed_tools": []string{"a", "b"}}}

		stage.extractMetadata(elem, acc)

		assert.Equal(t, []string{"a", "b"}, acc.allowedTools)
	})

	t.Run("handles nil metadata", func(t *testing.T) {
		acc := &providerInput{metadata: make(map[string]interface{})}
		elem := &StreamElement{Metadata: nil}

		stage.extractMetadata(elem, acc)

		assert.Empty(t, acc.systemPrompt)
		assert.Nil(t, acc.allowedTools)
	})

	t.Run("merges all metadata", func(t *testing.T) {
		acc := &providerInput{metadata: make(map[string]interface{})}
		elem := &StreamElement{Metadata: map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}}

		stage.extractMetadata(elem, acc)

		assert.Equal(t, "value1", acc.metadata["key1"])
		assert.Equal(t, 42, acc.metadata["key2"])
	})
}

func TestProviderStage_EmitResponseMessages(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	t.Run("emits all messages with metadata", func(t *testing.T) {
		ctx := context.Background()
		output := make(chan StreamElement, 10)
		messages := []types.Message{
			{Role: "assistant", Content: "Response 1"},
			{Role: "assistant", Content: "Response 2"},
		}
		metadata := map[string]interface{}{"key": "value"}

		go func() {
			err := stage.emitResponseMessages(ctx, messages, metadata, output)
			assert.NoError(t, err)
			close(output)
		}()

		var received []StreamElement
		for elem := range output {
			received = append(received, elem)
		}

		assert.Len(t, received, 2)
		assert.Equal(t, "Response 1", received[0].Message.Content)
		assert.Equal(t, "value", received[0].Metadata["key"])
	})

	t.Run("handles empty messages", func(t *testing.T) {
		ctx := context.Background()
		output := make(chan StreamElement, 10)

		err := stage.emitResponseMessages(ctx, []types.Message{}, nil, output)

		assert.NoError(t, err)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		output := make(chan StreamElement) // Unbuffered, will block
		messages := []types.Message{{Role: "assistant", Content: "Test"}}

		err := stage.emitResponseMessages(ctx, messages, nil, output)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestProviderStage_GetMaxRounds(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)

	t.Run("returns default when no policy", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, nil, nil)
		assert.Equal(t, 10, stage.getMaxRounds()) // defaultMaxRounds
	})

	t.Run("returns policy value when set", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, &pipeline.ToolPolicy{MaxRounds: 5}, nil)
		assert.Equal(t, 5, stage.getMaxRounds())
	})

	t.Run("returns default when policy MaxRounds is zero", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, &pipeline.ToolPolicy{MaxRounds: 0}, nil)
		assert.Equal(t, 10, stage.getMaxRounds())
	})
}

// =============================================================================
// Event Emission Tests - Ensure ProviderStage emits provider call events
// =============================================================================

// waitForWG waits for a WaitGroup with a timeout, returns false if timed out
func waitForWG(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func TestProviderStage_EmitsProviderCallStartedEvent(t *testing.T) {
	// Create event bus and emitter
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	// Track received events
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) {
		receivedEvent = e
		wg.Done()
	})

	// Create provider and stage with emitter
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, emitter)

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Wait for event with timeout
	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatal("timed out waiting for ProviderCallStarted event - event was not emitted")
	}

	// Verify event data
	require.NotNil(t, receivedEvent)
	assert.Equal(t, events.EventProviderCallStarted, receivedEvent.Type)
	assert.Equal(t, "test-run", receivedEvent.RunID)
	assert.Equal(t, "test-session", receivedEvent.SessionID)

	data, ok := receivedEvent.Data.(events.ProviderCallStartedData)
	require.True(t, ok, "event data should be ProviderCallStartedData")
	assert.Equal(t, "test-provider", data.Provider)
	assert.Equal(t, 1, data.MessageCount) // 1 user message
}

func TestProviderStage_EmitsProviderCallCompletedEvent(t *testing.T) {
	// Create event bus and emitter
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	// Track received events
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		receivedEvent = e
		wg.Done()
	})

	// Create provider and stage with emitter
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, emitter)

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Wait for event with timeout
	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatal("timed out waiting for ProviderCallCompleted event - event was not emitted")
	}

	// Verify event data
	require.NotNil(t, receivedEvent)
	assert.Equal(t, events.EventProviderCallCompleted, receivedEvent.Type)
	assert.Equal(t, "test-run", receivedEvent.RunID)
	assert.Equal(t, "test-session", receivedEvent.SessionID)

	data, ok := receivedEvent.Data.(*events.ProviderCallCompletedData)
	require.True(t, ok, "event data should be *ProviderCallCompletedData")
	assert.Equal(t, "test-provider", data.Provider)
	assert.Greater(t, data.Duration, time.Duration(0), "duration should be positive")
}

func TestProviderStage_EmitsProviderCallCompletedEvent_NonStreaming(t *testing.T) {
	// Create event bus and emitter
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	// Track received events
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		receivedEvent = e
		wg.Done()
	})

	// Create provider WITHOUT streaming support using the wrapper
	baseProvider := mock.NewProvider("test-provider", "test-model", false)
	provider := &nonStreamingProvider{Provider: baseProvider}
	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, emitter)

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Wait for event with timeout
	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatal("timed out waiting for ProviderCallCompleted event in non-streaming mode - event was not emitted")
	}

	// Verify event data
	require.NotNil(t, receivedEvent)
	assert.Equal(t, events.EventProviderCallCompleted, receivedEvent.Type)

	data, ok := receivedEvent.Data.(*events.ProviderCallCompletedData)
	require.True(t, ok, "event data should be *ProviderCallCompletedData")
	assert.Equal(t, "test-provider", data.Provider)
}

func TestProviderStage_NoEventsWithNilEmitter(t *testing.T) {
	// Create event bus to verify NO events are emitted
	bus := events.NewEventBus()

	eventReceived := false
	bus.SubscribeAll(func(e *events.Event) {
		eventReceived = true
	})

	// Create provider and stage WITHOUT emitter (nil)
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Give some time for any events to propagate
	time.Sleep(50 * time.Millisecond)

	// Verify no events were received (since emitter is nil)
	assert.False(t, eventReceived, "no events should be emitted when emitter is nil")
}

func TestProviderStage_WithToolRegistry(t *testing.T) {
	// Test that ProviderStage correctly builds tools when allowed_tools are specified
	// This exercises the buildProviderTools function

	t.Run("builds tools when allowed_tools in metadata", func(t *testing.T) {
		// Create tool registry with a test tool
		toolRegistry := tools.NewRegistry()
		toolRegistry.Register(&tools.ToolDescriptor{
			Name:        "test_tool",
			Description: "A test tool",
			InputSchema: []byte(`{"type": "object"}`),
		})

		// Use tool provider mock
		provider := mock.NewToolProvider("test-provider", "test-model", false, nil)

		stage := NewProviderStage(provider, toolRegistry, nil, &ProviderConfig{
			MaxTokens:   100,
			Temperature: 0.7,
		})

		// Create input with allowed_tools
		input := make(chan StreamElement, 1)
		userMsg := types.Message{Role: "user", Content: "Test message"}
		elem := NewMessageElement(&userMsg)
		elem.Metadata["system_prompt"] = "You are a helpful assistant"
		elem.Metadata["allowed_tools"] = []string{"test_tool"}
		input <- elem
		close(input)

		// Execute stage
		output := make(chan StreamElement, 10)
		ctx := context.Background()
		err := stage.Process(ctx, input, output)

		require.NoError(t, err)

		// Drain output
		for range output {
		}
	})

	t.Run("handles empty allowed_tools", func(t *testing.T) {
		toolRegistry := tools.NewRegistry()
		provider := mock.NewToolProvider("test-provider", "test-model", false, nil)

		stage := NewProviderStage(provider, toolRegistry, nil, &ProviderConfig{
			MaxTokens:   100,
			Temperature: 0.7,
		})

		// Create input without allowed_tools
		input := make(chan StreamElement, 1)
		userMsg := types.Message{Role: "user", Content: "Test message"}
		elem := NewMessageElement(&userMsg)
		elem.Metadata["system_prompt"] = "You are a helpful assistant"
		input <- elem
		close(input)

		output := make(chan StreamElement, 10)
		ctx := context.Background()
		err := stage.Process(ctx, input, output)

		require.NoError(t, err)

		for range output {
		}
	})
}

// =============================================================================
// Tool Execution Tests - Test executeToolCalls and handleToolResult
// =============================================================================

// mockAsyncExecutor implements tools.AsyncToolExecutor for testing
type mockAsyncExecutor struct {
	name       string
	status     tools.ToolExecutionStatus
	content    []byte
	errorMsg   string
	pendingMsg string
}

func (m *mockAsyncExecutor) Name() string {
	return m.name
}

func (m *mockAsyncExecutor) Execute(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	if m.status == tools.ToolStatusFailed {
		return nil, fmt.Errorf("%s", m.errorMsg)
	}
	return m.content, nil
}

func (m *mockAsyncExecutor) ExecuteAsync(_ context.Context, descriptor *tools.ToolDescriptor, _ json.RawMessage) (*tools.ToolExecutionResult, error) {
	result := &tools.ToolExecutionResult{
		Status:  m.status,
		Content: m.content,
		Error:   m.errorMsg,
	}
	if m.status == tools.ToolStatusPending {
		result.PendingInfo = &tools.PendingToolInfo{
			Reason:   "requires_approval",
			Message:  m.pendingMsg,
			ToolName: descriptor.Name,
		}
	}
	return result, nil
}

func TestProviderStage_ExecuteToolCalls_NilRegistry(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil) // nil registry

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "some_tool", Args: json.RawMessage(`{}`)},
	}

	_, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool registry not configured")
}

func TestProviderStage_ExecuteToolCalls_BlockedTool(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	// Register a tool
	registry.Register(&tools.ToolDescriptor{
		Name:        "blocked_tool",
		Description: "A blocked tool",
		InputSchema: []byte(`{"type": "object"}`),
	})

	// Create stage with tool policy that blocks the tool
	toolPolicy := &pipeline.ToolPolicy{
		Blocklist: []string{"blocked_tool"},
	}
	stage := NewProviderStage(provider, registry, toolPolicy, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "blocked_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool", results[0].Role)
	assert.Contains(t, results[0].ToolResult.Content, "blocked by policy")
	assert.Contains(t, results[0].ToolResult.Error, "blocked by policy")
}

func TestProviderStage_ExecuteToolCalls_ToolNotFound(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()
	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "nonexistent_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool", results[0].Role)
	assert.Contains(t, results[0].ToolResult.Content, "Error:")
	assert.NotEmpty(t, results[0].ToolResult.Error)
}

func TestProviderStage_ExecuteToolCalls_Complete(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	// Register mock executor that returns complete status
	executor := &mockAsyncExecutor{
		name:    "test-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"result": "success"}`),
	}
	registry.RegisterExecutor(executor)

	// Register a tool that uses this executor (Mode matches executor name)
	registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "A test tool",
		Mode:        "test-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})

	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "test_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool", results[0].Role)
	assert.NotNil(t, results[0].ToolResult)
	assert.Empty(t, results[0].ToolResult.Error)
	assert.Contains(t, results[0].ToolResult.Content, "result")
}

func TestProviderStage_ExecuteToolCalls_Pending(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	// Register mock executor that returns pending status
	executor := &mockAsyncExecutor{
		name:       "pending-executor",
		status:     tools.ToolStatusPending,
		pendingMsg: "Tool requires user approval",
	}
	registry.RegisterExecutor(executor)

	registry.Register(&tools.ToolDescriptor{
		Name:        "pending_tool",
		Description: "A tool requiring approval",
		Mode:        "pending-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})

	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "pending_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool", results[0].Role)
	assert.Contains(t, results[0].ToolResult.Content, "requires")
	assert.Empty(t, results[0].ToolResult.Error)
}

func TestProviderStage_ExecuteToolCalls_Failed(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	// Register mock executor that returns failed status
	executor := &mockAsyncExecutor{
		name:     "failing-executor",
		status:   tools.ToolStatusFailed,
		errorMsg: "Tool execution failed: network error",
	}
	registry.RegisterExecutor(executor)

	registry.Register(&tools.ToolDescriptor{
		Name:        "failing_tool",
		Description: "A tool that fails",
		Mode:        "failing-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})

	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "failing_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "tool", results[0].Role)
	assert.Contains(t, results[0].ToolResult.Content, "failed")
	assert.NotEmpty(t, results[0].ToolResult.Error)
}

func TestProviderStage_ExecuteToolCalls_MultipleToolCalls(t *testing.T) {
	// Test multiple tool calls in a single request
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	// Register mock executor for complete status
	executor := &mockAsyncExecutor{
		name:    "multi-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"result": "ok"}`),
	}
	registry.RegisterExecutor(executor)

	registry.Register(&tools.ToolDescriptor{
		Name:        "tool1",
		Description: "Tool 1",
		Mode:        "multi-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})
	registry.Register(&tools.ToolDescriptor{
		Name:        "tool2",
		Description: "Tool 2",
		Mode:        "multi-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})

	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "tool1", Args: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "tool2", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "call-1", results[0].ToolResult.ID)
	assert.Equal(t, "call-2", results[1].ToolResult.ID)
}

func TestProviderStage_BuildProviderTools_ToolChoice(t *testing.T) {
	// Test that tool choice from policy is used
	registry := tools.NewRegistry()
	registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "Test",
		InputSchema: []byte(`{"type": "object"}`),
	})

	provider := mock.NewToolProvider("test", "model", false, nil)
	toolPolicy := &pipeline.ToolPolicy{
		ToolChoice: "required",
	}
	stage := NewProviderStage(provider, registry, toolPolicy, nil)

	// Build tools with allowed_tools
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"})

	require.NoError(t, err)
	assert.NotNil(t, providerTools)
	assert.Equal(t, "required", toolChoice)
}

func TestProviderStage_BuildProviderTools_ToolNotFound(t *testing.T) {
	// Test that missing tools are skipped (logs warning but continues)
	registry := tools.NewRegistry()
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, registry, nil, nil)

	// Build tools with non-existent tool — no descriptors to send, returns nil
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"nonexistent_tool"})

	require.NoError(t, err)
	// No tools found means no BuildTooling call
	assert.Equal(t, "", toolChoice)
	assert.Nil(t, providerTools)
}

func TestProviderStage_BuildProviderTools_ProviderNoToolSupport(t *testing.T) {
	// Test that providers without tool support return nil tools
	registry := tools.NewRegistry()
	registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "Test",
		InputSchema: []byte(`{"type": "object"}`),
	})

	// Regular mock provider doesn't implement ToolSupport
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, registry, nil, nil)

	// Build tools with allowed_tools - should return nil since provider doesn't support tools
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"})

	require.NoError(t, err)
	assert.Nil(t, providerTools)
	assert.Empty(t, toolChoice)
}

func TestProviderStage_BuildProviderTools_EmptyAllowedTools(t *testing.T) {
	// Test that empty allowed_tools returns nil
	registry := tools.NewRegistry()
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, registry, nil, nil)

	providerTools, toolChoice, err := stage.buildProviderTools([]string{})

	require.NoError(t, err)
	assert.Nil(t, providerTools)
	assert.Empty(t, toolChoice)
}

func TestProviderStage_BuildProviderTools_NilRegistry(t *testing.T) {
	// Test that nil registry returns nil tools
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, nil, nil, nil)

	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"})

	require.NoError(t, err)
	assert.Nil(t, providerTools)
	assert.Empty(t, toolChoice)
}

func TestProviderStage_BuildProviderTools_SystemToolsIncluded(t *testing.T) {
	// System-namespaced tools (skill__, a2a__, etc.) are included automatically
	// even when they're not in the allowedTools list.
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "skill__activate",
		Description: "Activate a skill",
		InputSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
	})
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "regular_tool",
		Description: "A regular tool",
		InputSchema: []byte(`{"type":"object"}`),
	})

	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, registry, nil, nil)

	// Only allow regular_tool — skill__activate should still be included as a system tool
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"regular_tool"})

	require.NoError(t, err)
	assert.Equal(t, "auto", toolChoice)
	assert.NotNil(t, providerTools)
}

func TestProviderStage_BuildProviderTools_SystemToolsWithEmptyAllowed(t *testing.T) {
	// System tools are included even when allowedTools is empty.
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "a2a__send",
		Description: "Send to agent",
		InputSchema: []byte(`{"type":"object"}`),
	})

	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, registry, nil, nil)

	providerTools, toolChoice, err := stage.buildProviderTools([]string{})

	require.NoError(t, err)
	assert.Equal(t, "auto", toolChoice)
	assert.NotNil(t, providerTools)
}

func TestProviderStage_HandleToolResult_AllStatuses(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	testCases := []struct {
		name           string
		status         tools.ToolExecutionStatus
		content        []byte
		errorMsg       string
		pendingMsg     string
		expectError    bool
		contentContain string
	}{
		{
			name:           "complete status with JSON",
			status:         tools.ToolStatusComplete,
			content:        []byte(`{"key": "value"}`),
			contentContain: "key",
		},
		{
			name:           "complete status with plain text",
			status:         tools.ToolStatusComplete,
			content:        []byte("plain text result"),
			contentContain: "plain text result",
		},
		{
			name:           "failed status",
			status:         tools.ToolStatusFailed,
			errorMsg:       "execution error",
			expectError:    true,
			contentContain: "failed",
		},
		{
			name:           "pending status with message",
			status:         tools.ToolStatusPending,
			pendingMsg:     "Approval required",
			contentContain: "Approval required",
		},
		{
			name:           "pending status without message",
			status:         tools.ToolStatusPending,
			pendingMsg:     "",
			contentContain: "requires approval",
		},
		{
			name:           "unknown status",
			status:         tools.ToolExecutionStatus("unknown"),
			expectError:    true,
			contentContain: "Unknown tool status",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			call := types.MessageToolCall{
				ID:   "test-call-id",
				Name: "test_tool",
				Args: json.RawMessage(`{}`),
			}

			asyncResult := &tools.ToolExecutionResult{
				Status:  tc.status,
				Content: tc.content,
				Error:   tc.errorMsg,
			}
			if tc.status == tools.ToolStatusPending {
				asyncResult.PendingInfo = &tools.PendingToolInfo{
					Message:  tc.pendingMsg,
					ToolName: "test_tool",
				}
			}

			result := stage.handleToolResult(call, asyncResult)

			assert.Equal(t, "test-call-id", result.ID)
			assert.Equal(t, "test_tool", result.Name)
			assert.Contains(t, result.Content, tc.contentContain)
			if tc.expectError {
				assert.NotEmpty(t, result.Error)
			}
		})
	}
}

func TestProviderStage_EmitsBothStartAndCompletedEvents(t *testing.T) {
	// Create event bus and emitter
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	// Track received events
	var mu sync.Mutex
	receivedTypes := make([]events.EventType, 0)
	var wg sync.WaitGroup
	wg.Add(2) // Expect both Started and Completed

	bus.Subscribe(events.EventProviderCallStarted, func(e *events.Event) {
		mu.Lock()
		receivedTypes = append(receivedTypes, e.Type)
		mu.Unlock()
		wg.Done()
	})

	bus.Subscribe(events.EventProviderCallCompleted, func(e *events.Event) {
		mu.Lock()
		receivedTypes = append(receivedTypes, e.Type)
		mu.Unlock()
		wg.Done()
	})

	// Create provider and stage with emitter
	provider := mock.NewProvider("test-provider", "test-model", false)
	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, emitter)

	// Create input
	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helpful assistant"
	input <- elem
	close(input)

	// Execute stage
	output := make(chan StreamElement, 10)
	ctx := context.Background()
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Wait for both events with timeout
	if !waitForWG(&wg, 500*time.Millisecond) {
		mu.Lock()
		t.Fatalf("timed out waiting for events - only received: %v", receivedTypes)
		mu.Unlock()
	}

	// Verify both events were received
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, receivedTypes, 2, "should receive both Started and Completed events")
	assert.Contains(t, receivedTypes, events.EventProviderCallStarted)
	assert.Contains(t, receivedTypes, events.EventProviderCallCompleted)
}
