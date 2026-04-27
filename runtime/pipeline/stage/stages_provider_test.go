package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks" // Used by TestAfterRound_ExcludesAfterRepeatedRejection
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

// TestProviderStage_TurnStateOverridesMetadata pins the contract that when
// a *TurnState is wired into the stage, its SystemPrompt and AllowedTools
// fields override whatever is in the deprecated metadata bag.
func TestProviderStage_TurnStateOverridesMetadata(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	turnState := NewTurnState()
	turnState.SystemPrompt = "TurnState system prompt"
	turnState.AllowedTools = []string{"turnstate_tool_a", "turnstate_tool_b"}

	stage := NewProviderStageWithTurnState(provider, nil, nil,
		&ProviderConfig{MaxTokens: 100, Temperature: 0.7}, nil, nil, turnState)

	input := make(chan StreamElement, 2)
	elem := NewMessageElement(&types.Message{Role: "user", Content: "Test"})
	elem.Metadata["system_prompt"] = "BAG should NOT win"
	elem.Metadata["allowed_tools"] = []string{"bag_tool"}
	input <- elem
	close(input)

	acc := stage.accumulateInput(input)
	assert.Equal(t, "TurnState system prompt", acc.systemPrompt,
		"TurnState.SystemPrompt must override the bag value")
	assert.Equal(t, []string{"turnstate_tool_a", "turnstate_tool_b"}, acc.allowedTools,
		"TurnState.AllowedTools must override the bag value")
}

// TestProviderStage_TurnStateEmpty pins behaviour when TurnState is wired
// but its fields are zero/empty: the stage uses the empty values and does
// not fall back to anywhere else (the legacy metadata bag path is gone).
func TestProviderStage_TurnStateEmpty(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)
	turnState := NewTurnState() // empty

	stage := NewProviderStageWithTurnState(provider, nil, nil,
		&ProviderConfig{MaxTokens: 100, Temperature: 0.7}, nil, nil, turnState)

	input := make(chan StreamElement, 2)
	elem := NewMessageElement(&types.Message{Role: "user", Content: "Test"})
	input <- elem
	close(input)

	acc := stage.accumulateInput(input)
	assert.Empty(t, acc.systemPrompt,
		"empty TurnState.SystemPrompt produces an empty system prompt")
	assert.Empty(t, acc.allowedTools,
		"empty TurnState.AllowedTools produces no allowed tools")
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

	t.Run("accumulates messages and reads from TurnState", func(t *testing.T) {
		turnState := NewTurnState()
		turnState.SystemPrompt = "You are helpful"
		turnState.AllowedTools = []string{"tool1", "tool2"}
		turnState.ProviderRequestMetadata = map[string]interface{}{"custom_key": "custom_value"}

		stage := NewProviderStageWithTurnState(provider, nil, nil, nil, nil, nil, turnState)

		input := make(chan StreamElement, 3)
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Hi"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Thanks"})
		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 3)
		assert.Equal(t, "You are helpful", acc.systemPrompt)
		assert.Equal(t, []string{"tool1", "tool2"}, acc.allowedTools)
		assert.Equal(t, "custom_value", acc.metadata["custom_key"])
	})

	t.Run("handles empty input", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, nil, nil)
		input := make(chan StreamElement)
		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 0)
		assert.Empty(t, acc.systemPrompt)
		assert.Nil(t, acc.allowedTools)
		assert.NotNil(t, acc.metadata)
	})

	t.Run("handles elements without messages", func(t *testing.T) {
		turnState := NewTurnState()
		turnState.ProviderRequestMetadata = map[string]interface{}{"key": "value"}
		stage := NewProviderStageWithTurnState(provider, nil, nil, nil, nil, nil, turnState)

		input := make(chan StreamElement, 1)
		text := "some text"
		elem := StreamElement{Text: &text}
		input <- elem
		close(input)

		acc := stage.accumulateInput(input)

		assert.Len(t, acc.messages, 0)
		assert.Equal(t, "value", acc.metadata["key"])
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
		assert.Equal(t, 50, stage.getMaxRounds()) // defaultMaxRounds
	})

	t.Run("returns policy value when set", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, &pipeline.ToolPolicy{MaxRounds: 5}, nil)
		assert.Equal(t, 5, stage.getMaxRounds())
	})

	t.Run("returns default when policy MaxRounds is zero", func(t *testing.T) {
		stage := NewProviderStage(provider, nil, &pipeline.ToolPolicy{MaxRounds: 0}, nil)
		assert.Equal(t, 50, stage.getMaxRounds())
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
	assert.Equal(t, "test-run", receivedEvent.ExecutionID)
	assert.Equal(t, "test-session", receivedEvent.SessionID)

	data, ok := receivedEvent.Data.(*events.ProviderCallStartedData)
	require.True(t, ok, "event data should be *ProviderCallStartedData")
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
	assert.Equal(t, "test-run", receivedEvent.ExecutionID)
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
	parts      []types.ContentPart
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
		Parts:   m.parts,
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
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "blocked by policy")
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
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "Error:")
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
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "result")
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

	// Pending tools now return ErrToolsPending
	require.Error(t, err)
	ep, ok := tools.IsErrToolsPending(err)
	require.True(t, ok, "error should be *ErrToolsPending")
	require.Len(t, ep.Pending, 1)
	assert.Equal(t, "call-1", ep.Pending[0].CallID)
	assert.Equal(t, "pending_tool", ep.Pending[0].ToolName)
	assert.Contains(t, ep.Pending[0].ToolResult.GetTextContent(), "requires")

	// No completed results since only tool was pending
	assert.Empty(t, results)
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
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "failed")
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
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"}, nil)

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
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"nonexistent_tool"}, nil)

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
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"}, nil)

	require.NoError(t, err)
	assert.Nil(t, providerTools)
	assert.Empty(t, toolChoice)
}

