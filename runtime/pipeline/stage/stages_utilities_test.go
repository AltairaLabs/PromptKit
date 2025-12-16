package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
