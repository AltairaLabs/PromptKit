package providers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Context keys for mock provider scenario and turn information
type contextKey string

const (
	// MockScenarioIDKey is used to pass scenario ID through context
	MockScenarioIDKey contextKey = "mock_scenario_id"
	// MockTurnNumberKey is used to pass turn number through context
	MockTurnNumberKey contextKey = "mock_turn_number"
)

// MockProvider is a provider implementation for testing and development.
// It returns mock responses without making any API calls, using a repository
// pattern to source responses from various backends (files, memory, databases).
//
// MockProvider is designed to be reusable across different contexts:
//   - Arena testing: scenario and turn-specific responses
//   - SDK examples: simple deterministic responses
//   - Unit tests: programmatic response configuration
type MockProvider struct {
	id                string
	model             string
	value             string                 // For backward compatibility with existing tests
	repository        MockResponseRepository // Source of mock responses
	includeRawOutput  bool
	supportsStreaming bool
}

// NewMockProvider creates a new mock provider with default in-memory responses.
// This constructor maintains backward compatibility with existing code.
func NewMockProvider(id, model string, includeRawOutput bool) *MockProvider {
	response := fmt.Sprintf("Mock response from %s model %s", id, model)
	repo := NewInMemoryMockRepository(response)

	return &MockProvider{
		id:                id,
		model:             model,
		value:             response, // For backward compatibility
		repository:        repo,
		includeRawOutput:  includeRawOutput,
		supportsStreaming: true,
	}
}

// NewMockProviderWithRepository creates a mock provider with a custom response repository.
// This allows for advanced scenarios like file-based or database-backed mock responses.
func NewMockProviderWithRepository(id, model string, includeRawOutput bool, repo MockResponseRepository) *MockProvider {
	return &MockProvider{
		id:                id,
		model:             model,
		repository:        repo,
		includeRawOutput:  includeRawOutput,
		supportsStreaming: true,
	}
}

// ID returns the provider ID.
func (m *MockProvider) ID() string {
	return m.id
}

// Chat returns a mock response using the configured repository.
func (m *MockProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	// Try to get response from repository with scenario context
	params := MockResponseParams{
		ProviderID: m.id,
		ModelName:  m.model,
	}

	// Extract scenario context if available
	if scenarioID, ok := ctx.Value(MockScenarioIDKey).(string); ok {
		params.ScenarioID = scenarioID
	}
	if turnNumber, ok := ctx.Value(MockTurnNumberKey).(int); ok {
		params.TurnNumber = turnNumber
	}

	// Debug logging for troubleshooting mock provider behavior
	logger.Debug("MockProvider Chat request",
		"provider_id", m.id,
		"model", m.model,
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"has_scenario_context", params.ScenarioID != "",
		"backward_compat_value", m.value != "")

	responseText, err := m.repository.GetResponse(ctx, params)
	if err != nil {
		logger.Debug("MockProvider repository error", "error", err)
		return ChatResponse{}, fmt.Errorf("failed to get mock response: %w", err)
	}

	// Use value if set (for backward compatibility with tests), otherwise use repository response
	if m.value != "" {
		logger.Debug("MockProvider using backward compatibility value", "response", m.value)
		responseText = m.value
	} else {
		logger.Debug("MockProvider using repository response", "response", responseText)
	}

	// Count tokens based on message length (rough approximation)
	inputTokens := 0
	for _, msg := range req.Messages {
		inputTokens += len(msg.Content) / 4 // Rough approximation: ~4 chars per token
	}
	if inputTokens == 0 {
		inputTokens = 10
	}

	outputTokens := len(responseText) / 4
	if outputTokens == 0 {
		outputTokens = 20
	}

	costBreakdown := types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.00001,  // $0.01 per 1K tokens
		OutputCostUSD: float64(outputTokens) * 0.00001, // $0.01 per 1K tokens
		TotalCost:     float64(inputTokens+outputTokens) * 0.00001,
	}

	return ChatResponse{
		Content:  responseText,
		CostInfo: &costBreakdown,
	}, nil
}

// ChatStream returns a mock streaming response using the configured repository.
func (m *MockProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	outChan := make(chan StreamChunk, 1)

	go func() {
		defer close(outChan)

		// Try to get response from repository with scenario context
		params := MockResponseParams{
			ProviderID: m.id,
			ModelName:  m.model,
		}

		// Extract scenario context if available
		if scenarioID, ok := ctx.Value(MockScenarioIDKey).(string); ok {
			params.ScenarioID = scenarioID
		}
		if turnNumber, ok := ctx.Value(MockTurnNumberKey).(int); ok {
			params.TurnNumber = turnNumber
		}

		// Debug logging for troubleshooting mock provider streaming behavior
		logger.Debug("MockProvider ChatStream request",
			"provider_id", m.id,
			"model", m.model,
			"scenario_id", params.ScenarioID,
			"turn_number", params.TurnNumber,
			"has_scenario_context", params.ScenarioID != "",
			"backward_compat_value", m.value != "")

		responseText, err := m.repository.GetResponse(ctx, params)
		if err != nil {
			logger.Debug("MockProvider stream repository error", "error", err)
			// Send error in stream
			return
		}

		// Use value if set (for backward compatibility with tests), otherwise use repository response
		if m.value != "" {
			logger.Debug("MockProvider stream using backward compatibility value", "response", m.value)
			responseText = m.value
		} else {
			logger.Debug("MockProvider stream using repository response", "response", responseText)
		}

		// Count input tokens
		inputTokens := 0
		for _, msg := range req.Messages {
			inputTokens += len(msg.Content) / 4
		}
		if inputTokens == 0 {
			inputTokens = 10
		}

		outputTokens := len(responseText) / 4
		if outputTokens == 0 {
			outputTokens = 20
		}

		// Send the mock response as a single chunk
		outChan <- StreamChunk{
			Content:      responseText,
			Delta:        responseText,
			TokenCount:   outputTokens,
			DeltaTokens:  outputTokens,
			FinishReason: ptr("stop"),
			FinalResult: &ChatResponse{
				Content: responseText,
				CostInfo: &types.CostInfo{
					InputTokens:   inputTokens,
					OutputTokens:  outputTokens,
					InputCostUSD:  float64(inputTokens) * 0.00001,
					OutputCostUSD: float64(outputTokens) * 0.00001,
					TotalCost:     float64(inputTokens+outputTokens) * 0.00001,
				},
			},
		}
	}()

	return outChan, nil
}

// SupportsStreaming indicates whether the provider supports streaming.
func (m *MockProvider) SupportsStreaming() bool {
	return m.supportsStreaming
}

// Close is a no-op for the mock provider.
func (m *MockProvider) Close() error {
	return nil
}

// ShouldIncludeRawOutput returns whether raw API responses should be included.
func (m *MockProvider) ShouldIncludeRawOutput() bool {
	return m.includeRawOutput
}

// CalculateCost calculates cost breakdown for given token counts.
func (m *MockProvider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
	// Mock provider uses simple fixed pricing
	inputCostPer1K := 0.01
	outputCostPer1K := 0.01
	cachedCostPer1K := 0.005 // 50% discount for cached tokens

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

// WithMockScenarioContext adds scenario information to context for MockProvider
func WithMockScenarioContext(ctx context.Context, scenarioID string, turnNumber int) context.Context {
	ctx = context.WithValue(ctx, MockScenarioIDKey, scenarioID)
	ctx = context.WithValue(ctx, MockTurnNumberKey, turnNumber)
	return ctx
}