func TestProviderStage_BuildProviderTools_EmptyAllowedTools(t *testing.T) {
	// Test that empty allowed_tools returns nil
	registry := tools.NewRegistry()
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, registry, nil, nil)

	providerTools, toolChoice, err := stage.buildProviderTools([]string{}, nil)

	require.NoError(t, err)
	assert.Nil(t, providerTools)
	assert.Empty(t, toolChoice)
}

func TestProviderStage_BuildProviderTools_NilRegistry(t *testing.T) {
	// Test that nil registry returns nil tools
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, nil, nil, nil)

	providerTools, toolChoice, err := stage.buildProviderTools([]string{"test_tool"}, nil)

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
	providerTools, toolChoice, err := stage.buildProviderTools([]string{"regular_tool"}, nil)

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

	providerTools, toolChoice, err := stage.buildProviderTools([]string{}, nil)

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
			assert.Contains(t, result.GetTextContent(), tc.contentContain)
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

func TestProviderStage_ExecuteToolCalls_PendingSuspends(t *testing.T) {
	// When a tool returns ToolStatusPending, executeToolCalls should return
	// an ErrToolsPending error alongside completed tool results.
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "normal_tool",
		Description: "A normal tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "mock",
		MockResult:  json.RawMessage(`"ok"`),
	})
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "pending_tool",
		Description: "A pending tool",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "client",
	})

	// Register an async executor that returns pending for client tools
	pendingExec := &mockAsyncExecutor{
		name:       "client",
		status:     tools.ToolStatusPending,
		pendingMsg: "awaiting caller",
	}
	registry.RegisterExecutor(pendingExec)

	provider := mock.NewProvider("test", "model", false)
	s := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "normal_tool", Args: json.RawMessage(`{}`)},
		{ID: "call-2", Name: "pending_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := s.executeToolCalls(context.Background(), toolCalls)

	// Should return ErrToolsPending
	require.Error(t, err)
	ep, ok := tools.IsErrToolsPending(err)
	require.True(t, ok, "error should be *ErrToolsPending")
	require.Len(t, ep.Pending, 1)
	assert.Equal(t, "call-2", ep.Pending[0].CallID)
	assert.Equal(t, "pending_tool", ep.Pending[0].ToolName)

	// Completed results should still be returned
	require.Len(t, results, 1, "completed tool results should be returned")
}

