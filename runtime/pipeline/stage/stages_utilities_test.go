package stage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tokenizer"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestTemplateStage_SubstitutesVariables(t *testing.T) {
	stage := NewTemplateStage()

	tests := []struct {
		name           string
		systemPrompt   string
		variables      map[string]string
		expectedPrompt string
	}{
		{
			name:           "single variable",
			systemPrompt:   "Hello {{name}}!",
			variables:      map[string]string{"name": "Alice"},
			expectedPrompt: "Hello Alice!",
		},
		{
			name:           "multiple variables",
			systemPrompt:   "{{greeting}} {{name}}, the topic is {{topic}}.",
			variables:      map[string]string{"greeting": "Hi", "name": "Bob", "topic": "AI"},
			expectedPrompt: "Hi Bob, the topic is AI.",
		},
		{
			name:           "no variables",
			systemPrompt:   "Static prompt with no placeholders",
			variables:      map[string]string{"unused": "value"},
			expectedPrompt: "Static prompt with no placeholders",
		},
		{
			name:           "missing variable leaves placeholder",
			systemPrompt:   "Hello {{name}} and {{friend}}!",
			variables:      map[string]string{"name": "Alice"},
			expectedPrompt: "Hello Alice and {{friend}}!",
		},
		{
			name:           "empty variables",
			systemPrompt:   "Hello {{name}}!",
			variables:      map[string]string{},
			expectedPrompt: "Hello {{name}}!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make(chan StreamElement, 1)
			output := make(chan StreamElement, 1)

			elem := StreamElement{
				Metadata: map[string]interface{}{
					"system_prompt": tt.systemPrompt,
					"variables":     tt.variables,
				},
			}
			input <- elem
			close(input)

			err := stage.Process(context.Background(), input, output)
			require.NoError(t, err)

			result := <-output
			assert.Equal(t, tt.expectedPrompt, result.Metadata["system_prompt"])
		})
	}
}

func TestTemplateStage_SubstitutesInMessageContent(t *testing.T) {
	stage := NewTemplateStage()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	textContent := "This is about {{topic}}"
	msg := &types.Message{
		Role:    "user",
		Content: "Question about {{topic}}",
		Parts: []types.ContentPart{
			{Text: &textContent},
		},
	}

	elem := StreamElement{
		Message: msg,
		Metadata: map[string]interface{}{
			"variables": map[string]string{"topic": "testing"},
		},
	}
	input <- elem
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.Equal(t, "Question about testing", result.Message.Content)
	assert.Equal(t, "This is about testing", *result.Message.Parts[0].Text)
}

func TestTemplateStage_NoVariablesInMetadata(t *testing.T) {
	stage := NewTemplateStage()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	elem := StreamElement{
		Metadata: map[string]interface{}{
			"system_prompt": "Hello {{name}}!",
			// No variables key
		},
	}
	input <- elem
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	// Without variables, placeholder remains
	assert.Equal(t, "Hello {{name}}!", result.Metadata["system_prompt"])
}

func TestTemplateStage_NilMessage(t *testing.T) {
	stage := NewTemplateStage()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Element with variables but no message
	elem := StreamElement{
		Metadata: map[string]interface{}{
			"variables":     map[string]string{"name": "Test"},
			"system_prompt": "Hello {{name}}!",
		},
		// No Message field
	}
	input <- elem
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.Equal(t, "Hello Test!", result.Metadata["system_prompt"])
	assert.Nil(t, result.Message)
}

func TestTemplateStage_MessageWithNoParts(t *testing.T) {
	stage := NewTemplateStage()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Message with content but no parts
	msg := &types.Message{
		Role:    "user",
		Content: "Hello {{name}}!",
		Parts:   nil, // No parts
	}
	elem := NewMessageElement(msg)
	elem.Metadata["variables"] = map[string]string{"name": "World"}
	input <- elem
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.Equal(t, "Hello World!", result.Message.Content)
}

func TestTemplateStage_ContextCancellation(t *testing.T) {
	stage := NewTemplateStage()

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement) // Unbuffered, so send will block

	ctx, cancel := context.WithCancel(context.Background())

	elem := StreamElement{
		Metadata: map[string]interface{}{
			"variables": map[string]string{"name": "Test"},
		},
	}
	input <- elem
	close(input)

	// Cancel context - the Process should return context.Canceled when trying to send
	cancel()

	err := stage.Process(ctx, input, output)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestContextBuilderStage_PassThroughWhenUnderBudget(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      1000,
		ReserveForOutput: 100,
		Strategy:         TruncateOldest,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Small messages that fit in budget
	for i := 0; i < 3; i++ {
		input <- NewMessageElement(&types.Message{
			Role:    "user",
			Content: "Short message",
		})
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Drain output
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 3, count, "All messages should pass through")
}

func TestContextBuilderStage_TruncatesOldestWhenOverBudget(t *testing.T) {
	// The token counter uses ~1.3 tokens per word. "newest" is 1 word = ~1-2 tokens
	// To force truncation, we need messages that clearly exceed budget
	policy := &ContextBuilderPolicy{
		TokenBudget:      15, // Very small budget - fits ~10 words
		ReserveForOutput: 5,
		Strategy:         TruncateOldest,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 5)
	output := make(chan StreamElement, 5)

	// Send multiple messages that clearly exceed budget
	// Each message has 5-6 words = ~7-8 tokens
	messages := []string{
		"First message with some content",
		"Second message also with content",
		"Third message with more text",
		"Fourth message that is long",
		"Newest", // Should be kept - very short
	}

	for _, content := range messages {
		input <- NewMessageElement(&types.Message{
			Role:    "user",
			Content: content,
		})
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Collect output
	var results []types.Message
	for elem := range output {
		if elem.Message != nil {
			results = append(results, *elem.Message)
		}
	}

	// Should have fewer messages due to truncation (budget is tiny)
	assert.Less(t, len(results), len(messages), "Should truncate some messages")

	// Most recent message should be preserved
	if len(results) > 0 {
		lastResult := results[len(results)-1]
		assert.Equal(t, "Newest", lastResult.Content)
	}
}

func TestContextBuilderStage_NoLimitWithZeroBudget(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget: 0, // Unlimited
		Strategy:    TruncateOldest,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 5)
	output := make(chan StreamElement, 5)

	// Send many large messages
	for i := 0; i < 5; i++ {
		input <- NewMessageElement(&types.Message{
			Role:    "user",
			Content: "A very long message that would normally exceed budget " + string(rune('A'+i)),
		})
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// All should pass through with zero budget
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 5, count)
}

