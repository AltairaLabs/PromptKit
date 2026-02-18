package statestore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSummarizerProvider implements providers.Provider for testing LLMSummarizer.
type mockSummarizerProvider struct {
	predictFn func(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error)
}

func (m *mockSummarizerProvider) ID() string    { return "mock-summarizer" }
func (m *mockSummarizerProvider) Model() string { return "mock-model" }

func (m *mockSummarizerProvider) Predict(
	ctx context.Context,
	req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	if m.predictFn != nil {
		return m.predictFn(ctx, req)
	}
	return providers.PredictionResponse{}, nil
}

func (m *mockSummarizerProvider) PredictStream(
	_ context.Context,
	_ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

func (m *mockSummarizerProvider) SupportsStreaming() bool      { return false }
func (m *mockSummarizerProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockSummarizerProvider) Close() error                 { return nil }

func (m *mockSummarizerProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func TestLLMSummarizer_Summarize(t *testing.T) {
	called := false
	mock := &mockSummarizerProvider{
		predictFn: func(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
			called = true
			return providers.PredictionResponse{
				Content: "The user asked about Go testing and the assistant explained table-driven tests.",
			}, nil
		},
	}

	summarizer := NewLLMSummarizer(mock)
	messages := []types.Message{
		{Role: "user", Content: "How do I write tests in Go?"},
		{Role: "assistant", Content: "You can use table-driven tests with the testing package."},
	}

	result, err := summarizer.Summarize(context.Background(), messages)

	require.NoError(t, err)
	assert.True(t, called, "Predict should have been called")
	assert.Equal(t, "The user asked about Go testing and the assistant explained table-driven tests.", result)
}

func TestLLMSummarizer_SummarizeEmpty(t *testing.T) {
	called := false
	mock := &mockSummarizerProvider{
		predictFn: func(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
			called = true
			return providers.PredictionResponse{}, nil
		},
	}

	summarizer := NewLLMSummarizer(mock)

	result, err := summarizer.Summarize(context.Background(), []types.Message{})

	require.NoError(t, err)
	assert.Empty(t, result)
	assert.False(t, called, "Predict should not be called for empty messages")
}

func TestLLMSummarizer_SummarizeError(t *testing.T) {
	expectedErr := errors.New("provider unavailable")
	mock := &mockSummarizerProvider{
		predictFn: func(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
			return providers.PredictionResponse{}, expectedErr
		},
	}

	summarizer := NewLLMSummarizer(mock)
	messages := []types.Message{
		{Role: "user", Content: "Hello"},
	}

	result, err := summarizer.Summarize(context.Background(), messages)

	require.Error(t, err)
	assert.Empty(t, result)
	assert.ErrorIs(t, err, expectedErr)
	assert.Contains(t, err.Error(), "summarizer: prediction failed")
}

func TestLLMSummarizer_SummarizeFormatsMessages(t *testing.T) {
	var capturedReq providers.PredictionRequest
	mock := &mockSummarizerProvider{
		predictFn: func(_ context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
			capturedReq = req
			return providers.PredictionResponse{Content: "summary"}, nil
		},
	}

	summarizer := NewLLMSummarizer(mock)
	messages := []types.Message{
		{Role: "user", Content: "What is PromptKit?"},
		{Role: "assistant", Content: "PromptKit is a toolkit for building LLM applications."},
		{Role: "user", Content: "How do I install it?"},
	}

	result, err := summarizer.Summarize(context.Background(), messages)

	require.NoError(t, err)
	assert.Equal(t, "summary", result)

	// Verify system prompt is set
	assert.NotEmpty(t, capturedReq.System)
	assert.Contains(t, capturedReq.System, "summarizer")

	// Verify the request contains a single user message with all formatted messages
	require.Len(t, capturedReq.Messages, 1)
	assert.Equal(t, "user", capturedReq.Messages[0].Role)

	content := capturedReq.Messages[0].Content
	assert.Contains(t, content, "[user]: What is PromptKit?")
	assert.Contains(t, content, "[assistant]: PromptKit is a toolkit for building LLM applications.")
	assert.Contains(t, content, "[user]: How do I install it?")

	// Verify all three messages appear in order
	userIdx := strings.Index(content, "[user]: What is PromptKit?")
	assistantIdx := strings.Index(content, "[assistant]: PromptKit is a toolkit")
	user2Idx := strings.Index(content, "[user]: How do I install it?")
	assert.True(t, userIdx < assistantIdx, "first user message should appear before assistant message")
	assert.True(t, assistantIdx < user2Idx, "assistant message should appear before second user message")

	// Verify prediction parameters
	assert.Equal(t, 500, capturedReq.MaxTokens)
	assert.InDelta(t, 0.3, float64(capturedReq.Temperature), 0.01)
}