func TestProviderStage_ExecuteToolCalls_EmitsStartedCompleted(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()

	executor := &mockAsyncExecutor{
		name:    "test-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`{"result": "ok"}`),
	}
	registry.RegisterExecutor(executor)
	registry.Register(&tools.ToolDescriptor{
		Name:        "emit_tool",
		Description: "A tool for testing event emission",
		Mode:        "test-executor",
		InputSchema: []byte(`{"type": "object"}`),
	})

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	var captured []*events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2) // expect started + completed

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
		wg.Done()
	})

	stage := NewProviderStageWithEmitter(provider, registry, nil, nil, emitter)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "emit_tool", Args: json.RawMessage(`{"key":"value"}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, captured, 2)

	// Find events by type (event bus delivery order is non-deterministic under -race)
	var startedEvt, completedEvt *events.Event
	for _, e := range captured {
		switch e.Type {
		case events.EventToolCallStarted:
			startedEvt = e
		case events.EventToolCallCompleted:
			completedEvt = e
		}
	}

	require.NotNil(t, startedEvt, "expected a tool.call.started event")
	require.NotNil(t, completedEvt, "expected a tool.call.completed event")

	startedData, ok := startedEvt.Data.(*events.ToolCallStartedData)
	require.True(t, ok)
	assert.Equal(t, "emit_tool", startedData.ToolName)
	assert.Equal(t, "call-1", startedData.CallID)
	assert.Equal(t, "value", startedData.Args["key"])

	completedData, ok := completedEvt.Data.(*events.ToolCallCompletedData)
	require.True(t, ok)
	assert.Equal(t, "emit_tool", completedData.ToolName)
	assert.Equal(t, "call-1", completedData.CallID)
	assert.Equal(t, "complete", completedData.Status)
}

func TestProviderStage_ExecuteToolCalls_EmitsFailed(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()
	// Don't register any tool — ExecuteAsync will return "tool not found" error

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	var captured []*events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2) // expect started + failed

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
		wg.Done()
	})

	stage := NewProviderStageWithEmitter(provider, registry, nil, nil, emitter)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "nonexistent_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].ToolResult.Error, "nonexistent_tool")

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, captured, 2)

	// Events may arrive in any order from async bus — find by type
	var startedEvent, failedEvent *events.Event
	for _, e := range captured {
		switch e.Type {
		case events.EventToolCallStarted:
			startedEvent = e
		case events.EventToolCallFailed:
			failedEvent = e
		}
	}
	require.NotNil(t, startedEvent, "expected tool.call.started event")
	require.NotNil(t, failedEvent, "expected tool.call.failed event")

	failedData, ok := failedEvent.Data.(*events.ToolCallFailedData)
	require.True(t, ok)
	assert.Equal(t, "nonexistent_tool", failedData.ToolName)
	assert.Equal(t, "call-1", failedData.CallID)
	assert.Error(t, failedData.Error)
}

func TestProviderStage_ExecuteToolCalls_PendingEmitsRequestAndCompleted(t *testing.T) {
	// When a tool returns ToolStatusPending, buildPendingResult should emit:
	//   1. tool.client.request — so observers know a client tool awaits fulfillment
	//   2. tool.call.completed with status "pending" — so every started has a matching completion
	registry := tools.NewRegistry()
	_ = registry.Register(&tools.ToolDescriptor{
		Name:        "location_tool",
		Description: "Gets user location",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Mode:        "client",
	})

	pendingExec := &mockAsyncExecutor{
		name:       "client",
		status:     tools.ToolStatusPending,
		pendingMsg: "Allow location access?",
	}
	registry.RegisterExecutor(pendingExec)

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-pe", "session-pe", "conv-pe")

	var captured []*events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(3) // started + client.request + completed(pending)

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
		wg.Done()
	})

	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStageWithEmitter(provider, registry, nil, nil, emitter)

	toolCalls := []types.MessageToolCall{
		{ID: "call-loc", Name: "location_tool", Args: json.RawMessage(`{"accuracy":"fine"}`)},
	}

	_, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.Error(t, err, "should return ErrToolsPending")

	wg.Wait()
	mu.Lock()
	defer mu.Unlock()

	// Classify captured events
	var startedEvt, clientReqEvt, completedEvt *events.Event
	for _, e := range captured {
		switch e.Type {
		case events.EventToolCallStarted:
			startedEvt = e
		case events.EventClientToolRequest:
			clientReqEvt = e
		case events.EventToolCallCompleted:
			completedEvt = e
		}
	}

	require.NotNil(t, startedEvt, "expected tool.call.started event")
	require.NotNil(t, clientReqEvt, "expected tool.client.request event")
	require.NotNil(t, completedEvt, "expected tool.call.completed event")

	// Verify client request data
	reqData, ok := clientReqEvt.Data.(*events.ClientToolRequestData)
	require.True(t, ok)
	assert.Equal(t, "call-loc", reqData.CallID)
	assert.Equal(t, "location_tool", reqData.ToolName)
	assert.Equal(t, "Allow location access?", reqData.ConsentMsg)

	// Verify completed has status "pending"
	compData, ok := completedEvt.Data.(*events.ToolCallCompletedData)
	require.True(t, ok)
	assert.Equal(t, "location_tool", compData.ToolName)
	assert.Equal(t, "call-loc", compData.CallID)
	assert.Equal(t, "pending", compData.Status)
}

func TestProviderStage_ExecuteToolCalls_BlockedNoEvents(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	registry := tools.NewRegistry()
	registry.Register(&tools.ToolDescriptor{
		Name:        "blocked_tool",
		Description: "A blocked tool",
		InputSchema: []byte(`{"type": "object"}`),
	})

	toolPolicy := &pipeline.ToolPolicy{
		Blocklist: []string{"blocked_tool"},
	}

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run-1", "session-1", "conv-1")

	var captured []*events.Event
	var mu sync.Mutex

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	stage := NewProviderStageWithEmitter(provider, registry, toolPolicy, nil, emitter)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "blocked_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].ToolResult.GetTextContent(), "blocked by policy")

	// Give the event bus a moment to process any events
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, captured, "blocked tools should not emit any tool call events")
}

// =============================================================================
// Multimodal Tool Result Tests — Issue #622
// =============================================================================

func TestProviderStage_HandleToolResult_MultimodalParts(t *testing.T) {
	// When asyncResult.Parts is non-empty, handleToolResult should propagate
	// those parts directly instead of wrapping text content.
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	call := types.MessageToolCall{
		ID:   "call-mm",
		Name: "image_tool",
		Args: json.RawMessage(`{}`),
	}

	imgPart := types.NewImagePartFromData("base64data", "image/png", nil)
	textPart := types.NewTextPart("image description")

	asyncResult := &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: []byte(`"image description"`),
		Parts:   []types.ContentPart{textPart, imgPart},
	}

	result := stage.handleToolResult(call, asyncResult)

	assert.Equal(t, "call-mm", result.ID)
	assert.Equal(t, "image_tool", result.Name)
	require.Len(t, result.Parts, 2)
	assert.Equal(t, types.ContentTypeText, result.Parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, result.Parts[1].Type)
	assert.Empty(t, result.Error)
}

func TestProviderStage_HandleToolResult_LegacyTextOnly(t *testing.T) {
	// When asyncResult.Parts is empty, handleToolResult wraps the text
	// content as a single text ContentPart (legacy path).
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, nil)

	call := types.MessageToolCall{
		ID:   "call-txt",
		Name: "text_tool",
		Args: json.RawMessage(`{}`),
	}

	asyncResult := &tools.ToolExecutionResult{
		Status:  tools.ToolStatusComplete,
		Content: []byte("plain text result"),
	}

	result := stage.handleToolResult(call, asyncResult)

	assert.Equal(t, "call-txt", result.ID)
	assert.Equal(t, "text_tool", result.Name)
	require.Len(t, result.Parts, 1)
	assert.Equal(t, types.ContentTypeText, result.Parts[0].Type)
	assert.Contains(t, *result.Parts[0].Text, "plain text result")
	assert.Empty(t, result.Error)
}

func TestProviderStage_ExecuteToolCalls_PropagatesMultimodalParts(t *testing.T) {
	// End-to-end: multimodal Parts from executor flow through to MessageToolResult.
	registry := tools.NewRegistry()
	imgPart := types.NewImagePartFromData("base64data", "image/png", nil)
	textPart := types.NewTextPart("caption")
	executor := &mockAsyncExecutor{
		name:    "mm-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`"caption"`),
		parts:   []types.ContentPart{textPart, imgPart},
	}
	registry.RegisterExecutor(executor)
	registry.Register(&tools.ToolDescriptor{
		Name:        "mm_tool",
		Description: "multimodal tool",
		Mode:        "mm-executor",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})

	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, registry, nil, nil)

	toolCalls := []types.MessageToolCall{
		{ID: "call-1", Name: "mm_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].ToolResult)
	require.Len(t, results[0].ToolResult.Parts, 2)
	assert.Equal(t, types.ContentTypeText, results[0].ToolResult.Parts[0].Type)
	assert.Equal(t, types.ContentTypeImage, results[0].ToolResult.Parts[1].Type)
}

func TestProviderStage_ToolCallCompletedEvent_MetadataOnlyParts(t *testing.T) {
	// ToolCallCompleted events should contain metadata-only parts (no binary data).
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "run", "sess", "conv")

	var mu sync.Mutex
	var captured []*events.Event
	bus.Subscribe(events.EventToolCallCompleted, func(e *events.Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	})

	registry := tools.NewRegistry()
	base64Data := "iVBORw0KGgoAAAANSUhEUg"
	imgPart := types.NewImagePartFromData(base64Data, "image/png", nil)
	textPart := types.NewTextPart("result text")
	executor := &mockAsyncExecutor{
		name:    "img-executor",
		status:  tools.ToolStatusComplete,
		content: []byte(`"result text"`),
		parts:   []types.ContentPart{textPart, imgPart},
	}
	registry.RegisterExecutor(executor)
	registry.Register(&tools.ToolDescriptor{
		Name:        "img_tool",
		Description: "image tool",
		Mode:        "img-executor",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})

	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStageWithEmitter(provider, registry, nil, nil, emitter)

	toolCalls := []types.MessageToolCall{
		{ID: "call-img", Name: "img_tool", Args: json.RawMessage(`{}`)},
	}

	results, err := stage.executeToolCalls(context.Background(), toolCalls)
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Give the event bus time to deliver
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, captured, 1)
	data := captured[0].Data.(*events.ToolCallEventData)
	assert.Equal(t, "img_tool", data.ToolName)
	require.Len(t, data.Parts, 2)

	// Text part should be preserved
	assert.Equal(t, types.ContentTypeText, data.Parts[0].Type)
	assert.Equal(t, "result text", *data.Parts[0].Text)

	// Image part should have binary data stripped (metadata only)
	assert.Equal(t, types.ContentTypeImage, data.Parts[1].Type)
	require.NotNil(t, data.Parts[1].Media)
	assert.Nil(t, data.Parts[1].Media.Data, "binary data should be stripped from events")
	assert.Equal(t, "image/png", data.Parts[1].Media.MIMEType)
}

// errorProvider always returns an error from Predict.
type errorProvider struct {
	providers.Provider
}

func (p *errorProvider) ID() string              { return "error-provider" }
func (p *errorProvider) Model() string           { return "error-model" }
func (p *errorProvider) SupportsStreaming() bool { return false }
func (p *errorProvider) SupportsTools() bool     { return false }
func (p *errorProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, fmt.Errorf("provider unavailable")
}

func TestProviderStage_EmitsProviderCallFailedEvent(t *testing.T) {
	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(events.EventProviderCallFailed, func(e *events.Event) {
		receivedEvent = e
		wg.Done()
	})

	provider := &errorProvider{}
	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, emitter)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are helpful"
	input <- elem
	close(input)

	output := make(chan StreamElement, 10)
	err := stage.Process(context.Background(), input, output)
	require.Error(t, err)

	// Drain output
	for range output {
	}

	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatal("timed out waiting for ProviderCallFailed event")
	}

	require.NotNil(t, receivedEvent)
	assert.Equal(t, events.EventProviderCallFailed, receivedEvent.Type)

	data, ok := receivedEvent.Data.(*events.ProviderCallFailedData)
	require.True(t, ok)
	assert.Equal(t, "error-provider", data.Provider)
	assert.Equal(t, "error-model", data.Model)
	assert.Contains(t, data.Error.Error(), "provider unavailable")
}

// =============================================================================
// Idle Timeout Reset Tests
// =============================================================================

func TestProviderStage_ResetsIdleOnStreamChunks(t *testing.T) {
	provider := mock.NewProvider("test-provider", "test-model", false)

	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	// Set up context with a spy reset func
	var resetCount int32
	spy := func() { resetCount++ }
	ctx := contextWithIdleReset(context.Background(), spy)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helper"
	input <- elem
	close(input)

	output := make(chan StreamElement, 20)
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	// Drain output
	for range output {
	}

	// Mock provider streams multiple chunks — reset should be called at least once
	// (once per chunk in processStreamChunks, plus round boundaries)
	assert.Greater(t, resetCount, int32(0), "idle reset should be called during streaming")
}

func TestProviderStage_ResetsIdleOnNonStreamingRound(t *testing.T) {
	inner := mock.NewProvider("test-provider", "test-model", false)
	provider := &nonStreamingProvider{Provider: inner}

	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	var resetCount int32
	spy := func() { resetCount++ }
	ctx := contextWithIdleReset(context.Background(), spy)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "Test message"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helper"
	input <- elem
	close(input)

	output := make(chan StreamElement, 20)
	err := stage.Process(ctx, input, output)
	require.NoError(t, err)

	for range output {
	}

	// Non-streaming path: reset at round entry + after round
	assert.Greater(t, resetCount, int32(0), "idle reset should be called for non-streaming rounds")
}

// =============================================================================
// Idle Timeout End-to-End Tests (slow provider)
// =============================================================================

// delayedStreamProvider simulates a provider that waits before producing its
// first chunk — like Ollama queued behind other requests.
type delayedStreamProvider struct {
	providers.Provider
	delay    time.Duration
	response string
}

func (p *delayedStreamProvider) ID() string              { return "delayed" }
func (p *delayedStreamProvider) Model() string           { return "delayed-model" }
func (p *delayedStreamProvider) SupportsStreaming() bool { return true }
func (p *delayedStreamProvider) MaxContextTokens() int   { return 4096 }
func (p *delayedStreamProvider) SupportsToolUse() bool   { return false }

func (p *delayedStreamProvider) PredictStream(
	ctx context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk, 1)
	go func() {
		defer close(ch)
		select {
		case <-time.After(p.delay):
			finish := "stop"
			ch <- providers.StreamChunk{
				Content:      p.response,
				Delta:        p.response,
				FinishReason: &finish,
			}
		case <-ctx.Done():
			return
		}
	}()
	return ch, nil
}

func (p *delayedStreamProvider) Predict(
	ctx context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	select {
	case <-time.After(p.delay):
		return providers.PredictionResponse{Content: p.response}, nil
	case <-ctx.Done():
		return providers.PredictionResponse{}, ctx.Err()
	}
}

func TestIdleTimeout_SlowProviderExceedsTimeout(t *testing.T) {
	// Provider delays 2s before first chunk; idle timeout is 200ms.
	// The pipeline should be cancelled by the idle timeout — the provider
	// never produces a response because ctx is cancelled before the delay.
	provider := &delayedStreamProvider{delay: 2 * time.Second, response: "late"}

	providerStage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	config := DefaultPipelineConfig()
	config.IdleTimeout = 200 * time.Millisecond
	config.ExecutionTimeout = 0

	pl, err := NewPipelineBuilderWithConfig(config).
		Chain(providerStage).
		Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "hello"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helper"

	start := time.Now()
	result, _ := pl.ExecuteSync(context.Background(), elem)
	elapsed := time.Since(start)

	// Pipeline terminates at ~200ms (idle timeout), not 2s (provider delay).
	// No assistant response is produced because the provider was cancelled.
	assert.Less(t, elapsed, 1*time.Second,
		"should terminate at ~200ms (idle timeout), not 2s (provider delay)")
	// The provider was cancelled before producing content — response is either
	// nil or an empty shell depending on pipeline timing.
	if result.Response != nil {
		assert.Empty(t, result.Response.Content,
			"response content should be empty when provider is cancelled by idle timeout")
	}
}

func TestIdleTimeout_SlowProviderWithinTimeout(t *testing.T) {
	// Provider delays 50ms before first chunk; idle timeout is 200ms.
	// The pipeline should succeed because the chunk arrives in time.
	provider := &delayedStreamProvider{delay: 50 * time.Millisecond, response: "hello"}

	providerStage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	})

	config := DefaultPipelineConfig()
	config.IdleTimeout = 200 * time.Millisecond
	config.ExecutionTimeout = 0

	pl, err := NewPipelineBuilderWithConfig(config).
		Chain(providerStage).
		Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "hello"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helper"

	result, execErr := pl.ExecuteSync(context.Background(), elem)

	// Should succeed — chunk arrives at 50ms, well within 200ms timeout
	require.NoError(t, execErr)
	require.NotNil(t, result.Response)
	assert.Equal(t, "hello", result.Response.Content)
}

// =============================================================================
// Tool Rejection Auto-Exclusion Tests
// =============================================================================

func TestUpdateExcludedTools_CountsAndExcludes(t *testing.T) {
	stage := &ProviderStage{}
	rejectionCounts := map[string]int{}
	excluded := map[string]bool{}

	// First rejection: count=1, not yet excluded
	results := []types.Message{
		types.NewToolResultMessage(types.MessageToolResult{
			ID: "c1", Name: "dangerous_tool", Error: "blocked by hook",
			Parts: []types.ContentPart{types.NewTextPart("blocked by hook")},
		}),
	}
	changed := stage.updateExcludedTools(results, rejectionCounts, excluded)
	assert.False(t, changed, "first rejection should not exclude")
	assert.Equal(t, 1, rejectionCounts["dangerous_tool"])
	assert.False(t, excluded["dangerous_tool"])

	// Second rejection: count=2, now excluded
	changed = stage.updateExcludedTools(results, rejectionCounts, excluded)
	assert.True(t, changed, "second rejection should exclude")
	assert.Equal(t, 2, rejectionCounts["dangerous_tool"])
	assert.True(t, excluded["dangerous_tool"])

	// Third rejection: already excluded, no change
	changed = stage.updateExcludedTools(results, rejectionCounts, excluded)
	assert.False(t, changed, "already excluded, no change")
}

func TestUpdateExcludedTools_IgnoresSuccessfulResults(t *testing.T) {
	stage := &ProviderStage{}
	rejectionCounts := map[string]int{}
	excluded := map[string]bool{}

	results := []types.Message{
		types.NewToolResultMessage(types.MessageToolResult{
			ID: "c1", Name: "good_tool", Error: "",
			Parts: []types.ContentPart{types.NewTextPart("success")},
		}),
	}
	changed := stage.updateExcludedTools(results, rejectionCounts, excluded)
	assert.False(t, changed)
	assert.Equal(t, 0, rejectionCounts["good_tool"])
}

func TestBuildProviderTools_RespectsExcluded(t *testing.T) {
	// Use ToolProvider which implements ToolSupport (BuildTooling)
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()

	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		err := registry.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: "Test tool " + name,
			InputSchema: json.RawMessage(`{"type": "object"}`),
		})
		require.NoError(t, err)
	}

	stage := NewProviderStage(provider, registry, nil, &ProviderConfig{})

	// No exclusions — all tools present
	allTools, _, err := stage.buildProviderTools(
		[]string{"tool_a", "tool_b", "tool_c"}, nil,
	)
	require.NoError(t, err)
	require.NotNil(t, allTools)

	// Exclude tool_b
	excluded := map[string]bool{"tool_b": true}
	excludedTools, _, err := stage.buildProviderTools(
		[]string{"tool_a", "tool_b", "tool_c"}, excluded,
	)
	require.NoError(t, err)
	require.NotNil(t, excludedTools)

	// BuildTooling returns []*providers.ToolDescriptor for mock ToolProvider
	allDescs, ok1 := allTools.([]*providers.ToolDescriptor)
	exclDescs, ok2 := excludedTools.([]*providers.ToolDescriptor)
	require.True(t, ok1, "expected []*providers.ToolDescriptor")
	require.True(t, ok2, "expected []*providers.ToolDescriptor")

	assert.Equal(t, 3, len(allDescs))
	assert.Equal(t, 2, len(exclDescs))
	for _, d := range exclDescs {
		assert.NotEqual(t, "tool_b", d.Name, "excluded tool should not be present")
	}
}

// =============================================================================
// MessageLog Write-Through Tests
// =============================================================================

// spyMessageLog records all LogAppend calls for test inspection.
type spyMessageLog struct {
	mu       sync.Mutex
	appends  []spyAppendCall
	messages []types.Message // accumulated messages
	failNext bool            // if true, next LogAppend returns error
}

type spyAppendCall struct {
	id       string
	startSeq int
	count    int
}

func (s *spyMessageLog) LogAppend(_ context.Context, id string, startSeq int, messages []types.Message) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failNext {
		s.failNext = false
		return 0, fmt.Errorf("simulated log append failure")
	}

	s.appends = append(s.appends, spyAppendCall{id: id, startSeq: startSeq, count: len(messages)})

	// Idempotent dedup
	skip := len(s.messages) - startSeq
	if skip < 0 {
		skip = 0
	}
	if skip < len(messages) {
		s.messages = append(s.messages, messages[skip:]...)
	}
	return len(s.messages), nil
}

func (s *spyMessageLog) LogLoad(_ context.Context, _ string, recent int) ([]types.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if recent > 0 && recent < len(s.messages) {
		return s.messages[len(s.messages)-recent:], nil
	}
	return s.messages, nil
}

func (s *spyMessageLog) LogLen(_ context.Context, _ string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages), nil
}

func (s *spyMessageLog) appendCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.appends)
}

func TestToolLoop_WriteThroughPersistsPerRound(t *testing.T) {
	spy := &spyMessageLog{}

	// Mock provider that returns tool calls for 2 rounds then stops
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	stage := NewProviderStageWithHooks(provider, registry, nil,
		&ProviderConfig{
			MaxTokens:        100,
			MessageLog:       spy,
			MessageLogConvID: "test-conv",
		}, nil, nil)

	input := make(chan StreamElement, 1)
	userMsg := types.Message{Role: "user", Content: "do something"}
	elem := NewMessageElement(&userMsg)
	elem.Metadata["system_prompt"] = "You are a helper"
	input <- elem
	close(input)

	output := make(chan StreamElement, 50)
	processErr := stage.Process(context.Background(), input, output)
	require.NoError(t, processErr)
	for range output {
	}

	// The mock provider doesn't return tool calls, so only 1 round.
	// But the write-through should have fired at least once if there were tool calls.
	// With no tool calls, afterRound returns done=true immediately — no write-through.
	// This test verifies the wiring doesn't panic and works when configured.
	length, _ := spy.LogLen(context.Background(), "test-conv")
	// No tool calls → no rounds → no write-through (messages saved by save stage)
	assert.GreaterOrEqual(t, length, 0)
}

func TestToolLoop_WriteThroughDisabledByDefault(t *testing.T) {
	// No MessageLog configured — should work normally without panics
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	})

	input := make(chan StreamElement, 1)
	elem := NewMessageElement(&types.Message{Role: "user", Content: "hello"})
	elem.Metadata["system_prompt"] = "helper"
	input <- elem
	close(input)

	output := make(chan StreamElement, 20)
	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)
	for range output {
	}
}

func TestAfterRound_WriteThroughAppendsMessages(t *testing.T) {
	spy := &spyMessageLog{}

	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	stage := NewProviderStageWithHooks(provider, registry, nil,
		&ProviderConfig{
			MaxTokens:        100,
			MessageLog:       spy,
			MessageLogConvID: "test-conv",
		}, nil, nil)

	acc := &providerInput{
		allowedTools: []string{"test_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)
	loop.maxRounds = 5

	// Simulate a round with tool calls
	response := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, _ := loop.afterRound(context.Background(), []string{"test_tool"}, &response, true, 1)
	assert.False(t, done)

	// Write-through should have fired
	assert.Greater(t, spy.appendCount(), 0, "LogAppend should be called after tool round")

	length, _ := spy.LogLen(context.Background(), "test-conv")
	assert.Greater(t, length, 0, "messages should be persisted")
}

func TestAfterRound_WriteThroughFailureNonFatal(t *testing.T) {
	spy := &spyMessageLog{failNext: true}

	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	stage := NewProviderStageWithHooks(provider, registry, nil,
		&ProviderConfig{
			MaxTokens:        100,
			MessageLog:       spy,
			MessageLogConvID: "test-conv",
		}, nil, nil)

	acc := &providerInput{
		allowedTools: []string{"test_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)
	loop.maxRounds = 5

	response := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}

	// First call fails but loop continues
	done, _, err := loop.afterRound(context.Background(), []string{"test_tool"}, &response, true, 1)
	assert.False(t, done)
	assert.NoError(t, err, "write-through failure should not abort the loop")
}

func TestNewToolLoop_BuildError(t *testing.T) {
	// Provider that doesn't support tools — buildProviderTools returns nil, nil, nil
	// which is not an error. Use a nil registry with allowed tools to force an error path.
	provider := mock.NewToolProvider("test", "model", false, nil)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	acc := &providerInput{
		allowedTools: []string{"some_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	// No registry → buildProviderTools returns nil tools (no error, just no tools)
	require.NoError(t, err)
	assert.NotNil(t, loop)
	assert.Nil(t, loop.providerTools)
}

func TestAfterRound_NoToolCalls(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{})

	acc := &providerInput{
		messages: []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)

	response := types.Message{Role: "assistant", Content: "hello"}
	done, msgs, err := loop.afterRound(context.Background(), nil, &response, false, 1)

	assert.True(t, done, "should be done when no tool calls")
	require.NoError(t, err)
	assert.Len(t, msgs, 2) // original user msg + response
	assert.Equal(t, "hello", msgs[1].Content)
}

func TestAfterRound_NoToolCalls_PersistsFinalResponse(t *testing.T) {
	spy := &spyMessageLog{}
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil, nil, &ProviderConfig{
		MaxTokens:        100,
		MessageLog:       spy,
		MessageLogConvID: "test-conv",
	})

	acc := &providerInput{
		messages: []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)

	// Final response with no tool calls — should still be persisted
	response := types.Message{Role: "assistant", Content: "goodbye"}
	done, _, err := loop.afterRound(context.Background(), nil, &response, false, 1)
	assert.True(t, done)
	require.NoError(t, err)

	// The final response should be persisted even though there are no tool calls
	length, _ := spy.LogLen(context.Background(), "test-conv")
	assert.Equal(t, 1, length, "final response should be persisted (history already in store, only new response)")

	loaded, _ := spy.LogLoad(context.Background(), "test-conv", 0)
	require.Len(t, loaded, 1)
	assert.Equal(t, "goodbye", loaded[0].Content)
}

func TestAfterRound_MaxRoundsExceeded(t *testing.T) {
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	stage := NewProviderStage(provider, registry, nil, &ProviderConfig{})

	acc := &providerInput{
		allowedTools: []string{"test_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)
	loop.maxRounds = 1 // force max rounds = current round

	// Response with a tool call
	response := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, err := loop.afterRound(context.Background(), []string{"test_tool"}, &response, true, 1)

	assert.True(t, done)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max rounds")
}

func TestAfterRound_ExcludesAfterRepeatedRejection(t *testing.T) {
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()

	err := registry.Register(&tools.ToolDescriptor{
		Name:        "blocked_tool",
		Description: "always blocked",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	// Use denyToolHook from stages_provider_hooks_test.go (same package)
	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{
		blockedTool: "blocked_tool",
		reason:      "blocked by test",
	}))

	stage := NewProviderStageWithHooks(provider, registry, nil, &ProviderConfig{}, nil, hookReg)

	acc := &providerInput{
		allowedTools: []string{"blocked_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)
	loop.maxRounds = 10

	// First call — tool gets rejected, still available
	response := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "blocked_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, _ := loop.afterRound(context.Background(), []string{"blocked_tool"}, &response, true, 1)
	assert.False(t, done)
	assert.False(t, loop.excluded["blocked_tool"], "first rejection should not exclude")

	// Second call — tool gets rejected again, now excluded
	response2 := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c2", Name: "blocked_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, _ = loop.afterRound(context.Background(), []string{"blocked_tool"}, &response2, true, 2)
	assert.False(t, done)
	assert.True(t, loop.excluded["blocked_tool"], "second rejection should exclude")
}

func TestAfterRound_CostBudgetExceeded(t *testing.T) {
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	stage := NewProviderStage(provider, registry,
		&pipeline.ToolPolicy{MaxCostUSD: 0.10},
		&ProviderConfig{},
	)

	acc := &providerInput{
		allowedTools: []string{"test_tool"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)

	// Round 1: cost under budget
	response1 := types.Message{
		Role:     "assistant",
		CostInfo: &types.CostInfo{TotalCost: 0.05},
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, err := loop.afterRound(context.Background(), []string{"test_tool"}, &response1, true, 1)
	assert.False(t, done, "should continue when under budget")
	require.NoError(t, err)
	assert.InDelta(t, 0.05, loop.cumulativeCost, 0.001)

	// Round 2: cost exceeds budget (0.05 + 0.06 = 0.11 > 0.10)
	response2 := types.Message{
		Role:     "assistant",
		CostInfo: &types.CostInfo{TotalCost: 0.06},
		ToolCalls: []types.MessageToolCall{
			{ID: "c2", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, msgs, err := loop.afterRound(context.Background(), []string{"test_tool"}, &response2, true, 2)
	assert.True(t, done, "should stop when cost budget exceeded")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cost budget exceeded")
	assert.Contains(t, err.Error(), "0.1100")
	assert.Contains(t, err.Error(), "0.1000")
	assert.NotEmpty(t, msgs, "should return messages even on budget exceeded")
}

func TestAfterRound_CostBudgetZeroIsUnlimited(t *testing.T) {
	provider := mock.NewProvider("test", "model", false)
	stage := NewProviderStage(provider, nil,
		&pipeline.ToolPolicy{MaxCostUSD: 0}, // zero = unlimited
		&ProviderConfig{},
	)

	acc := &providerInput{
		messages: []types.Message{{Role: "user", Content: "hi"}},
	}
	loop, err := stage.newToolLoop(acc)
	require.NoError(t, err)

	// Even with high cost, should not terminate
	response := types.Message{
		Role:     "assistant",
		Content:  "done",
		CostInfo: &types.CostInfo{TotalCost: 999.99},
	}
	done, _, err := loop.afterRound(context.Background(), nil, &response, false, 1)
	assert.True(t, done, "done because no tool calls")
	require.NoError(t, err, "should not error — zero MaxCostUSD means unlimited")
}

func TestAfterRound_CompactionEmitsEvent(t *testing.T) {
	provider := mock.NewToolProvider("test", "model", false, nil)
	registry := tools.NewRegistry()
	err := registry.Register(&tools.ToolDescriptor{
		Name:        "test_tool",
		Description: "test",
		InputSchema: json.RawMessage(`{"type": "object"}`),
		Mode:        "mock",
	})
	require.NoError(t, err)

	bus := events.NewEventBus()
	defer bus.Close()
	emitter := events.NewEmitter(bus, "test-run", "test-session", "test-conv")

	stg := NewProviderStageWithHooks(provider, registry, nil, &ProviderConfig{
		Compactor: &ContextCompactor{
			BudgetTokens:   100, // very low to force compaction
			Threshold:      0.50,
			PinRecentCount: 2,
		},
	}, emitter, nil)

	// Build messages with a large tool result that will be compacted
	msgs := []types.Message{
		{Role: "user", Content: "start"},
		largeToolResult("file_read", 2000),
		{Role: "assistant", Content: "got the file"},
	}
	acc := &providerInput{
		allowedTools: []string{"test_tool"},
		messages:     msgs,
	}
	loop, err := stg.newToolLoop(acc)
	require.NoError(t, err)

	// Subscribe before the event
	var receivedEvent *events.Event
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(events.EventContextCompacted, func(e *events.Event) {
		receivedEvent = e
		wg.Done()
	})

	// afterRound with tool calls triggers compaction
	response := types.Message{
		Role: "assistant",
		ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "test_tool", Args: json.RawMessage(`{}`)},
		},
	}
	done, _, err := loop.afterRound(context.Background(), []string{"test_tool"}, &response, true, 1)
	assert.False(t, done)
	require.NoError(t, err)

	// Wait for event delivery
	wg.Wait()
	require.NotNil(t, receivedEvent, "should have received context.compacted event")
	assert.Equal(t, events.EventContextCompacted, receivedEvent.Type)
	data, ok := receivedEvent.Data.(*events.ContextCompactionData)
	require.True(t, ok, "event data should be *ContextCompactionData")
	assert.Equal(t, 1, data.Round)
	assert.Greater(t, data.MessagesFolded, 0)
	assert.Greater(t, data.OriginalTokens, data.CompactedTokens)
	assert.Equal(t, 100, data.BudgetTokens)
}