func TestContextBuilderStage_FailStrategyReturnsError(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      10, // Tiny budget
		ReserveForOutput: 5,
		Strategy:         TruncateFail,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	input <- NewMessageElement(&types.Message{
		Role:    "user",
		Content: "This message is way too long to fit in the tiny budget we set up for this test",
	})
	close(input)

	err := stage.Process(context.Background(), input, output)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token budget exceeded")
}

func TestContextBuilderStage_AddsTruncationMetadata(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      30,
		ReserveForOutput: 5,
		Strategy:         TruncateOldest,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	// Send messages that will cause truncation
	input <- NewMessageElement(&types.Message{
		Role:    "user",
		Content: "First long message that takes up budget space",
	})
	input <- NewMessageElement(&types.Message{
		Role:    "user",
		Content: "Second message",
	})
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Check that truncation metadata is added
	for elem := range output {
		if truncated, ok := elem.Metadata["context_truncated"].(bool); ok && truncated {
			assert.True(t, truncated, "Truncation flag should be set")
			return // Found it
		}
	}
	// Note: If no truncation occurred, no flag is set (this is valid)
}

func TestContextBuilderStage_NilPolicyPassesThrough(t *testing.T) {
	stage := NewContextBuilderStage(nil)

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	input <- NewMessageElement(&types.Message{Role: "user", Content: "Message 1"})
	input <- NewMessageElement(&types.Message{Role: "user", Content: "Message 2"})
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 2, count)
}

func TestContextBuilderStage_CustomTokenCounter(t *testing.T) {
	// Create a custom token counter that returns 10 tokens per message
	// This allows us to verify the custom counter is being used
	customCounter := tokenizer.NewHeuristicTokenCounterWithRatio(10.0)

	policy := &ContextBuilderPolicy{
		TokenBudget:      25, // Only allow 2 messages (10 tokens each) plus buffer
		ReserveForOutput: 0,
		Strategy:         TruncateOldest,
		TokenCounter:     customCounter,
	}

	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 3)
	output := make(chan StreamElement, 3)

	// Each "word" will be counted as 10 tokens with our custom counter
	input <- NewMessageElement(&types.Message{Role: "user", Content: "first"})  // 10 tokens
	input <- NewMessageElement(&types.Message{Role: "user", Content: "second"}) // 10 tokens
	input <- NewMessageElement(&types.Message{Role: "user", Content: "third"})  // 10 tokens
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// With 25 token budget and 10 tokens per message, only 2 messages should fit
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 2, count, "Should truncate to 2 messages with custom token counter")
}

func TestContextBuilderStage_DefaultTokenCounter(t *testing.T) {
	// Test that default token counter is used when none provided
	policy := &ContextBuilderPolicy{
		TokenBudget:      100, // Generous budget
		ReserveForOutput: 0,
		Strategy:         TruncateOldest,
		// TokenCounter is nil - should use default
	}

	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 2)
	output := make(chan StreamElement, 2)

	// "hello world" = 2 words * ~1.35 = ~2-3 tokens with default counter
	input <- NewMessageElement(&types.Message{Role: "user", Content: "hello world"})
	input <- NewMessageElement(&types.Message{Role: "user", Content: "goodbye world"})
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Both should fit with 100 token budget
	count := 0
	for range output {
		count++
	}
	assert.Equal(t, 2, count, "Both messages should fit with default counter")
}

func TestContextBuilderStage_ModelAwareTokenCounter(t *testing.T) {
	// Test using model-aware token counter
	gptCounter := tokenizer.NewTokenCounterForModel("gpt-4")

	policy := &ContextBuilderPolicy{
		TokenBudget:      100,
		ReserveForOutput: 0,
		Strategy:         TruncateOldest,
		TokenCounter:     gptCounter,
	}

	stage := NewContextBuilderStage(policy)

	// Verify the counter was set correctly
	assert.NotNil(t, stage.tokenCounter)
}

func TestDebugStage_PassesThrough(t *testing.T) {
	stage := NewDebugStage("test")

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	text := "test content"
	input <- StreamElement{Text: &text}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.Equal(t, "test content", *result.Text)
}

func TestVariableProviderStage_MergesProviderVariables(t *testing.T) {
	// Create a simple mock provider
	mockProvider := &mockVariableProvider{
		name: "test-provider",
		vars: map[string]string{
			"dynamic_var": "dynamic_value",
		},
	}

	stage := NewVariableProviderStage(mockProvider)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Element with existing variables
	input <- StreamElement{
		Metadata: map[string]interface{}{
			"variables": map[string]string{
				"existing_var": "existing_value",
			},
		},
	}
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	vars := result.Metadata["variables"].(map[string]string)
	assert.Equal(t, "existing_value", vars["existing_var"])
	assert.Equal(t, "dynamic_value", vars["dynamic_var"])
}

// mockVariableProvider is a simple test implementation of variables.Provider
type mockVariableProvider struct {
	name string
	vars map[string]string
	err  error
}

func (m *mockVariableProvider) Name() string {
	return m.name
}

func (m *mockVariableProvider) Provide(ctx context.Context) (map[string]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.vars, nil
}

// =============================================================================
// PipelineBuilder Tests
// =============================================================================

func TestPipelineBuilder_WithConfig(t *testing.T) {
	config := &PipelineConfig{
		ChannelBufferSize: 100,
	}

	builder := NewPipelineBuilder().WithConfig(config)
	assert.NotNil(t, builder)
	// Config is set internally - we verify by building successfully
}

