package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteConversationStream_SingleTurn tests basic streaming functionality
func TestExecuteConversationStream_SingleTurn(t *testing.T) {
	// Create mock streaming turn executor
	mockExecutor := &MockStreamingTurnExecutor{
		chunks: []mockChunk{
			{Delta: "Hello", TokenCount: 1},
			{Delta: " world", TokenCount: 2},
			{Delta: "!", TokenCount: 3, FinishReason: strPtr("stop")},
		},
	}

	executor := NewDefaultConversationExecutor(
		mockExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Test message"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockStreamingProvider{},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-stream-single",
	}

	stream, err := executor.ExecuteConversationStream(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Collect all chunks
	chunks := []ConversationStreamChunk{}
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		chunks = append(chunks, chunk)
	}

	// Verify we got chunks
	require.Greater(t, len(chunks), 0)

	// Check we got deltas
	var totalContent string
	for _, chunk := range chunks {
		totalContent += chunk.Delta
	}
	assert.Equal(t, "Hello world!", totalContent)

	// Final chunk should have complete conversation result
	final := chunks[len(chunks)-1]
	assert.NotNil(t, final.Result)
	assert.Greater(t, len(final.Result.Messages), 0)
}

// TestExecuteConversationStream_MultipleTurns tests multi-turn streaming
func TestExecuteConversationStream_MultipleTurns(t *testing.T) {
	turnCount := 0
	mockExecutor := &MockStreamingTurnExecutor{
		generateChunks: func() []mockChunk {
			turnCount++
			return []mockChunk{
				{Delta: "Response", TokenCount: 1},
				{Delta: " ", TokenCount: 2},
				{Delta: string(rune('0' + turnCount)), TokenCount: 3, FinishReason: strPtr("stop")},
			}
		},
	}

	executor := NewDefaultConversationExecutor(
		mockExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "First message"},
			{Role: "user", Content: "Second message"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockStreamingProvider{},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
		StateStoreConfig: &StateStoreConfig{
			Store:  createTestStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conv-stream-multi",
	}

	stream, err := executor.ExecuteConversationStream(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Collect all chunks
	chunks := []ConversationStreamChunk{}
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		chunks = append(chunks, chunk)
	}

	// Should have chunks from both turns
	require.Greater(t, len(chunks), 6) // At least 3 chunks per turn

	// Final result should have 2 assistant messages + 2 user messages + system message = 5 total
	final := chunks[len(chunks)-1]
	require.NotNil(t, final.Result)
	assert.GreaterOrEqual(t, len(final.Result.Messages), 3) // At least system + user + assistant
}

// TestExecuteConversationStream_Error tests error handling
func TestExecuteConversationStream_Error(t *testing.T) {
	mockExecutor := &MockStreamingTurnExecutor{
		shouldError: true,
	}

	executor := NewDefaultConversationExecutor(
		mockExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Test message"},
		},
	}

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockStreamingProvider{},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
	}

	stream, err := executor.ExecuteConversationStream(context.Background(), req)
	require.NoError(t, err) // Stream creation should not error
	require.NotNil(t, stream)

	// Should receive error in stream
	chunk := <-stream
	require.Error(t, chunk.Error)
	// Don't check exact error message since it's a test assertion error

	// Stream should be closed
	_, ok := <-stream
	assert.False(t, ok, "Stream should be closed after error")
}

// TestExecuteConversationStream_ContextCancellation tests cancellation
func TestExecuteConversationStream_ContextCancellation(t *testing.T) {
	mockExecutor := &MockStreamingTurnExecutor{
		chunks: []mockChunk{
			{Delta: "Hello", TokenCount: 1},
			{Delta: " world", TokenCount: 2},
			{Delta: "!", TokenCount: 3, FinishReason: strPtr("stop")},
		},
	}

	executor := NewDefaultConversationExecutor(
		mockExecutor,
		nil,
		nil,
		createTestPromptRegistry(t),
	)

	scenario := &config.Scenario{
		ID:       "test",
		TaskType: "support",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Test message"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := ConversationRequest{
		Region:   "us",
		Scenario: scenario,
		Provider: &MockStreamingProvider{},
		Config: &config.Config{
			Defaults: config.Defaults{
				Verbose: false,
			},
		},
	}

	stream, err := executor.ExecuteConversationStream(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Read first chunk
	chunk := <-stream
	require.NoError(t, chunk.Error)

	// Cancel context
	cancel()

	// Remaining chunks should stop or error
	for chunk := range stream {
		if chunk.Error != nil {
			assert.ErrorIs(t, chunk.Error, context.Canceled)
			break
		}
	}
}

// MockStreamingTurnExecutor implements turnexecutors.TurnExecutor for streaming tests
type MockStreamingTurnExecutor struct {
	chunks         []mockChunk
	generateChunks func() []mockChunk
	shouldError    bool
	callCount      int
}

// mockChunk is a simple test structure for generating stream chunks
type mockChunk struct {
	Delta        string
	TokenCount   int
	FinishReason *string
}

func (m *MockStreamingTurnExecutor) ExecuteTurn(ctx context.Context, req turnexecutors.TurnRequest) error {
	// For streaming tests, we use ExecuteTurnStream
	// But if called directly, save mock messages to StateStore
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
		messages := []types.Message{
			{Role: "user", Content: req.ScriptedContent},
			{Role: "assistant", Content: "mock response"},
		}

		store, ok := req.StateStoreConfig.Store.(statestore.Store)
		if ok {
			state, loadErr := store.Load(ctx, req.ConversationID)
			if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
				return loadErr
			}
			if state == nil {
				state = &statestore.ConversationState{
					ID:       req.ConversationID,
					UserID:   req.StateStoreConfig.UserID,
					Messages: []types.Message{},
				}
			}
			state.Messages = append(state.Messages, messages...)
			if saveErr := store.Save(ctx, state); saveErr != nil {
				return saveErr
			}
		}
	}
	return nil
}

