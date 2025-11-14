package mock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MockToolProvider extends MockProvider to support tool/function calling.
// It implements the ToolSupport interface to enable tool call simulation
// while maintaining compatibility with the existing MockProvider API.
type MockToolProvider struct {
	*MockProvider
}

// NewMockToolProvider creates a new mock provider with tool support.
// This uses default in-memory responses for backward compatibility.
func NewMockToolProvider(id, model string, includeRawOutput bool, additionalConfig map[string]interface{}) *MockToolProvider {

	if additionalConfig != nil {
		if mockConfigPath, ok := additionalConfig["mock_config"].(string); ok && mockConfigPath != "" {
			// Create file-based repository and use MockToolProvider for tool call simulation
			repository, err := NewFileMockRepository(mockConfigPath)
			if err != nil {
				logger.Warn("failed to load mock config from %s: %w", mockConfigPath, err)
				return &MockToolProvider{
					MockProvider: NewMockProvider(id, model, includeRawOutput),
				}
			}
			return &MockToolProvider{
				MockProvider: NewMockProviderWithRepository(id, model, includeRawOutput, repository),
			}
		}
	}

	return &MockToolProvider{
		MockProvider: NewMockProvider(id, model, includeRawOutput),
	}

}

// NewMockToolProviderWithRepository creates a mock provider with tool support
// using a custom response repository for advanced scenarios.
func NewMockToolProviderWithRepository(id, model string, includeRawOutput bool, repo MockResponseRepository) *MockToolProvider {
	return &MockToolProvider{
		MockProvider: NewMockProviderWithRepository(id, model, includeRawOutput, repo),
	}
}

// BuildTooling implements the ToolSupport interface.
// For mock providers, we just return the tools as-is since we don't need
// to transform them into a provider-specific format.
func (m *MockToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
	logger.Debug("MockToolProvider BuildTooling",
		"provider_id", m.id,
		"tool_count", len(descriptors))

	// For mocking purposes, we return the descriptors unchanged
	return descriptors, nil
}

// PredictWithTools implements the ToolSupport interface.
// This method handles the initial chat request with tools available,
// potentially returning tool calls based on the mock configuration.
func (m *MockToolProvider) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	logger.Debug("MockToolProvider PredictWithTools",
		"provider_id", m.id,
		"tool_choice", toolChoice,
		"message_count", len(req.Messages))

	// Detect turn number based on conversation history and tool results
	turnNumber := m.detectTurnFromConversation(req)

	// Get mock turn from repository
	params := MockResponseParams{
		ScenarioID: m.getScenarioID(req),
		TurnNumber: turnNumber,
		ProviderID: m.id,
		ModelName:  m.model,
	}

	logger.Debug("MockToolProvider PredictWithTools using turn",
		"provider_id", m.id,
		"detected_turn", turnNumber,
		"scenario_id", params.ScenarioID)

	mockTurn, err := m.repository.GetTurn(ctx, params)
	if err != nil {
		return providers.PredictionResponse{}, nil, fmt.Errorf("failed to get mock turn: %w", err)
	}

	// Build cost info for the response
	inputTokens := m.calculateInputTokens(req.Messages)
	outputTokens := m.calculateOutputTokens(mockTurn.Content)
	costInfo := m.generateMockCostInfo(inputTokens, outputTokens)

	// If turn type is tool_calls, return tool calls
	if mockTurn.Type == "tool_calls" {
		toolCalls := make([]types.MessageToolCall, len(mockTurn.ToolCalls))
		for i, tc := range mockTurn.ToolCalls {
			argsBytes, err := json.Marshal(tc.Arguments)
			if err != nil {
				return providers.PredictionResponse{}, nil, fmt.Errorf("failed to marshal tool call arguments: %w", err)
			}

			toolCalls[i] = types.MessageToolCall{
				ID:   fmt.Sprintf("call_%d_%s", i, tc.Name),
				Name: tc.Name,
				Args: json.RawMessage(argsBytes),
			}
		}

		logger.Debug("MockToolProvider returning tool calls",
			"provider_id", m.id,
			"tool_call_count", len(toolCalls))

		return providers.PredictionResponse{
			Content:   mockTurn.Content,
			ToolCalls: toolCalls,
			CostInfo:  &costInfo,
		}, toolCalls, nil
	}

	// Otherwise return normal text response
	logger.Debug("MockToolProvider returning text response",
		"provider_id", m.id,
		"response_length", len(mockTurn.Content))

	return providers.PredictionResponse{
		Content:  mockTurn.Content,
		CostInfo: &costInfo,
	}, nil, nil
}

// generateMockCostInfo creates cost information for mock responses.
func (m *MockToolProvider) generateMockCostInfo(inputTokens, outputTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.00001,  // $0.01 per 1K tokens
		OutputCostUSD: float64(outputTokens) * 0.00001, // $0.01 per 1K tokens
		TotalCost:     float64(inputTokens+outputTokens) * 0.00001,
	}
}

// calculateInputTokens estimates input tokens from messages (rough approximation).
func (m *MockToolProvider) calculateInputTokens(messages []types.Message) int {
	tokenCount := 0
	for _, msg := range messages {
		tokenCount += len(msg.Content) / 4 // Rough approximation: ~4 chars per token
	}
	if tokenCount == 0 {
		tokenCount = 10
	}
	return tokenCount
}

// calculateOutputTokens estimates output tokens from response text (rough approximation).
func (m *MockToolProvider) calculateOutputTokens(responseText string) int {
	tokens := len(responseText) / 4
	if tokens == 0 {
		tokens = 20
	}
	return tokens
}

// detectTurnFromConversation analyzes the conversation history to determine the current turn number
func (m *MockToolProvider) detectTurnFromConversation(req providers.PredictionRequest) int {
	// Start with base turn number from metadata
	baseTurnNumber := 1
	if req.Metadata != nil {
		if turn, ok := req.Metadata["mock_turn_number"].(int); ok {
			baseTurnNumber = turn
		}
	}

	// Count tool result messages to detect if we've advanced past the initial turn
	toolResultCount := 0
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			toolResultCount++
		}
	}

	// If we have tool results, we're in a continuation turn
	adjustedTurnNumber := baseTurnNumber
	if toolResultCount > 0 {
		adjustedTurnNumber = baseTurnNumber + 1
	}

	logger.Debug("MockToolProvider turn detection",
		"provider_id", m.id,
		"base_turn", baseTurnNumber,
		"tool_result_count", toolResultCount,
		"adjusted_turn", adjustedTurnNumber)

	return adjustedTurnNumber
}

// getScenarioID extracts the scenario ID from request metadata
func (m *MockToolProvider) getScenarioID(req providers.PredictionRequest) string {
	if req.Metadata != nil {
		if sid, ok := req.Metadata["mock_scenario_id"].(string); ok {
			return sid
		}
	}
	return ""
}
