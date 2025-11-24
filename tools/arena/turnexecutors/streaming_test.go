package turnexecutors

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper for test mocks
func strPtr(s string) *string {
	return &s
}

// TestScriptedExecutor_ExecuteTurnStream_Success tests basic streaming functionality
func TestScriptedExecutor_ExecuteTurnStream_Success(t *testing.T) {
	// Create mock provider with streaming support
	mockProvider := &MockStreamingProvider{
		streamChunks: []providers.StreamChunk{
			{Content: "Hello", Delta: "Hello", TokenCount: 1},
			{Content: "Hello world", Delta: " world", TokenCount: 2},
			{Content: "Hello world!", Delta: "!", TokenCount: 3, FinishReason: strPtr("stop")},
		},
	}

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(aiExecutor)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Test message",
	}

	stream, err := executor.ExecuteTurnStream(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Collect all chunks
	chunks := []MessageStreamChunk{}
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		chunks = append(chunks, chunk)
	}

	// Verify we got chunks
	require.Greater(t, len(chunks), 0)

	// Check final chunk has complete messages
	final := chunks[len(chunks)-1]
	require.NotNil(t, final.Messages)
	require.Len(t, final.Messages, 2) // user + assistant
	assert.Equal(t, "user", final.Messages[0].Role)
	assert.Equal(t, "Test message", final.Messages[0].Content)
	assert.Equal(t, "assistant", final.Messages[1].Role)
	assert.Equal(t, "Hello world!", final.Messages[1].Content)

	// Verify deltas are present in intermediate chunks
	assert.Equal(t, "Hello", chunks[0].Delta)
	assert.Equal(t, " world", chunks[1].Delta)
	assert.Equal(t, "!", chunks[2].Delta)
}

// TestScriptedExecutor_ExecuteTurnStream_ProviderError tests error handling
func TestScriptedExecutor_ExecuteTurnStream_ProviderError(t *testing.T) {
	mockProvider := &MockStreamingProvider{
		streamError: assert.AnError,
	}

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(aiExecutor)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Test message",
	}

	stream, err := executor.ExecuteTurnStream(context.Background(), req)
	require.NoError(t, err) // Stream creation should not error
	require.NotNil(t, stream)

	// Should receive error in stream
	chunk := <-stream
	require.Error(t, chunk.Error)
	assert.Equal(t, assert.AnError, chunk.Error)

	// Stream should be closed
	_, ok := <-stream
	assert.False(t, ok, "Stream should be closed after error")
}

// TestScriptedExecutor_ExecuteTurnStream_ContextCancellation tests cancellation
func TestScriptedExecutor_ExecuteTurnStream_ContextCancellation(t *testing.T) {
	mockProvider := &MockStreamingProvider{
		streamChunks: []providers.StreamChunk{
			{Content: "Hello", Delta: "Hello", TokenCount: 1},
			{Content: "Hello world", Delta: " world", TokenCount: 2},
			{Content: "Hello world!", Delta: "!", TokenCount: 3, FinishReason: strPtr("stop")},
		},
	}

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(aiExecutor)

	ctx, cancel := context.WithCancel(context.Background())

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Test message",
	}

	stream, err := executor.ExecuteTurnStream(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Read first chunk
	chunk := <-stream
	require.NoError(t, chunk.Error)

	// Cancel context
	cancel()

	// Remaining chunks should stop
	for chunk := range stream {
		if chunk.Error != nil {
			assert.ErrorIs(t, chunk.Error, context.Canceled)
			break
		}
	}
}

// TestSupportsStreaming verifies executor checks provider streaming support
func TestSupportsStreaming(t *testing.T) {
	tests := []struct {
		name              string
		provider          providers.Provider
		supportsStreaming bool
	}{
		{
			name: "streaming provider",
			provider: &MockStreamingProvider{
				streamChunks: []providers.StreamChunk{},
			},
			supportsStreaming: true,
		},
		{
			name:              "non-streaming provider",
			provider:          &MockNonStreamingProvider{},
			supportsStreaming: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.provider.SupportsStreaming()
			assert.Equal(t, tt.supportsStreaming, result)
		})
	}
}