func (m *MockStreamingTurnExecutor) ExecuteTurnStream(ctx context.Context, req turnexecutors.TurnRequest) (<-chan turnexecutors.MessageStreamChunk, error) {
	m.callCount++
	ch := make(chan turnexecutors.MessageStreamChunk)

	go func() {
		defer close(ch)

		if m.shouldError {
			ch <- turnexecutors.MessageStreamChunk{Error: assert.AnError}
			return
		}

		chunks := m.chunks
		if m.generateChunks != nil {
			chunks = m.generateChunks()
		}

		// Accumulate messages as we stream
		messages := []types.Message{
			{
				Role:    "user",
				Content: req.ScriptedContent,
			},
			{
				Role:    "assistant",
				Content: "",
			},
		}

		accumulated := ""
		for i, chunk := range chunks {
			accumulated += chunk.Delta
			messages[1].Content = accumulated

			// Create new chunk with current message state
			newChunk := turnexecutors.MessageStreamChunk{
				Messages:     messages,
				Delta:        chunk.Delta,
				MessageIndex: 1, // assistant message index
				TokenCount:   chunk.TokenCount,
			}
			if i == len(chunks)-1 {
				newChunk.FinishReason = chunk.FinishReason
			}

			select {
			case <-ctx.Done():
				ch <- turnexecutors.MessageStreamChunk{Messages: messages, Error: ctx.Err()}
				return
			case ch <- newChunk:
			}
		}

		// Save messages to StateStore if configured
		if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
			store, ok := req.StateStoreConfig.Store.(statestore.Store)
			if ok {
				// Load existing conversation
				state, loadErr := store.Load(ctx, req.ConversationID)
				if loadErr != nil && !errors.Is(loadErr, statestore.ErrNotFound) {
					ch <- turnexecutors.MessageStreamChunk{Messages: messages, Error: loadErr}
					return
				}
				if state == nil {
					state = &statestore.ConversationState{
						ID:       req.ConversationID,
						UserID:   req.StateStoreConfig.UserID,
						Messages: []types.Message{},
					}
				}

				// Append new messages
				state.Messages = append(state.Messages, messages...)

				// Save back
				if saveErr := store.Save(ctx, state); saveErr != nil {
					ch <- turnexecutors.MessageStreamChunk{Messages: messages, Error: saveErr}
					return
				}
			}
		}
	}()

	return ch, nil
}

// MockStreamingProvider implements providers.Provider for tests
type MockStreamingProvider struct{}

func (m *MockStreamingProvider) ID() string {
	return "mock-streaming"
}

func (m *MockStreamingProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	return providers.ChatResponse{}, nil
}

func (m *MockStreamingProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
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
	inputCostPer1K := 0.01
	outputCostPer1K := 0.01
	cachedCostPer1K := 0.005

	inputCost := float64(inputTokens-cachedTokens) / 1000.0 * inputCostPer1K
	cachedCost := float64(cachedTokens) / 1000.0 * cachedCostPer1K
	outputCost := float64(outputTokens) / 1000.0 * outputCostPer1K

	return types.CostInfo{
		InputTokens:   inputTokens - cachedTokens,
		OutputTokens:  outputTokens,
		CachedTokens:  cachedTokens,
		InputCostUSD:  inputCost,
		OutputCostUSD: outputCost,
		CachedCostUSD: cachedCost,
		TotalCost:     inputCost + cachedCost + outputCost,
	}
}

func strPtr(s string) *string {
	return &s
}