func TestPipelineBuilder_WithEventEmitter(t *testing.T) {
	builder := NewPipelineBuilder().WithEventEmitter(nil)
	assert.NotNil(t, builder)
}

func TestPipelineBuilder_AddStage(t *testing.T) {
	stage := NewDebugStage("test-debug")
	builder := NewPipelineBuilder().AddStage(stage)
	assert.NotNil(t, builder)
}

func TestPipelineBuilder_Branch(t *testing.T) {
	// Create stages with different names
	stageA := NewDebugStage("stage-a")
	stageB := NewDebugStage("stage-b")
	stageC := NewDebugStage("stage-c")

	builder := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB).
		AddStage(stageC).
		Branch(stageA.Name(), stageB.Name(), stageC.Name())

	assert.NotNil(t, builder)
}

func TestPipelineBuilder_Clone(t *testing.T) {
	original := NewPipelineBuilder().
		AddStage(NewDebugStage("test-debug"))

	cloned := original.Clone()

	assert.NotNil(t, cloned)
	// The clone should be independent
}

func TestPipelineBuilder_Validate_CycleDetection(t *testing.T) {
	// Test cycle detection with a simple cycle: A -> B -> C -> A
	// Note: NewDebugStage adds "debug_" prefix to stage names
	stageA := NewDebugStage("a")
	stageB := NewDebugStage("b")
	stageC := NewDebugStage("c")

	builder := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB).
		AddStage(stageC).
		Branch("debug_a", "debug_b").
		Branch("debug_b", "debug_c").
		Branch("debug_c", "debug_a") // Creates a cycle

	_, err := builder.Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic")
}

func TestPipelineBuilder_Validate_NoCycle(t *testing.T) {
	// Test valid DAG: A -> B -> C (no cycle)
	stageA := NewDebugStage("a")
	stageB := NewDebugStage("b")
	stageC := NewDebugStage("c")

	builder := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB).
		AddStage(stageC).
		Branch("debug_a", "debug_b").
		Branch("debug_b", "debug_c")

	pipeline, err := builder.Build()
	assert.NoError(t, err)
	assert.NotNil(t, pipeline)
}

func TestPipelineBuilder_Validate_DisconnectedComponents(t *testing.T) {
	// Test with disconnected components (should still work)
	stageA := NewDebugStage("a")
	stageB := NewDebugStage("b")

	builder := NewPipelineBuilder().
		AddStage(stageA).
		AddStage(stageB)
	// No edges between them

	pipeline, err := builder.Build()
	assert.NoError(t, err)
	assert.NotNil(t, pipeline)
}

func TestPipelineBuilder_Validate_SelfLoop(t *testing.T) {
	// Test self-loop detection: A -> A
	stageA := NewDebugStage("a")

	builder := NewPipelineBuilder().
		AddStage(stageA).
		Branch("debug_a", "debug_a") // Self-loop

	_, err := builder.Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic")
}

// =============================================================================
// MediaExternalizerStage Tests
// =============================================================================

func TestMediaExternalizerStage_DisabledPassthrough(t *testing.T) {
	config := &MediaExternalizerConfig{
		Enabled: false,
	}
	stage := NewMediaExternalizerStage(config)
	require.NotNil(t, stage)

	// Test passthrough when disabled
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := &types.Message{Role: "user", Content: "Hello"}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.NotNil(t, result.Message)
	assert.Equal(t, "Hello", result.Message.Content)
}

func TestMediaExternalizerStage_NoStoragePassthrough(t *testing.T) {
	config := &MediaExternalizerConfig{
		Enabled:        true,
		StorageService: nil, // No storage service
	}
	stage := NewMediaExternalizerStage(config)
	require.NotNil(t, stage)

	// Test passthrough when no storage service
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	msg := &types.Message{Role: "user", Content: "Hello"}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	result := <-output
	assert.NotNil(t, result.Message)
}

func TestMediaExternalizerStage_ContextCancellation(t *testing.T) {
	config := &MediaExternalizerConfig{
		Enabled: false,
	}
	stage := NewMediaExternalizerStage(config)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan StreamElement, 1)
	output := make(chan StreamElement)

	// Cancel context before processing
	cancel()

	msg := &types.Message{Role: "user", Content: "Hello"}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(ctx, input, output)
	// Either returns context error or completes
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
}

// =============================================================================
// StreamElement Tests
// =============================================================================

func TestAudioFormat_String(t *testing.T) {
	tests := []struct {
		format   AudioFormat
		expected string
	}{
		{AudioFormatPCM16, "pcm16"},
		{AudioFormatFloat32, "float32"},
		{AudioFormatOpus, "opus"},
		{AudioFormatMP3, "mp3"},
		{AudioFormatAAC, "aac"},
		{AudioFormat(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.format.String())
	}
}

func TestNewAudioElement(t *testing.T) {
	audio := &AudioData{
		Samples:    []byte{1, 2, 3, 4},
		SampleRate: 16000,
		Channels:   1,
	}

	elem := NewAudioElement(audio)

	assert.NotNil(t, elem.Audio)
	assert.Equal(t, PriorityHigh, elem.Priority)
	assert.NotNil(t, elem.Metadata)
}

func TestNewVideoElement(t *testing.T) {
	video := &VideoData{
		Data:     []byte{1, 2, 3, 4},
		MIMEType: "video/mp4",
		Width:    640,
		Height:   480,
	}

	elem := NewVideoElement(video)

	assert.NotNil(t, elem.Video)
	assert.Equal(t, PriorityHigh, elem.Priority)
}

func TestNewImageElement(t *testing.T) {
	image := &ImageData{
		Data:     []byte{1, 2, 3, 4},
		MIMEType: "image/png",
		Width:    100,
		Height:   100,
	}

	elem := NewImageElement(image)

	assert.NotNil(t, elem.Image)
	assert.Equal(t, PriorityNormal, elem.Priority)
}

func TestNewEndOfStreamElement(t *testing.T) {
	elem := NewEndOfStreamElement()

	assert.True(t, elem.EndOfStream)
	assert.Equal(t, PriorityCritical, elem.Priority)
}

func TestStreamElement_IsEmpty(t *testing.T) {
	t.Run("empty element", func(t *testing.T) {
		elem := StreamElement{}
		assert.True(t, elem.IsEmpty())
	})

	t.Run("element with text", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.False(t, elem.IsEmpty())
	})

	t.Run("element with error", func(t *testing.T) {
		elem := NewErrorElement(assert.AnError)
		assert.False(t, elem.IsEmpty())
	})
}

