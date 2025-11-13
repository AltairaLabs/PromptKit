package mock

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

func init() {
	providers.RegisterProviderFactory("mock", func(spec providers.ProviderSpec) (providers.Provider, error) {
		// Use MockToolProvider by default as it's backward compatible with MockProvider
		// and supports both tool calls and scenario-specific responses
		if repo, ok := spec.AdditionalConfig["repository"].(MockResponseRepository); ok {
			return NewMockToolProviderWithRepository(spec.ID, spec.Model, spec.IncludeRawOutput, repo), nil
		}
		return NewMockToolProvider(spec.ID, spec.Model, spec.IncludeRawOutput, spec.AdditionalConfig), nil
	})
}

// ID returns the provider ID.
func (m *MockProvider) ID() string {
	return m.id
}

// Chat returns a mock response using the configured repository.
func (m *MockProvider) Chat(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	// Try to get response from repository with scenario context
	params := MockResponseParams{
		ProviderID: m.id,
		ModelName:  m.model,
	}

	// Extract scenario context if available from providers.ChatRequest.Metadata
	if req.Metadata != nil {
		if scenarioID, ok := req.Metadata["mock_scenario_id"].(string); ok {
			params.ScenarioID = scenarioID
		}
		if turnNumber, ok := req.Metadata["mock_turn_number"].(int); ok {
			params.TurnNumber = turnNumber
		}
	}

	// Debug logging for troubleshooting mock provider behavior
	logger.Debug("MockProvider Chat request",
		"provider_id", m.id,
		"model", m.model,
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"has_scenario_context", params.ScenarioID != "",
		"backward_compat_value", m.value != "")

	// Get structured turn response (supports multimodal content)
	turn, err := m.repository.GetTurn(ctx, params)
	if err != nil {
		logger.Debug("MockProvider repository error", "error", err)
		return providers.ChatResponse{}, fmt.Errorf("failed to get mock response: %w", err)
	}

	// Use value if set (for backward compatibility with tests)
	var responseText string
	var parts []types.ContentPart
	
	if m.value != "" {
		logger.Debug("MockProvider using backward compatibility value", "response", m.value)
		responseText = m.value
		// For backward compatibility, create a single text part
		parts = []types.ContentPart{types.NewTextPart(m.value)}
	} else {
		logger.Debug("MockProvider using repository response", "turn_type", turn.Type, "has_parts", len(turn.Parts) > 0)
		// Get text content for backward compatibility
		responseText = turn.Content
		if responseText == "" {
			responseText = turn.Text
		}
		
		// Convert mock turn to content parts
		parts = turn.ToContentParts()
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

	return providers.ChatResponse{
		Content:  responseText,
		Parts:    parts,
		CostInfo: &costBreakdown,
	}, nil
}

// ChatStream returns a mock streaming response using the configured repository.
func (m *MockProvider) ChatStream(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	outChan := make(chan providers.StreamChunk, 1)

	go func() {
		defer close(outChan)
		m.handleStreamRequest(ctx, req, outChan)
	}()

	return outChan, nil
}

// handleStreamRequest processes the stream request and sends the response
func (m *MockProvider) handleStreamRequest(ctx context.Context, req providers.ChatRequest, outChan chan<- providers.StreamChunk) {
	params := m.buildMockResponseParams(req)
	m.logStreamRequest(params)

	// Get structured turn response (supports multimodal content)
	turn, err := m.getStreamTurn(ctx, params)
	if err != nil {
		logger.Debug("MockProvider stream repository error", "error", err)
		return
	}

	// Use value if set (for backward compatibility with tests)
	var responseText string
	var parts []types.ContentPart
	
	if m.value != "" {
		logger.Debug("MockProvider stream using backward compatibility value", "response", m.value)
		responseText = m.value
		parts = []types.ContentPart{types.NewTextPart(m.value)}
	} else {
		logger.Debug("MockProvider stream using repository response", "turn_type", turn.Type, "has_parts", len(turn.Parts) > 0)
		responseText = turn.Content
		if responseText == "" {
			responseText = turn.Text
		}
		parts = turn.ToContentParts()
	}

	inputTokens := m.calculateInputTokens(req.Messages)
	outputTokens := m.calculateOutputTokens(responseText)

	chunk := m.createStreamChunk(responseText, parts, inputTokens, outputTokens)
	outChan <- chunk
}

// buildMockResponseParams creates parameters for the mock response
func (m *MockProvider) buildMockResponseParams(req providers.ChatRequest) MockResponseParams {
	params := MockResponseParams{
		ProviderID: m.id,
		ModelName:  m.model,
	}

	// Extract scenario context if available from providers.ChatRequest.Metadata
	if req.Metadata != nil {
		if scenarioID, ok := req.Metadata["mock_scenario_id"].(string); ok {
			params.ScenarioID = scenarioID
		}
		if turnNumber, ok := req.Metadata["mock_turn_number"].(int); ok {
			params.TurnNumber = turnNumber
		}
	}

	return params
}

// logStreamRequest logs debug information for the stream request
func (m *MockProvider) logStreamRequest(params MockResponseParams) {
	logger.Debug("MockProvider ChatStream request",
		"provider_id", m.id,
		"model", m.model,
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"has_scenario_context", params.ScenarioID != "",
		"backward_compat_value", m.value != "")
}

// getStreamResponse gets the response text from repository or fallback value
func (m *MockProvider) getStreamTurn(ctx context.Context, params MockResponseParams) (*MockTurn, error) {
	return m.repository.GetTurn(ctx, params)
}

// calculateInputTokens estimates input tokens from messages
func (m *MockProvider) calculateInputTokens(messages []types.Message) int {
	inputTokens := 0
	for _, msg := range messages {
		inputTokens += len(msg.Content) / 4
	}
	if inputTokens == 0 {
		inputTokens = 10
	}
	return inputTokens
}

// calculateOutputTokens estimates output tokens from response text
func (m *MockProvider) calculateOutputTokens(responseText string) int {
	outputTokens := len(responseText) / 4
	if outputTokens == 0 {
		outputTokens = 20
	}
	return outputTokens
}

// createStreamChunk creates a stream chunk with the response and cost info
func (m *MockProvider) createStreamChunk(responseText string, parts []types.ContentPart, inputTokens, outputTokens int) providers.StreamChunk {
	costInfo := &types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.00001,
		OutputCostUSD: float64(outputTokens) * 0.00001,
		TotalCost:     float64(inputTokens+outputTokens) * 0.00001,
	}

	return providers.StreamChunk{
		Content:      responseText,
		Delta:        responseText,
		TokenCount:   outputTokens,
		DeltaTokens:  outputTokens,
		FinishReason: ptr("stop"),
		CostInfo:     costInfo,
		FinalResult: &providers.ChatResponse{
			Content:  responseText,
			Parts:    parts,
			CostInfo: costInfo,
		},
	}
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

// ptr is a helper function to create a pointer to a value
func ptr[T any](v T) *T {
	return &v
}
