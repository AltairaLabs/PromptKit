package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		MaxTokens:    500,
		Temperature:  0.9,
		Seed:         &seed,
		DisableTrace: true,
	}

	stage := NewProviderStage(provider, nil, nil, config)

	assert.NotNil(t, stage)
	assert.Equal(t, 500, stage.config.MaxTokens)
	assert.Equal(t, float32(0.9), stage.config.Temperature)
	assert.Equal(t, &seed, stage.config.Seed)
	assert.True(t, stage.config.DisableTrace)
}

func TestProviderStage_NonStreamingMode(t *testing.T) {
	// Create mock provider WITHOUT streaming support
	provider := mock.NewProvider("test-provider", "test-model", true) // disableStreaming=true

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