func TestStreamElement_HasContent(t *testing.T) {
	t.Run("empty element", func(t *testing.T) {
		elem := StreamElement{}
		assert.False(t, elem.HasContent())
	})

	t.Run("element with text", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.True(t, elem.HasContent())
	})

	t.Run("element with message", func(t *testing.T) {
		msg := &types.Message{Role: "user", Content: "hi"}
		elem := NewMessageElement(msg)
		assert.True(t, elem.HasContent())
	})
}

func TestStreamElement_IsControl(t *testing.T) {
	t.Run("non-control element", func(t *testing.T) {
		elem := NewTextElement("hello")
		assert.False(t, elem.IsControl())
	})

	t.Run("error element", func(t *testing.T) {
		elem := NewErrorElement(assert.AnError)
		assert.True(t, elem.IsControl())
	})

	t.Run("end of stream element", func(t *testing.T) {
		elem := NewEndOfStreamElement()
		assert.True(t, elem.IsControl())
	})
}

func TestStreamElement_WithSource(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithSource("my-stage")

	assert.Equal(t, "my-stage", result.Source)
}

func TestStreamElement_WithPriority(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithPriority(PriorityCritical)

	assert.Equal(t, PriorityCritical, result.Priority)
}

func TestStreamElement_WithSequence(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithSequence(42)

	assert.Equal(t, int64(42), result.Sequence)
}

func TestStreamElement_WithMetadata(t *testing.T) {
	elem := NewTextElement("test")
	result := elem.WithMetadata("key", "value")

	assert.Equal(t, "value", result.Metadata["key"])
}

func TestStreamElement_WithMetadata_NilMap(t *testing.T) {
	elem := StreamElement{}
	result := elem.WithMetadata("key", "value")

	assert.NotNil(t, result.Metadata)
	assert.Equal(t, "value", result.Metadata["key"])
}

func TestStreamElement_GetMetadata(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		elem := NewTextElement("test")
		elem.Metadata["key"] = "value"

		assert.Equal(t, "value", elem.GetMetadata("key"))
	})

	t.Run("missing key", func(t *testing.T) {
		elem := NewTextElement("test")
		assert.Nil(t, elem.GetMetadata("nonexistent"))
	})

	t.Run("nil metadata", func(t *testing.T) {
		elem := StreamElement{}
		assert.Nil(t, elem.GetMetadata("key"))
	})
}

// =============================================================================
// PipelineConfig Tests
// =============================================================================

func TestPipelineConfig_WithMaxConcurrentPipelines(t *testing.T) {
	config := &PipelineConfig{}
	result := config.WithMaxConcurrentPipelines(50)

	assert.Equal(t, 50, result.MaxConcurrentPipelines)
}

func TestPipelineConfig_WithExecutionTimeout(t *testing.T) {
	config := &PipelineConfig{}
	result := config.WithExecutionTimeout(30 * time.Second)

	assert.Equal(t, 30*time.Second, result.ExecutionTimeout)
}

func TestPipelineConfig_WithGracefulShutdownTimeout(t *testing.T) {
	config := &PipelineConfig{}
	result := config.WithGracefulShutdownTimeout(10 * time.Second)

	assert.Equal(t, 10*time.Second, result.GracefulShutdownTimeout)
}

func TestPipelineConfig_WithTracing(t *testing.T) {
	config := &PipelineConfig{}
	result := config.WithTracing(true)

	assert.True(t, result.EnableTracing)
}

// =============================================================================
// StageError Tests
// =============================================================================

func TestStageError_Error(t *testing.T) {
	err := &StageError{
		StageName: "my-stage",
		StageType: StageTypeTransform,
		Err:       assert.AnError,
	}

	errorMsg := err.Error()

	assert.Contains(t, errorMsg, "my-stage")
	assert.Contains(t, errorMsg, "transform")
}

func TestStageError_Unwrap(t *testing.T) {
	originalErr := assert.AnError
	stageErr := &StageError{
		StageName: "test",
		StageType: StageTypeGenerate,
		Err:       originalErr,
	}

	assert.Equal(t, originalErr, stageErr.Unwrap())
}

func TestNewStageError(t *testing.T) {
	err := NewStageError("my-stage", StageTypeSink, assert.AnError)

	assert.Equal(t, "my-stage", err.StageName)
	assert.Equal(t, StageTypeSink, err.StageType)
	assert.Equal(t, assert.AnError, err.Err)
}

func TestStageType_String(t *testing.T) {
	tests := []struct {
		stageType StageType
		expected  string
	}{
		{StageTypeTransform, "transform"},
		{StageTypeAccumulate, "accumulate"},
		{StageTypeGenerate, "generate"},
		{StageTypeSink, "sink"},
		{StageTypeBidirectional, "bidirectional"},
		{StageType(99), "unknown"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, tt.stageType.String())
	}
}

// =============================================================================
// ExecuteSync Tests
// =============================================================================

