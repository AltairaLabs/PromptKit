package turnexecutors

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/stretchr/testify/mock"
)

// Error path testing mocks - use testify/mock for error injection

// MockStateStore mocks the statestore.Store interface
type MockStateStore struct {
	mock.Mock
}

func (m *MockStateStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*statestore.ConversationState), args.Error(1)
}

func (m *MockStateStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	args := m.Called(ctx, state)
	return args.Error(0)
}

func (m *MockStateStore) Fork(ctx context.Context, sourceID, newID string) error {
	args := m.Called(ctx, sourceID, newID)
	return args.Error(0)
}

func (m *MockStateStore) Delete(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockStateStore) List(ctx context.Context, userID string) ([]string, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]string), args.Error(1)
}

// MockContentGen mocks selfplay.Generator with error support
type MockContentGen struct {
	mock.Mock
}

func (m *MockContentGen) NextUserTurn(ctx context.Context, history []types.Message, scenarioID string, opts *selfplay.GeneratorOptions) (*pipeline.ExecutionResult, error) {
	args := m.Called(ctx, history, scenarioID, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*pipeline.ExecutionResult), args.Error(1)
}

// MockSelfPlayContentProvider mocks selfplay.Provider with error support
type MockSelfPlayContentProvider struct {
	mock.Mock
}

func (m *MockSelfPlayContentProvider) GetContentGenerator(role, persona string) (selfplay.Generator, error) {
	args := m.Called(role, persona)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(selfplay.Generator), args.Error(1)
}

// TestSelfPlayExecutor_LoadHistory_Error tests error handling in loadHistory
func TestSelfPlayExecutor_LoadHistory_Error(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("Load", mock.Anything, "test-conv").Return(nil, errors.New("storage error"))

	executor := &SelfPlayExecutor{}

	req := TurnRequest{
		ConversationID: "test-conv",
		StateStoreConfig: &StateStoreConfig{
			Store: mockStore,
		},
	}

	_, err := executor.loadHistory(context.Background(), &req)
	if err == nil {
		t.Error("Expected error from loadHistory, got nil")
	}
	if err.Error() != "failed to load history from StateStore: storage error" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestSelfPlayExecutor_LoadHistory_NotFound tests handling of ErrNotFound
func TestSelfPlayExecutor_LoadHistory_NotFound(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("Load", mock.Anything, "test-conv").Return(nil, statestore.ErrNotFound)

	executor := &SelfPlayExecutor{}

	req := TurnRequest{
		ConversationID: "test-conv",
		StateStoreConfig: &StateStoreConfig{
			Store: mockStore,
		},
	}

	history, err := executor.loadHistory(context.Background(), &req)
	if err != nil {
		t.Errorf("Expected no error for ErrNotFound, got: %v", err)
	}
	if history != nil {
		t.Errorf("Expected nil history for ErrNotFound, got: %v", history)
	}
}

// TestSelfPlayExecutor_LoadHistory_NoStateStore tests when no state store configured
func TestSelfPlayExecutor_LoadHistory_NoStateStore(t *testing.T) {
	executor := &SelfPlayExecutor{}

	req := TurnRequest{
		ConversationID:   "test-conv",
		StateStoreConfig: nil,
	}

	history, err := executor.loadHistory(context.Background(), &req)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if history != nil {
		t.Errorf("Expected nil history, got: %v", history)
	}
}

// TestSelfPlayExecutor_GenerateUserMessage_Error tests error handling
func TestSelfPlayExecutor_GenerateUserMessage_Error(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)
	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("generation failed"))

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{
		contentProvider: mockContentProvider,
	}

	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	_, _, err := executor.generateUserMessage(context.Background(), &req, nil)
	if err == nil {
		t.Error("Expected error from generateUserMessage, got nil")
	}
	if err.Error() != "failed to generate user turn: generation failed" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestSelfPlayExecutor_GenerateUserMessage_NoContent tests when response is empty
func TestSelfPlayExecutor_GenerateUserMessage_NoContent(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)

	// Return result with no content
	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Role:    "assistant",
			Content: "", // Empty content
		},
	}, nil)

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{
		contentProvider: mockContentProvider,
	}

	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	_, _, err := executor.generateUserMessage(context.Background(), &req, nil)
	if err == nil {
		t.Error("Expected error for empty content, got nil")
	}
	if err.Error() != "no response content generated" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestSelfPlayExecutor_LoadHistoryForStream_Error tests error handling in streaming
func TestSelfPlayExecutor_LoadHistoryForStream_Error(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("Load", mock.Anything, "test-conv").Return(nil, errors.New("storage error"))

	executor := &SelfPlayExecutor{}

	req := TurnRequest{
		ConversationID: "test-conv",
		StateStoreConfig: &StateStoreConfig{
			Store: mockStore,
		},
	}

	outChan := make(chan MessageStreamChunk, 1)

	history, err := executor.loadHistoryForStream(context.Background(), &req, outChan)

	// Should have sent error to channel
	select {
	case chunk := <-outChan:
		if chunk.Error == nil {
			t.Error("Expected error in channel, got nil")
		}
	default:
		t.Error("Expected error chunk in channel")
	}

	if err == nil {
		t.Error("Expected error from loadHistoryForStream, got nil")
	}
	if history != nil {
		t.Error("Expected nil history on error")
	}
}

