package turnexecutors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockProvider implements providers.Provider for testing
type MockProvider struct {
	mock.Mock
}

func (m *MockProvider) ID() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(providers.ChatResponse), args.Error(1)
}

func (m *MockProvider) ShouldIncludeRawOutput() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockProvider) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan providers.StreamChunk), args.Error(1)
}

func (m *MockProvider) SupportsStreaming() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	args := m.Called(inputTokens, outputTokens, cachedTokens)
	return args.Get(0).(types.CostInfo)
}

func TestNewScriptedExecutor(t *testing.T) {
	aiExecutor := NewPipelineExecutor(tools.NewRegistry())
	executor := NewScriptedExecutor(aiExecutor)

	assert.NotNil(t, executor)
	assert.Equal(t, aiExecutor, executor.pipelineExecutor)
}

func TestScriptedExecutor_ExecuteTurn_Success(t *testing.T) {
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock successful AI response
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.ChatResponse{
		Content:  "I can help you with that.",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry)
	executor := NewScriptedExecutor(aiExecutor)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil, // Tests use nil, will get default prompt
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Hello, I need help.",
		StateStoreConfig: &StateStoreConfig{
			Store:  statestore.NewArenaStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conversation",
	}
	// Ensure validator configs present so DynamicValidatorMiddleware continues the chain
	if req.StateStoreConfig != nil {
		req.StateStoreConfig.Metadata = map[string]interface{}{
			"validator_configs": []validators.ValidatorConfig{{Type: "length", Params: map[string]interface{}{"length": 100}}},
		}
	}

	// Pre-populate the configured state store with an initial conversation so
	// ExecuteTurn can load/update it during execution. The default arena
	// state store expects a ConversationState to exist for the given ID.
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeIface := req.StateStoreConfig.Store
		store, ok := storeIface.(runtimestore.Store)
		require.True(t, ok, "state store must implement runtimestore.Store")

		initState := &runtimestore.ConversationState{
			ID:       req.ConversationID,
			UserID:   req.StateStoreConfig.UserID,
			Messages: []types.Message{},
			Metadata: map[string]interface{}{},
		}
		saveErr := store.Save(context.Background(), initState)
		require.NoError(t, saveErr)
	}

	err := executor.ExecuteTurn(context.Background(), req)

	require.NoError(t, err)

	// Load messages from StateStore
	store := req.StateStoreConfig.Store.(runtimestore.Store)
	state, loadErr := store.Load(context.Background(), req.ConversationID)
	require.NoError(t, loadErr)
	require.NotNil(t, state)
	require.Len(t, state.Messages, 2) // user + assistant

	assert.Equal(t, "user", state.Messages[0].Role)
	assert.Equal(t, "Hello, I need help.", state.Messages[0].Content)
	assert.Equal(t, "assistant", state.Messages[1].Role)
	assert.Equal(t, "I can help you with that.", state.Messages[1].Content)
	mockProvider.AssertExpectations(t)
}

func TestScriptedExecutor_ExecuteTurn_AIResponseError(t *testing.T) {
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock AI response error
	expectedErr := errors.New("API rate limit exceeded")
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(providers.ChatResponse{}, expectedErr)

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry)
	executor := NewScriptedExecutor(aiExecutor)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Hello!",
	}

	err := executor.ExecuteTurn(context.Background(), req)

	// Should return error
	require.Error(t, err)
	mockProvider.AssertExpectations(t)
}

func TestScriptedExecutor_ExecuteTurn_EmptyScriptedContent(t *testing.T) {
	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry)
	executor := NewScriptedExecutor(aiExecutor)

	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "", // Empty content
		StateStoreConfig: &StateStoreConfig{
			Store:  statestore.NewArenaStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conversation-2",
	}
	if req.StateStoreConfig != nil {
		req.StateStoreConfig.Metadata = map[string]interface{}{
			"validator_configs": []validators.ValidatorConfig{{Type: "length", Params: map[string]interface{}{"length": 100}}},
		}
	}

	// Ensure conversation exists in state store before executing the turn.
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeIface := req.StateStoreConfig.Store
		store, ok := storeIface.(runtimestore.Store)
		require.True(t, ok, "state store must implement runtimestore.Store")

		initState := &runtimestore.ConversationState{
			ID:       req.ConversationID,
			UserID:   req.StateStoreConfig.UserID,
			Messages: []types.Message{},
			Metadata: map[string]interface{}{},
		}
		saveErr := store.Save(context.Background(), initState)
		require.NoError(t, saveErr)
	}

	// Mock AI response - should still work with empty user message
	costBreakdown := types.CostInfo{
		InputTokens:   8,
		OutputTokens:  7,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.ChatResponse{
		Content:  "Hello! How can I help you?",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(response, nil)

	err := executor.ExecuteTurn(context.Background(), req)

	require.NoError(t, err)

	// Load and verify messages from StateStore
	store := req.StateStoreConfig.Store.(runtimestore.Store)
	state, loadErr := store.Load(context.Background(), req.ConversationID)
	require.NoError(t, loadErr)
	require.NotNil(t, state)
	require.Len(t, state.Messages, 2)              // user + assistant
	assert.Equal(t, "", state.Messages[0].Content) // Empty scripted content
	mockProvider.AssertExpectations(t)
}

func TestScriptedExecutor_ExecuteTurn_WithHistory(t *testing.T) {
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	response := providers.ChatResponse{
		Content: "Based on our previous conversation, I can help.",
		CostInfo: &types.CostInfo{
			InputTokens:   20,
			OutputTokens:  10,
			InputCostUSD:  0.001,
			OutputCostUSD: 0.001,
			TotalCost:     0.002,
		},
	}
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry)
	executor := NewScriptedExecutor(aiExecutor)

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "Follow-up question",
		StateStoreConfig: &StateStoreConfig{
			Store:  statestore.NewArenaStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conversation-3",
	}
	if req.StateStoreConfig != nil {
		req.StateStoreConfig.Metadata = map[string]interface{}{
			"validator_configs": []validators.ValidatorConfig{{Type: "length", Params: map[string]interface{}{"length": 100}}},
		}
	}

	// Pre-populate the state store with an existing conversation so history
	// is available to the executor.
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeIface := req.StateStoreConfig.Store
		store, ok := storeIface.(runtimestore.Store)
		require.True(t, ok, "state store must implement runtimestore.Store")

		// Add a prior message to simulate history
		initState := &runtimestore.ConversationState{
			ID:     req.ConversationID,
			UserID: req.StateStoreConfig.UserID,
			Messages: []types.Message{
				{Role: "user", Content: "Previous question", Timestamp: time.Now()},
			},
			Metadata: map[string]interface{}{},
		}
		saveErr := store.Save(context.Background(), initState)
		require.NoError(t, saveErr)
	}

	err := executor.ExecuteTurn(context.Background(), req)

	require.NoError(t, err)

	// Load and verify messages from StateStore
	store := req.StateStoreConfig.Store.(runtimestore.Store)
	state, loadErr := store.Load(context.Background(), req.ConversationID)
	require.NoError(t, loadErr)
	require.NotNil(t, state)
	// With a pre-existing history we expect the previous messages to remain and
	// the new user+assistant turn to be appended. Verify at least two messages
	// were added and the final user message matches the scripted content.
	require.GreaterOrEqual(t, len(state.Messages), 2)
	// New user message should be the second-to-last entry
	userIdx := len(state.Messages) - 2
	assistantIdx := len(state.Messages) - 1
	assert.Equal(t, "Follow-up question", state.Messages[userIdx].Content)
	assert.Equal(t, "Based on our previous conversation, I can help.", state.Messages[assistantIdx].Content)

	// Verify Chat was called (history is handled by StateStore middleware, not passed directly)
	mockProvider.AssertCalled(t, "Chat", mock.Anything, mock.Anything)
}