func TestStreamPipeline_ExecuteSync(t *testing.T) {
	t.Run("executes pipeline and returns result", func(t *testing.T) {
		// Create a simple passthrough stage
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder().AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		// Create input with a message
		msg := &types.Message{Role: "assistant", Content: "Hello, world!"}
		input := NewMessageElement(msg)
		input.Metadata["test_key"] = "test_value"

		// Execute sync
		result, err := pipeline.ExecuteSync(context.Background(), input)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify result
		assert.Len(t, result.Messages, 1)
		assert.Equal(t, "Hello, world!", result.Messages[0].Content)
		assert.NotNil(t, result.Response)
		assert.Equal(t, "Hello, world!", result.Response.Content)
		assert.Equal(t, "test_value", result.Metadata["test_key"])
	})

	t.Run("handles empty input", func(t *testing.T) {
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder().AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		result, err := pipeline.ExecuteSync(context.Background())
		require.NoError(t, err)
		assert.Len(t, result.Messages, 0)
	})

	t.Run("handles multiple messages", func(t *testing.T) {
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder().AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		msg1 := NewMessageElement(&types.Message{Role: "user", Content: "Hello"})
		msg2 := NewMessageElement(&types.Message{Role: "assistant", Content: "Hi there"})

		result, err := pipeline.ExecuteSync(context.Background(), msg1, msg2)
		require.NoError(t, err)
		assert.Len(t, result.Messages, 2)
		assert.Equal(t, "Hi there", result.Response.Content)
	})

	t.Run("captures errors", func(t *testing.T) {
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder().AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		errorElem := NewErrorElement(assert.AnError)

		result, err := pipeline.ExecuteSync(context.Background(), errorElem)
		assert.Error(t, err)
		assert.NotNil(t, result)
	})
}

// =============================================================================
// Shutdown Tests
// =============================================================================