// MockStreamingProvider is a mock provider that supports streaming
type MockStreamingProvider struct {
	streamChunks []providers.StreamChunk
	streamError  error
}

func (m *MockStreamingProvider) ID() string {
	return "mock-streaming"
}

func (m *MockStreamingProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{
		Content: "mock response",
	}, nil
}

func (m *MockStreamingProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	if m.streamError != nil {
		ch := make(chan providers.StreamChunk, 1)
		ch <- providers.StreamChunk{Error: m.streamError}
		close(ch)
		return ch, nil
	}

	ch := make(chan providers.StreamChunk, len(m.streamChunks))
	for _, chunk := range m.streamChunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (m *MockStreamingProvider) SupportsStreaming() bool {
	return true
}

func (m *MockStreamingProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockStreamingProvider) Close() error {
	return nil
}

func (m *MockStreamingProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.0001,
		OutputCostUSD: float64(outputTokens) * 0.0001,
		TotalCost:     float64(inputTokens+outputTokens) * 0.0001,
	}
}

// MockNonStreamingProvider is a mock provider that doesn't support streaming
type MockNonStreamingProvider struct{}

func (m *MockNonStreamingProvider) ID() string {
	return "mock-non-streaming"
}

func (m *MockNonStreamingProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{
		Content: "mock response",
	}, nil
}

func (m *MockNonStreamingProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *MockNonStreamingProvider) SupportsStreaming() bool {
	return false
}

func (m *MockNonStreamingProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *MockNonStreamingProvider) Close() error {
	return nil
}

func (m *MockNonStreamingProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.0001,
		OutputCostUSD: float64(outputTokens) * 0.0001,
		TotalCost:     float64(inputTokens+outputTokens) * 0.0001,
	}
}

// TestSelfPlayExecutor_ExecuteTurnStream_Success tests self-play streaming
func TestSelfPlayExecutor_ExecuteTurnStream_Success(t *testing.T) {
	// Mock streaming provider
	mockProvider := &MockStreamingProvider{
		streamChunks: []providers.StreamChunk{
			{Content: "AI", Delta: "AI", TokenCount: 1},
			{Content: "AI response", Delta: " response", TokenCount: 2, FinishReason: strPtr("stop")},
		},
	}

	// Mock content generator
	mockContentGen := &MockContentGenerator{
		message: types.Message{
			Role:    "user",
			Content: "Self-play generated message",
		},
	}

	mockSelfPlayProvider := &MockSelfPlayProvider{
		contentGen: mockContentGen,
	}

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewSelfPlayExecutor(aiExecutor, mockSelfPlayProvider)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		SelfPlayRole:    "user",
		SelfPlayPersona: "helpful",
	}

	stream, err := executor.ExecuteTurnStream(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Collect all chunks
	chunks := []MessageStreamChunk{}
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		chunks = append(chunks, chunk)
	}

	// Verify we got chunks
	require.Greater(t, len(chunks), 0)

	// Check final chunk
	final := chunks[len(chunks)-1]
	require.NotNil(t, final.Messages)
	require.Len(t, final.Messages, 2) // user + assistant
	assert.Equal(t, "user", final.Messages[0].Role)
	assert.Equal(t, "Self-play generated message", final.Messages[0].Content)
	assert.Equal(t, "assistant", final.Messages[1].Role)
	assert.Equal(t, "AI response", final.Messages[1].Content)
}

// MockContentGenerator implements selfplay.Generator for testing
type MockContentGenerator struct {
	message types.Message
}

func (m *MockContentGenerator) NextUserTurn(ctx context.Context, history []types.Message) (*pipeline.ExecutionResult, error) {
	return &pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Content: m.message.Content,
			Role:    m.message.Role,
		},
		CostInfo: types.CostInfo{}, // Empty cost info for mock
		Metadata: map[string]interface{}{
			"persona": "mock-persona",
			"role":    "self-play-user",
		},
	}, nil
}

// MockSelfPlayProvider implements selfplay.Provider for testing
type MockSelfPlayProvider struct {
	contentGen *MockContentGenerator
}

func (m *MockSelfPlayProvider) GetContentGenerator(role, persona string) (selfplay.Generator, error) {
	return m.contentGen, nil
}