func TestScriptedExecutor_ExecuteTurn_SetsTimestamp(t *testing.T) {
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()
	mockProvider.On("SupportsStreaming").Return(false).Maybe()

	// Mock successful AI response
	costBreakdown := types.CostInfo{
		InputTokens:   10,
		OutputTokens:  5,
		InputCostUSD:  0.0005,
		OutputCostUSD: 0.0005,
		TotalCost:     0.001,
	}
	response := providers.ChatResponse{
		Content:  "I can help you with that.",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Chat", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	aiExecutor := NewPipelineExecutor(toolRegistry)
	executor := NewScriptedExecutor(aiExecutor)

	// Record time before execution
	beforeExecution := time.Now()

	req := TurnRequest{
		Provider:        mockProvider,
		PromptRegistry:  nil,
		TaskType:        "assistance",
		Region:          "",
		Scenario:        &config.Scenario{},
		ScriptedContent: "What is the capital of France?",
		StateStoreConfig: &StateStoreConfig{
			Store:  statestore.NewArenaStateStore(),
			UserID: "test-user",
		},
		ConversationID: "test-conversation-timestamp",
	}
	if req.StateStoreConfig != nil {
		req.StateStoreConfig.Metadata = map[string]interface{}{
			"validator_configs": []validators.ValidatorConfig{{Type: "length", Params: map[string]interface{}{"length": 100}}},
		}
	}

	// Initialize conversation state
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeIface := req.StateStoreConfig.Store
		store, ok := storeIface.(runtimestore.Store)
		require.True(t, ok, "state store must implement runtimestore.Store")

		initState := &runtimestore.ConversationState{
			ID:       req.ConversationID,
			UserID:   req.StateStoreConfig.UserID,
			Messages: []types.Message{},
			Metadata: map[string]interface{}{},
		}
		saveErr := store.Save(context.Background(), initState)
		require.NoError(t, saveErr)
	}

	err := executor.ExecuteTurn(context.Background(), req)
	require.NoError(t, err)

	// Record time after execution
	afterExecution := time.Now()

	// Load messages from StateStore
	store := req.StateStoreConfig.Store.(runtimestore.Store)
	state, loadErr := store.Load(context.Background(), req.ConversationID)
	require.NoError(t, loadErr)
	require.NotNil(t, state)
	require.Len(t, state.Messages, 2) // user + assistant

	userMessage := state.Messages[0]
	assistantMessage := state.Messages[1]

	// Verify user message has timestamp set
	assert.False(t, userMessage.Timestamp.IsZero(), "User message timestamp should not be zero value")
	assert.True(t, userMessage.Timestamp.After(beforeExecution) || userMessage.Timestamp.Equal(beforeExecution),
		"User message timestamp should be after or equal to execution start time")
	assert.True(t, userMessage.Timestamp.Before(afterExecution) || userMessage.Timestamp.Equal(afterExecution),
		"User message timestamp should be before or equal to execution end time")

	// Verify assistant message has timestamp set
	assert.False(t, assistantMessage.Timestamp.IsZero(), "Assistant message timestamp should not be zero value")
	assert.True(t, assistantMessage.Timestamp.After(beforeExecution) || assistantMessage.Timestamp.Equal(beforeExecution),
		"Assistant message timestamp should be after or equal to execution start time")
	assert.True(t, assistantMessage.Timestamp.Before(afterExecution) || assistantMessage.Timestamp.Equal(afterExecution),
		"Assistant message timestamp should be before or equal to execution end time")

	mockProvider.AssertExpectations(t)
}