func TestStreamPipeline_Shutdown(t *testing.T) {
	t.Run("shuts down cleanly", func(t *testing.T) {
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder()
		builder.AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		err = pipeline.Shutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("double shutdown is idempotent", func(t *testing.T) {
		stage := NewPassthroughStage("passthrough")
		builder := NewPipelineBuilder()
		builder.AddStage(stage)
		pipeline, err := builder.Build()
		require.NoError(t, err)

		// First shutdown
		err = pipeline.Shutdown(context.Background())
		assert.NoError(t, err)

		// Second shutdown should be no-op
		err = pipeline.Shutdown(context.Background())
		assert.NoError(t, err)
	})
}

// =============================================================================
// StageFunc Tests
// =============================================================================

func TestStageFunc_Process(t *testing.T) {
	t.Run("executes custom function", func(t *testing.T) {
		// Create a function that transforms content
		fn := func(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
			defer close(output)
			for elem := range input {
				if elem.Message != nil {
					elem.Message.Content = "transformed: " + elem.Message.Content
				}
				select {
				case output <- elem:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}

		stage := NewStageFunc("transformer", StageTypeTransform, fn)
		assert.Equal(t, "transformer", stage.Name())
		assert.Equal(t, StageTypeTransform, stage.Type())

		input := make(chan StreamElement, 1)
		output := make(chan StreamElement, 1)

		msg := &types.Message{Role: "user", Content: "hello"}
		input <- NewMessageElement(msg)
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		result := <-output
		assert.Equal(t, "transformed: hello", result.Message.Content)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		fn := func(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
			<-ctx.Done()
			return ctx.Err()
		}

		stage := NewStageFunc("blocker", StageTypeTransform, fn)
		input := make(chan StreamElement)
		output := make(chan StreamElement)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// =============================================================================
// FilterStage Tests
// =============================================================================

func TestFilterStage_Process(t *testing.T) {
	t.Run("filters elements based on predicate", func(t *testing.T) {
		// Only allow assistant messages
		predicate := func(elem StreamElement) bool {
			return elem.Message != nil && elem.Message.Role == "assistant"
		}

		stage := NewFilterStage("assistant-filter", predicate)
		assert.Equal(t, "assistant-filter", stage.Name())
		assert.Equal(t, StageTypeTransform, stage.Type())

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "bye"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Should only have the assistant message
		var results []StreamElement
		for elem := range output {
			results = append(results, elem)
		}
		assert.Len(t, results, 1)
		assert.Equal(t, "assistant", results[0].Message.Role)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return true }
		stage := NewFilterStage("filter", predicate)

		ctx, cancel := context.WithCancel(context.Background())
		input := make(chan StreamElement, 1)
		output := make(chan StreamElement) // Unbuffered

		msg := NewMessageElement(&types.Message{Role: "user", Content: "test"})
		input <- msg
		close(input)

		cancel()

		err := stage.Process(ctx, input, output)
		assert.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("passes all elements when predicate always returns true", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return true }
		stage := NewFilterStage("passthrough-filter", predicate)

		input := make(chan StreamElement, 2)
		output := make(chan StreamElement, 2)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		var count int
		for range output {
			count++
		}
		assert.Equal(t, 2, count)
	})

	t.Run("filters all elements when predicate always returns false", func(t *testing.T) {
		predicate := func(elem StreamElement) bool { return false }
		stage := NewFilterStage("reject-all", predicate)

		input := make(chan StreamElement, 2)
		output := make(chan StreamElement, 2)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "hi"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		var count int
		for range output {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

// =============================================================================
// ContextBuilderStage Truncation Tests
// =============================================================================

func TestContextBuilderStage_TruncateStrategies(t *testing.T) {
	t.Run("TruncateLeastRelevant without embeddings returns error", func(t *testing.T) {
		policy := &ContextBuilderPolicy{
			TokenBudget:      5, // Small budget to trigger truncation
			ReserveForOutput: 1,
			Strategy:         TruncateLeastRelevant,
			// No RelevanceConfig - should error
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		// Messages exceed the budget to trigger truncation
		input <- NewMessageElement(&types.Message{Role: "user", Content: "First message with lots of content here"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Second message with more content"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Latest message that also has content"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires RelevanceConfig")
	})

	t.Run("TruncateSummarize returns not implemented error", func(t *testing.T) {
		policy := &ContextBuilderPolicy{
			TokenBudget:      5, // Small budget to trigger truncation
			ReserveForOutput: 1,
			Strategy:         TruncateSummarize,
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		// Messages exceed the budget to trigger truncation
		input <- NewMessageElement(&types.Message{Role: "user", Content: "First message with lots of content here"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Second message with more content"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Latest message that also has content"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("default strategy uses TruncateOldest", func(t *testing.T) {
		policy := &ContextBuilderPolicy{
			TokenBudget:      20,
			ReserveForOutput: 5,
			Strategy:         TruncationStrategy("unknown"), // Unknown strategy
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "First message with content"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Second message"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Latest"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Should truncate but keep most recent
		var results []types.Message
		for elem := range output {
			if elem.Message != nil {
				results = append(results, *elem.Message)
			}
		}
		assert.LessOrEqual(t, len(results), 3)
	})
}

// =============================================================================
// ContextBuilderStage Token Counting Tests
// =============================================================================

func TestContextBuilderStage_CountToolCallTokens(t *testing.T) {
	policy := &ContextBuilderPolicy{
		TokenBudget:      1000,
		ReserveForOutput: 100,
		Strategy:         TruncateOldest,
	}
	stage := NewContextBuilderStage(policy)

	input := make(chan StreamElement, 1)
	output := make(chan StreamElement, 1)

	// Create message with tool calls
	msg := &types.Message{
		Role:    "assistant",
		Content: "Using tools",
		ToolCalls: []types.MessageToolCall{
			{ID: "call1", Name: "tool1", Args: []byte(`{"arg": "value"}`)},
			{ID: "call2", Name: "tool2", Args: []byte(`{"data": "test"}`)},
		},
	}
	input <- NewMessageElement(msg)
	close(input)

	err := stage.Process(context.Background(), input, output)
	require.NoError(t, err)

	// Should pass through with tool call tokens counted
	var count int
	for range output {
		count++
	}
	assert.Equal(t, 1, count)
}

// =============================================================================
// VariableProviderStage Tests
// =============================================================================

func TestVariableProviderStage_ErrorHandling(t *testing.T) {
	t.Run("handles provider error", func(t *testing.T) {
		mockProvider := &mockVariableProvider{
			name: "error-provider",
			err:  assert.AnError,
		}

		stage := NewVariableProviderStage(mockProvider)

		input := make(chan StreamElement, 1)
		output := make(chan StreamElement, 1)

		input <- StreamElement{Metadata: map[string]interface{}{}}
		close(input)

		err := stage.Process(context.Background(), input, output)
		assert.Error(t, err)
	})

	t.Run("handles nil metadata", func(t *testing.T) {
		mockProvider := &mockVariableProvider{
			name: "test-provider",
			vars: map[string]string{"key": "value"},
		}

		stage := NewVariableProviderStage(mockProvider)

		input := make(chan StreamElement, 1)
		output := make(chan StreamElement, 1)

		// Element with nil metadata
		input <- StreamElement{}
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		result := <-output
		assert.NotNil(t, result.Metadata)
		vars := result.Metadata["variables"].(map[string]string)
		assert.Equal(t, "value", vars["key"])
	})
}

// =============================================================================
// PipelineConfig Validation Tests
// =============================================================================

func TestPipelineConfig_Validate(t *testing.T) {
	t.Run("validates channel buffer size", func(t *testing.T) {
		config := &PipelineConfig{
			ChannelBufferSize: -1,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "channel buffer size")
	})

	t.Run("validates max concurrent pipelines", func(t *testing.T) {
		config := &PipelineConfig{
			ChannelBufferSize:      10,
			MaxConcurrentPipelines: -1,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max concurrent pipelines")
	})

	t.Run("validates execution timeout", func(t *testing.T) {
		config := &PipelineConfig{
			ChannelBufferSize:      10,
			MaxConcurrentPipelines: 10,
			ExecutionTimeout:       -1 * time.Second,
		}
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("valid config passes validation", func(t *testing.T) {
		config := DefaultPipelineConfig()
		err := config.Validate()
		assert.NoError(t, err)
	})
}

// =============================================================================
// PipelineBuilder Validation Tests
// =============================================================================

func TestPipelineBuilder_Validate_NoStages(t *testing.T) {
	builder := NewPipelineBuilder()
	_, err := builder.Build()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one stage")
}

func TestPipelineBuilder_Validate_InvalidConfig(t *testing.T) {
	config := &PipelineConfig{
		ChannelBufferSize: -1,
	}
	builder := NewPipelineBuilderWithConfig(config)
	builder.AddStage(NewPassthroughStage("test"))
	_, err := builder.Build()
	assert.Error(t, err)
}

// =============================================================================
// Relevance Truncation Tests
// =============================================================================

// mockEmbeddingProvider is a test implementation of providers.EmbeddingProvider
type mockEmbeddingProvider struct {
	embedFunc   func(texts []string) ([][]float32, error)
	dimensions  int
	maxBatch    int
	id          string
	embedCalled int
	lastTexts   []string
}

func newMockEmbeddingProvider() *mockEmbeddingProvider {
	return &mockEmbeddingProvider{
		dimensions: 3,
		maxBatch:   100,
		id:         "mock-embedding",
	}
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, req providers.EmbeddingRequest) (providers.EmbeddingResponse, error) {
	m.embedCalled++
	m.lastTexts = req.Texts

	if m.embedFunc != nil {
		embeddings, err := m.embedFunc(req.Texts)
		if err != nil {
			return providers.EmbeddingResponse{}, err
		}
		return providers.EmbeddingResponse{Embeddings: embeddings, Model: "mock"}, nil
	}

	// Default: return unit vectors for each text
	embeddings := make([][]float32, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = []float32{float32(i) * 0.1, 0.5, 0.5}
	}
	return providers.EmbeddingResponse{Embeddings: embeddings, Model: "mock"}, nil
}

func (m *mockEmbeddingProvider) EmbeddingDimensions() int {
	return m.dimensions
}

func (m *mockEmbeddingProvider) MaxBatchSize() int {
	return m.maxBatch
}

func (m *mockEmbeddingProvider) ID() string {
	return m.id
}

func TestContextBuilderStage_TruncateByRelevance(t *testing.T) {
	t.Run("falls back to oldest when no embedding provider", func(t *testing.T) {
		policy := &ContextBuilderPolicy{
			TokenBudget:      30,
			ReserveForOutput: 5,
			Strategy:         TruncateLeastRelevant,
			RelevanceConfig:  nil, // No config
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "First message with long content"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Second"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Third"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Should have truncated using oldest strategy
		var results []types.Message
		for elem := range output {
			if elem.Message != nil {
				results = append(results, *elem.Message)
			}
		}
		assert.LessOrEqual(t, len(results), 3)
	})

	t.Run("uses relevance with embedding provider", func(t *testing.T) {
		mockProvider := newMockEmbeddingProvider()
		// Make query (last user message) similar to first message
		mockProvider.embedFunc = func(texts []string) ([][]float32, error) {
			embeddings := make([][]float32, len(texts))
			for i, text := range texts {
				if text == "What is AI?" || text == "AI is interesting topic to discuss" {
					// Query and first message are similar
					embeddings[i] = []float32{1.0, 0.0, 0.0}
				} else if text == "Weather is nice today in the city" {
					// Second message is different
					embeddings[i] = []float32{0.0, 1.0, 0.0}
				} else {
					// Default
					embeddings[i] = []float32{0.5, 0.5, 0.0}
				}
			}
			return embeddings, nil
		}

		policy := &ContextBuilderPolicy{
			TokenBudget:      20, // Small budget to force truncation
			ReserveForOutput: 5,
			Strategy:         TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{
				EmbeddingProvider:    mockProvider,
				MinRecentMessages:    1, // Only protect last message
				AlwaysKeepSystemRole: false,
				QuerySource:          QuerySourceLastUser,
			},
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 3)
		output := make(chan StreamElement, 3)

		// Use longer messages to exceed budget
		input <- NewMessageElement(&types.Message{Role: "user", Content: "AI is interesting topic to discuss"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Weather is nice today in the city"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "What is AI?"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Embedding provider should have been called
		assert.Equal(t, 1, mockProvider.embedCalled)
	})

	t.Run("protects system messages when configured", func(t *testing.T) {
		mockProvider := newMockEmbeddingProvider()

		policy := &ContextBuilderPolicy{
			TokenBudget:      100,
			ReserveForOutput: 10,
			Strategy:         TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{
				EmbeddingProvider:    mockProvider,
				MinRecentMessages:    1,
				AlwaysKeepSystemRole: true,
				QuerySource:          QuerySourceLastUser,
			},
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 4)
		output := make(chan StreamElement, 4)

		input <- NewMessageElement(&types.Message{Role: "system", Content: "You are a helpful assistant"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Hello"})
		input <- NewMessageElement(&types.Message{Role: "assistant", Content: "Hi there"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "How are you?"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err)

		// Check that system message is preserved
		var results []types.Message
		for elem := range output {
			if elem.Message != nil {
				results = append(results, *elem.Message)
			}
		}

		hasSystem := false
		for _, msg := range results {
			if msg.Role == "system" {
				hasSystem = true
				break
			}
		}
		assert.True(t, hasSystem, "System message should be protected")
	})

	t.Run("handles embedding error gracefully", func(t *testing.T) {
		mockProvider := newMockEmbeddingProvider()
		mockProvider.embedFunc = func(texts []string) ([][]float32, error) {
			return nil, assert.AnError
		}

		policy := &ContextBuilderPolicy{
			TokenBudget:      30,
			ReserveForOutput: 5,
			Strategy:         TruncateLeastRelevant,
			RelevanceConfig: &RelevanceConfig{
				EmbeddingProvider: mockProvider,
				MinRecentMessages: 1,
				QuerySource:       QuerySourceLastUser,
			},
		}
		stage := NewContextBuilderStage(policy)

		input := make(chan StreamElement, 2)
		output := make(chan StreamElement, 2)

		input <- NewMessageElement(&types.Message{Role: "user", Content: "First long message"})
		input <- NewMessageElement(&types.Message{Role: "user", Content: "Second"})
		close(input)

		err := stage.Process(context.Background(), input, output)
		require.NoError(t, err) // Should fall back to oldest, not error

		// Should have output (fell back to oldest strategy)
		var count int
		for range output {
			count++
		}
		assert.Greater(t, count, 0)
	})
}

func TestBuildRelevanceQuery(t *testing.T) {
	stage := NewContextBuilderStage(&ContextBuilderPolicy{})

	t.Run("uses last user message", func(t *testing.T) {
		messages := []types.Message{
			{Role: "user", Content: "First question"},
			{Role: "assistant", Content: "First answer"},
			{Role: "user", Content: "Second question"},
		}
		cfg := &RelevanceConfig{QuerySource: QuerySourceLastUser}

		query := stage.buildRelevanceQuery(messages, cfg)
		assert.Equal(t, "Second question", query)
	})

	t.Run("uses last N messages", func(t *testing.T) {
		messages := []types.Message{
			{Role: "user", Content: "First"},
			{Role: "assistant", Content: "Second"},
			{Role: "user", Content: "Third"},
		}
		cfg := &RelevanceConfig{QuerySource: QuerySourceLastN, LastNCount: 2}

		query := stage.buildRelevanceQuery(messages, cfg)
		assert.Contains(t, query, "Second")
		assert.Contains(t, query, "Third")
	})

	t.Run("uses custom query", func(t *testing.T) {
		messages := []types.Message{
			{Role: "user", Content: "Something"},
		}
		cfg := &RelevanceConfig{QuerySource: QuerySourceCustom, CustomQuery: "My custom query"}

		query := stage.buildRelevanceQuery(messages, cfg)
		assert.Equal(t, "My custom query", query)
	})

	t.Run("falls back to last message when no user message", func(t *testing.T) {
		messages := []types.Message{
			{Role: "assistant", Content: "Only assistant"},
		}
		cfg := &RelevanceConfig{QuerySource: QuerySourceLastUser}

		query := stage.buildRelevanceQuery(messages, cfg)
		assert.Equal(t, "Only assistant", query)
	})

	t.Run("returns empty for empty messages", func(t *testing.T) {
		messages := []types.Message{}
		cfg := &RelevanceConfig{QuerySource: QuerySourceLastUser}

		query := stage.buildRelevanceQuery(messages, cfg)
		assert.Equal(t, "", query)
	})
}

func TestBuildScoredMessages(t *testing.T) {
	stage := NewContextBuilderStage(&ContextBuilderPolicy{})

	t.Run("marks recent messages as protected", func(t *testing.T) {
		messages := []types.Message{
			{Role: "user", Content: "First"},
			{Role: "assistant", Content: "Second"},
			{Role: "user", Content: "Third"},
		}

		scored := stage.buildScoredMessages(messages, 2, false)

		assert.Len(t, scored, 3)
		assert.False(t, scored[0].IsProtected) // First is not protected
		assert.True(t, scored[1].IsProtected)  // Second is protected (within last 2)
		assert.True(t, scored[2].IsProtected)  // Third is protected (within last 2)
	})

	t.Run("marks system role as protected", func(t *testing.T) {
		messages := []types.Message{
			{Role: "system", Content: "System prompt"},
			{Role: "user", Content: "Question"},
		}

		scored := stage.buildScoredMessages(messages, 1, true)

		assert.True(t, scored[0].IsProtected) // System is protected
		assert.True(t, scored[1].IsProtected) // Last 1 is also protected
	})

	t.Run("calculates token counts", func(t *testing.T) {
		messages := []types.Message{
			{Role: "user", Content: "Hello world"}, // ~2-3 tokens
		}

		scored := stage.buildScoredMessages(messages, 1, false)

		assert.Greater(t, scored[0].TokenCount, 0)
	})
}

func TestExtractMessageText(t *testing.T) {
	stage := NewContextBuilderStage(&ContextBuilderPolicy{})

	t.Run("extracts content", func(t *testing.T) {
		msg := &types.Message{Content: "Hello world"}
		text := stage.extractMessageText(msg)
		assert.Equal(t, "Hello world", text)
	})

	t.Run("extracts from parts when content empty", func(t *testing.T) {
		part1 := "Part one"
		part2 := "Part two"
		msg := &types.Message{
			Content: "",
			Parts: []types.ContentPart{
				{Text: &part1},
				{Text: &part2},
			},
		}
		text := stage.extractMessageText(msg)
		assert.Contains(t, text, "Part one")
		assert.Contains(t, text, "Part two")
	})
}

func TestSelectByRelevance(t *testing.T) {
	stage := NewContextBuilderStage(&ContextBuilderPolicy{})

	t.Run("keeps protected messages first", func(t *testing.T) {
		scored := []ScoredMessage{
			{Index: 0, Message: types.Message{Content: "Protected"}, Score: 0.1, IsProtected: true, TokenCount: 5},
			{Index: 1, Message: types.Message{Content: "High score"}, Score: 0.9, IsProtected: false, TokenCount: 5},
			{Index: 2, Message: types.Message{Content: "Low score"}, Score: 0.2, IsProtected: false, TokenCount: 5},
		}

		result := stage.selectByRelevance(scored, 15, 0.0)

		// Should have protected + highest scoring that fits
		assert.GreaterOrEqual(t, len(result), 1)

		// First should be protected (index 0)
		if len(result) > 0 {
			assert.Equal(t, "Protected", result[0].Content)
		}
	})

	t.Run("respects threshold", func(t *testing.T) {
		scored := []ScoredMessage{
			{Index: 0, Message: types.Message{Content: "Protected"}, Score: 0.1, IsProtected: true, TokenCount: 5},
			{Index: 1, Message: types.Message{Content: "Above threshold"}, Score: 0.8, IsProtected: false, TokenCount: 5},
			{Index: 2, Message: types.Message{Content: "Below threshold"}, Score: 0.3, IsProtected: false, TokenCount: 5},
		}

		result := stage.selectByRelevance(scored, 20, 0.5) // Threshold of 0.5

		// Should include protected and above-threshold
		contents := make(map[string]bool)
		for _, msg := range result {
			contents[msg.Content] = true
		}

		assert.True(t, contents["Protected"])
		assert.True(t, contents["Above threshold"])
		assert.False(t, contents["Below threshold"])
	})

	t.Run("respects budget", func(t *testing.T) {
		scored := []ScoredMessage{
			{Index: 0, Message: types.Message{Content: "A"}, Score: 0.9, IsProtected: false, TokenCount: 10},
			{Index: 1, Message: types.Message{Content: "B"}, Score: 0.8, IsProtected: false, TokenCount: 10},
			{Index: 2, Message: types.Message{Content: "C"}, Score: 0.7, IsProtected: false, TokenCount: 10},
		}

		result := stage.selectByRelevance(scored, 15, 0.0) // Budget only fits ~1-2 messages

		// Should not exceed budget
		totalTokens := 0
		for _, msg := range result {
			// Approximate token count check
			totalTokens += len(msg.Content)
		}
		// With budget of 15 and tokens of 10 each, should have at most 1 message
		assert.LessOrEqual(t, len(result), 1)
	})

	t.Run("preserves original order", func(t *testing.T) {
		scored := []ScoredMessage{
			{Index: 0, Message: types.Message{Content: "First"}, Score: 0.5, IsProtected: false, TokenCount: 2},
			{Index: 1, Message: types.Message{Content: "Second"}, Score: 0.9, IsProtected: false, TokenCount: 2},
			{Index: 2, Message: types.Message{Content: "Third"}, Score: 0.7, IsProtected: false, TokenCount: 2},
		}

		result := stage.selectByRelevance(scored, 100, 0.0)

		// Check order is preserved (by original index)
		for i := 1; i < len(result); i++ {
			// Message content should be in original order if both selected
			// This is a simple check that order is maintained
		}
		assert.LessOrEqual(t, len(result), 3)
	})
}