// TestSelfPlayExecutor_GenerateUserMessageForStream_Error tests streaming gen error
func TestSelfPlayExecutor_GenerateUserMessageForStream_Error(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)
	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("generation failed"))

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{
		contentProvider: mockContentProvider,
	}

	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	outChan := make(chan MessageStreamChunk, 1)

	_, _, err := executor.generateUserMessageForStream(context.Background(), &req, nil, outChan)

	// Should have sent error to channel
	select {
	case chunk := <-outChan:
		if chunk.Error == nil {
			t.Error("Expected error in channel, got nil")
		}
	default:
		t.Error("Expected error chunk in channel")
	}

	if err == nil {
		t.Error("Expected error from generateUserMessageForStream, got nil")
	}
}

// TestSelfPlayExecutor_GenerateUserMessage_CompletionDetected tests that the completion
// marker is detected and stripped from the generated content.
func TestSelfPlayExecutor_GenerateUserMessage_CompletionDetected(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)

	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Role:    "assistant",
			Content: "Thanks for the help! " + selfplay.CompletionMarker,
		},
		Trace: pipeline.ExecutionTrace{},
	}, nil)

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{
		contentProvider: mockContentProvider,
	}

	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
		Metadata:        map[string]interface{}{"natural_termination_enabled": true},
	}

	msg, detected, err := executor.generateUserMessage(context.Background(), &req, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !detected {
		t.Error("Expected completion to be detected")
	}
	if msg.Content != "Thanks for the help!" {
		t.Errorf("Expected marker to be stripped, got: %q", msg.Content)
	}
}

// TestSelfPlayExecutor_GenerateUserMessage_NoCompletionMarker verifies that when
// there is no completion marker, detected is false.
func TestSelfPlayExecutor_GenerateUserMessage_NoCompletionMarker(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)

	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Role:    "assistant",
			Content: "Just a regular question?",
		},
		Trace: pipeline.ExecutionTrace{},
	}, nil)

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{
		contentProvider: mockContentProvider,
	}

	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	msg, detected, err := executor.generateUserMessage(context.Background(), &req, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if detected {
		t.Error("Expected no completion detection")
	}
	if msg.Content != "Just a regular question?" {
		t.Errorf("Expected content unchanged, got: %q", msg.Content)
	}
}

// TestSelfPlayExecutor_GenerateUserMessageForStream_CompletionDetected tests completion
// marker detection in the streaming variant.
func TestSelfPlayExecutor_GenerateUserMessageForStream_CompletionDetected(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)

	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Role:    "assistant",
			Content: "All good, thanks! " + selfplay.CompletionMarker,
		},
		Trace: pipeline.ExecutionTrace{},
	}, nil)

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{contentProvider: mockContentProvider}
	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
		Metadata:        map[string]interface{}{"natural_termination_enabled": true},
	}

	outChan := make(chan MessageStreamChunk, 1)
	msg, detected, err := executor.generateUserMessageForStream(context.Background(), &req, nil, outChan)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !detected {
		t.Error("Expected completion to be detected in streaming path")
	}
	if msg.Content != "All good, thanks!" {
		t.Errorf("Expected marker stripped, got: %q", msg.Content)
	}
}

// TestSelfPlayExecutor_GenerateUserMessageForStream_NoContent tests empty content in streaming.
func TestSelfPlayExecutor_GenerateUserMessageForStream_NoContent(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockGen := new(MockContentGen)

	mockGen.On("NextUserTurn", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&pipeline.ExecutionResult{
		Response: &pipeline.Response{Role: "assistant", Content: ""},
		Trace:    pipeline.ExecutionTrace{},
	}, nil)

	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(mockGen, nil)

	executor := &SelfPlayExecutor{contentProvider: mockContentProvider}
	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	outChan := make(chan MessageStreamChunk, 1)
	_, _, err := executor.generateUserMessageForStream(context.Background(), &req, nil, outChan)
	if err == nil {
		t.Error("Expected error for empty content")
	}

	select {
	case chunk := <-outChan:
		if chunk.Error == nil {
			t.Error("Expected error in channel")
		}
	default:
		t.Error("Expected error chunk in channel")
	}
}

// TestSelfPlayExecutor_GenerateUserMessageForStream_GetGeneratorError tests provider error.
func TestSelfPlayExecutor_GenerateUserMessageForStream_GetGeneratorError(t *testing.T) {
	mockContentProvider := new(MockSelfPlayContentProvider)
	mockContentProvider.On("GetContentGenerator", "test-role", "test-persona").Return(nil, errors.New("provider unavailable"))

	executor := &SelfPlayExecutor{contentProvider: mockContentProvider}
	req := TurnRequest{
		SelfPlayRole:    "test-role",
		SelfPlayPersona: "test-persona",
		Scenario:        &config.Scenario{ID: "test-scenario"},
	}

	outChan := make(chan MessageStreamChunk, 1)
	_, _, err := executor.generateUserMessageForStream(context.Background(), &req, nil, outChan)
	if err == nil {
		t.Error("Expected error from GetContentGenerator")
	}
}
