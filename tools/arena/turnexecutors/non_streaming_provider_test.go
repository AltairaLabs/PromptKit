package turnexecutors

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/mock"
)

// TestScriptedExecutor_HandleNonStreamingProvider_Error tests error handling
func TestScriptedExecutor_HandleNonStreamingProvider_Error(t *testing.T) {
	// Mock provider that doesn't support streaming
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("SupportsStreaming").Return(false)
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()

	// Mock provider will return an error during Predict
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(providers.PredictionResponse{}, errors.New("predict failed"))

	toolRegistry := tools.NewRegistry()
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(pipelineExecutor)

	scenario := &config.Scenario{
		TaskType: "test",
	}

	req := TurnRequest{
		Provider:        mockProvider,
		Scenario:        scenario,
		Temperature:     0.7,
		MaxTokens:       100,
		PromptRegistry:  nil,
		TaskType:        "test",
		ScriptedContent: "Hello",
	}

	outChan := make(chan MessageStreamChunk, 1)

	// Call handleNonStreamingProvider - should detect error and send to channel
	handled := executor.handleNonStreamingProvider(context.Background(), req, outChan)

	if !handled {
		t.Error("Expected handleNonStreamingProvider to return true")
	}

	// Check error was sent to channel
	select {
	case chunk := <-outChan:
		if chunk.Error == nil {
			t.Error("Expected error in channel, got nil")
		}
		// Error message includes provider failure
		expectedSubstring := "provider call failed"
		if !strings.Contains(chunk.Error.Error(), expectedSubstring) {
			t.Errorf("Expected error to contain '%s', got: %v", expectedSubstring, chunk.Error)
		}
	default:
		t.Error("Expected error chunk in channel")
	}
}

// TestScriptedExecutor_HandleNonStreamingProvider_Success tests success case
func TestScriptedExecutor_HandleNonStreamingProvider_Success(t *testing.T) {
	// Mock provider that doesn't support streaming
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("SupportsStreaming").Return(false)
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()

	// Mock successful response
	costBreakdown := types.CostInfo{
		InputTokens:  10,
		OutputTokens: 5,
	}
	response := providers.PredictionResponse{
		Content:  "Hello response",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(pipelineExecutor)

	scenario := &config.Scenario{
		TaskType: "test",
	}

	req := TurnRequest{
		Provider:        mockProvider,
		Scenario:        scenario,
		Temperature:     0.7,
		MaxTokens:       100,
		PromptRegistry:  nil,
		TaskType:        "test",
		ScriptedContent: "Hello",
	}

	outChan := make(chan MessageStreamChunk, 1)

	// Call handleNonStreamingProvider
	handled := executor.handleNonStreamingProvider(context.Background(), req, outChan)

	if !handled {
		t.Error("Expected handleNonStreamingProvider to return true")
	}

	// Check success finish chunk was sent
	select {
	case chunk := <-outChan:
		if chunk.Error != nil {
			t.Errorf("Expected no error, got: %v", chunk.Error)
		}
		if chunk.FinishReason == nil || *chunk.FinishReason != "stop" {
			t.Error("Expected FinishReason='stop'")
		}
	default:
		t.Error("Expected finish chunk in channel")
	}
}

// TestScriptedExecutor_HandleNonStreamingProvider_NotHandled tests streaming provider
func TestScriptedExecutor_HandleNonStreamingProvider_NotHandled(t *testing.T) {
	// Mock provider that DOES support streaming
	mockProvider := new(MockProvider)
	mockProvider.On("SupportsStreaming").Return(true)

	toolRegistry := tools.NewRegistry()
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)
	executor := NewScriptedExecutor(pipelineExecutor)

	req := TurnRequest{
		Provider: mockProvider,
	}

	outChan := make(chan MessageStreamChunk, 1)

	// Should return false for streaming provider
	handled := executor.handleNonStreamingProvider(context.Background(), req, outChan)

	if handled {
		t.Error("Expected handleNonStreamingProvider to return false for streaming provider")
	}

	// No chunks should be sent
	select {
	case <-outChan:
		t.Error("Expected no chunks in channel")
	default:
		// Expected - no chunks
	}
}

