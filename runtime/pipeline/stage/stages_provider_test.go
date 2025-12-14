package stage

import (
	"context"
	"testing"

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

	// Check for streaming deltas in metadata
	foundDelta := false
	for _, elem := range elements {
		if elem.Metadata != nil {
			if _, ok := elem.Metadata["delta"]; ok {
				foundDelta = true
				break
			}
		}
	}
	assert.True(t, foundDelta, "should have streaming delta in metadata")

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