// TestSelfPlayExecutor_HandleNonStreamingProvider_Error tests error handling
func TestSelfPlayExecutor_HandleNonStreamingProvider_Error(t *testing.T) {
	// Mock provider that doesn't support streaming
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("SupportsStreaming").Return(false)
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()

	// Mock provider will return an error during Predict
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(providers.PredictionResponse{}, errors.New("predict failed"))

	toolRegistry := tools.NewRegistry()
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)

	mockContentProvider := &MockSelfPlayProvider{
		contentGen: &MockContentGenerator{
			message: types.Message{
				Role:    "user",
				Content: "User message",
			},
		},
	}

	executor := NewSelfPlayExecutor(pipelineExecutor, mockContentProvider)

	scenario := &config.Scenario{
		TaskType: "test",
	}

	req := TurnRequest{
		Provider:        mockProvider,
		Scenario:        scenario,
		Temperature:     0.7,
		MaxTokens:       100,
		PromptRegistry:  nil,
		TaskType:        "test",
		SelfPlayRole:    "user",
		SelfPlayPersona: "test-persona",
	}

	userMessage := types.Message{
		Role:    "user",
		Content: "Test message",
	}

	outChan := make(chan MessageStreamChunk, 2)

	// Call handleNonStreamingProvider - should detect error and send to channel
	handled := executor.handleNonStreamingProvider(context.Background(), req, userMessage, outChan)

	if !handled {
		t.Error("Expected handleNonStreamingProvider to return true")
	}

	// Check error was sent to channel
	select {
	case chunk := <-outChan:
		if chunk.Error == nil {
			t.Error("Expected error in channel, got nil")
		}
	default:
		t.Error("Expected error chunk in channel")
	}
}

// TestSelfPlayExecutor_HandleNonStreamingProvider_Success tests success case
func TestSelfPlayExecutor_HandleNonStreamingProvider_Success(t *testing.T) {
	// Mock provider that doesn't support streaming
	mockProvider := new(MockProvider)
	mockProvider.On("ID").Return("test-provider")
	mockProvider.On("SupportsStreaming").Return(false)
	mockProvider.On("ShouldIncludeRawOutput").Return(false).Maybe()

	// Mock successful response
	costBreakdown := types.CostInfo{
		InputTokens:  10,
		OutputTokens: 5,
	}
	response := providers.PredictionResponse{
		Content:  "Response content",
		CostInfo: &costBreakdown,
	}
	mockProvider.On("Predict", mock.Anything, mock.Anything).Return(response, nil)

	toolRegistry := tools.NewRegistry()
	pipelineExecutor := NewPipelineExecutor(toolRegistry, nil)

	mockContentProvider := &MockSelfPlayProvider{
		contentGen: &MockContentGenerator{
			message: types.Message{
				Role:    "user",
				Content: "User message",
			},
		},
	}

	executor := NewSelfPlayExecutor(pipelineExecutor, mockContentProvider)

	scenario := &config.Scenario{
		TaskType: "test",
	}

	req := TurnRequest{
		Provider:        mockProvider,
		Scenario:        scenario,
		Temperature:     0.7,
		MaxTokens:       100,
		PromptRegistry:  nil,
		TaskType:        "test",
		SelfPlayRole:    "user",
		SelfPlayPersona: "test-persona",
	}

	userMessage := types.Message{
		Role:    "user",
		Content: "Test message",
	}

	outChan := make(chan MessageStreamChunk, 1)

	// Call handleNonStreamingProvider
	handled := executor.handleNonStreamingProvider(context.Background(), req, userMessage, outChan)

	if !handled {
		t.Error("Expected handleNonStreamingProvider to return true")
	}

	// Check finish chunk was sent
	select {
	case chunk := <-outChan:
		if chunk.Error != nil {
			t.Errorf("Expected no error, got: %v", chunk.Error)
		}
		if chunk.FinishReason == nil || *chunk.FinishReason != "stop" {
			t.Error("Expected FinishReason='stop'")
		}
	default:
		t.Error("Expected finish chunk in channel")
	}
}
