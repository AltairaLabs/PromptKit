// Package mock provides mock provider implementation for testing and development.
package mock

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Provider is a provider implementation for testing and development.
// It returns mock responses without making any API calls, using a repository
// pattern to source responses from various backends (files, memory, databases).
//
// Provider is designed to be reusable across different contexts:
//   - Arena testing: scenario and turn-specific responses
//   - SDK examples: simple deterministic responses
//   - Unit tests: programmatic response configuration
type Provider struct {
	id                string
	model             string
	value             string             // For backward compatibility with existing tests
	repository        ResponseRepository // Source of mock responses
	includeRawOutput  bool
	supportsStreaming bool
}

// NewProvider creates a new mock provider with default in-memory responses.
// This constructor maintains backward compatibility with existing code.
func NewProvider(id, model string, includeRawOutput bool) *Provider {
	response := fmt.Sprintf("Mock response from %s model %s", id, model)
	repo := NewInMemoryMockRepository(response)

	return &Provider{
		id:                id,
		model:             model,
		value:             response, // For backward compatibility
		repository:        repo,
		includeRawOutput:  includeRawOutput,
		supportsStreaming: true,
	}
}

// NewProviderWithRepository creates a mock provider with a custom response repository.
// This allows for advanced scenarios like file-based or database-backed mock responses.
func NewProviderWithRepository(id, model string, includeRawOutput bool, repo ResponseRepository) *Provider {
	return &Provider{
		id:                id,
		model:             model,
		repository:        repo,
		includeRawOutput:  includeRawOutput,
		supportsStreaming: true,
	}
}

func init() {
	providers.RegisterProviderFactory("mock", func(spec providers.ProviderSpec) (providers.Provider, error) {
		// Use ToolProvider by default as it's backward compatible with MockProvider
		// and supports both tool calls and scenario-specific responses
		if repo, ok := spec.AdditionalConfig["repository"].(ResponseRepository); ok {
			return NewToolProviderWithRepository(spec.ID, spec.Model, spec.IncludeRawOutput, repo), nil
		}
		return NewToolProvider(spec.ID, spec.Model, spec.IncludeRawOutput, spec.AdditionalConfig), nil
	})
}

// ID returns the provider ID.
func (m *Provider) ID() string {
	return m.id
}

// Predict returns a mock response using the configured repository.
func (m *Provider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	// Try to get response from repository with scenario context
	params := ResponseParams{
		ProviderID: m.id,
		ModelName:  m.model,
	}

	// Extract scenario context if available from providers.PredictionRequest.Metadata
	if req.Metadata != nil {
		if scenarioID, ok := req.Metadata["mock_scenario_id"].(string); ok {
			params.ScenarioID = scenarioID
		}
		if turnNumber, ok := req.Metadata["mock_turn_number"].(int); ok {
			params.TurnNumber = turnNumber
		}
		// Extract persona and role for selfplay user turns
		if personaID, ok := req.Metadata["mock_persona_id"].(string); ok {
			params.PersonaID = personaID
		}
		if arenaRole, ok := req.Metadata["arena_role"].(string); ok {
			params.ArenaRole = arenaRole
		}
	}

	// Debug logging for troubleshooting mock provider behavior
	logger.Debug("MockProvider Predict request",
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
		return providers.PredictionResponse{}, fmt.Errorf("failed to get mock response: %w", err)
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
		// Get text content
		responseText = turn.Content

		// Convert mock turn to content parts
		parts = turn.ToContentParts()
		logger.Debug("MockProvider parts converted", "num_parts", len(parts), "contentLength", len(responseText))

		// If we have parts but no responseText, generate a summary from parts
		if responseText == "" && len(parts) > 0 {
			responseText = generateContentSummary(parts)
		}
	} // Count tokens based on message length (rough approximation)
	inputTokens := 0
	for i := range req.Messages {
		inputTokens += len(req.Messages[i].Content) / 4 // Rough approximation: ~4 chars per token
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

	return providers.PredictionResponse{
		Content:  responseText,
		Parts:    parts,
		CostInfo: &costBreakdown,
	}, nil
}

// PredictStream returns a mock streaming response using the configured repository.
func (m *Provider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	outChan := make(chan providers.StreamChunk, 1)

	go func() {
		defer close(outChan)
		m.handleStreamRequest(ctx, req, outChan)
	}()

	return outChan, nil
}

// handleStreamRequest processes the stream request and sends the response
func (m *Provider) handleStreamRequest(ctx context.Context, req providers.PredictionRequest, outChan chan<- providers.StreamChunk) {
	params := m.buildResponseParams(req)
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
		parts = turn.ToContentParts()
	}

	inputTokens := m.calculateInputTokens(req.Messages)
	outputTokens := m.calculateOutputTokens(responseText)

	chunk := m.createStreamChunk(responseText, parts, inputTokens, outputTokens)
	outChan <- chunk
}

// buildResponseParams creates parameters for the mock response
func (m *Provider) buildResponseParams(req providers.PredictionRequest) ResponseParams {
	params := ResponseParams{
		ProviderID: m.id,
		ModelName:  m.model,
	}

	// Extract scenario context if available from providers.PredictionRequest.Metadata
	if req.Metadata != nil {
		if scenarioID, ok := req.Metadata["mock_scenario_id"].(string); ok {
			params.ScenarioID = scenarioID
		}
		if turnNumber, ok := req.Metadata["mock_turn_number"].(int); ok {
			params.TurnNumber = turnNumber
		}
		// Extract persona and role for selfplay user turns
		if personaID, ok := req.Metadata["mock_persona_id"].(string); ok {
			params.PersonaID = personaID
		}
		if arenaRole, ok := req.Metadata["arena_role"].(string); ok {
			params.ArenaRole = arenaRole
		}
	}

	return params
}

// logStreamRequest logs debug information for the stream request
func (m *Provider) logStreamRequest(params ResponseParams) {
	logger.Debug("MockProvider PredictStream request",
		"provider_id", m.id,
		"model", m.model,
		"scenario_id", params.ScenarioID,
		"turn_number", params.TurnNumber,
		"has_scenario_context", params.ScenarioID != "",
		"backward_compat_value", m.value != "")
}

// getStreamResponse gets the response text from repository or fallback value
func (m *Provider) getStreamTurn(ctx context.Context, params ResponseParams) (*Turn, error) {
	return m.repository.GetTurn(ctx, params)
}

// calculateInputTokens estimates input tokens from messages
func (m *Provider) calculateInputTokens(messages []types.Message) int {
	inputTokens := 0
	for i := range messages {
		inputTokens += len(messages[i].Content) / 4
	}
	if inputTokens == 0 {
		inputTokens = 10
	}
	return inputTokens
}

// calculateOutputTokens estimates output tokens from response text
func (m *Provider) calculateOutputTokens(responseText string) int {
	outputTokens := len(responseText) / 4
	if outputTokens == 0 {
		outputTokens = 20
	}
	return outputTokens
}

// createStreamChunk creates a stream chunk with the response and cost info
func (m *Provider) createStreamChunk(responseText string, parts []types.ContentPart, inputTokens, outputTokens int) providers.StreamChunk {
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
		FinalResult: &providers.PredictionResponse{
			Content:  responseText,
			Parts:    parts,
			CostInfo: costInfo,
		},
	}
}

// SupportsStreaming indicates whether the provider supports streaming.
func (m *Provider) SupportsStreaming() bool {
	return m.supportsStreaming
}

// Close is a no-op for the mock provider.
func (m *Provider) Close() error {
	return nil
}

// ShouldIncludeRawOutput returns whether raw API responses should be included.
func (m *Provider) ShouldIncludeRawOutput() bool {
	return m.includeRawOutput
}

// CalculateCost calculates cost breakdown for given token counts.
func (m *Provider) CalculateCost(inputTokens, outputTokens, cachedTokens int) types.CostInfo {
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

// generateContentSummary creates a human-readable summary from content parts
// generateContentSummary creates a human-readable text summary from content parts.
// This is used when Parts are provided but Content field is empty.
func generateContentSummary(parts []types.ContentPart) string {
	if len(parts) == 0 {
		return ""
	}

	textParts, mediaCounts := extractPartsAndCounts(parts)
	return buildSummary(textParts, mediaCounts)
}

// extractPartsAndCounts separates text parts and counts media types
func extractPartsAndCounts(parts []types.ContentPart) (textParts []string, mediaCounts map[string]int) {
	mediaCounts = make(map[string]int)

	for _, part := range parts {
		if part.Type == types.ContentTypeText && part.Text != nil {
			textParts = append(textParts, *part.Text)
		} else {
			mediaCounts[part.Type]++
		}
	}

	return textParts, mediaCounts
}

// buildSummary constructs the final summary from text parts and media counts
func buildSummary(textParts []string, mediaCounts map[string]int) string {
	summary := ""
	if len(textParts) > 0 {
		summary = textParts[0] // Use first text part as primary content
	}

	if len(mediaCounts) > 0 {
		mediaDesc := buildMediaDescription(mediaCounts)
		summary = appendMediaDescription(summary, mediaDesc)
	}

	return summary
}

// buildMediaDescription creates a description of media items from counts
func buildMediaDescription(mediaCounts map[string]int) []string {
	var mediaDesc []string

	mediaDesc = appendMediaType(mediaDesc, mediaCounts, types.ContentTypeImage, "image", "images")
	mediaDesc = appendMediaType(mediaDesc, mediaCounts, types.ContentTypeAudio, "audio", "audio files")
	mediaDesc = appendMediaType(mediaDesc, mediaCounts, types.ContentTypeVideo, "video", "videos")

	return mediaDesc
}

// appendMediaType adds a media type description if count > 0
func appendMediaType(mediaDesc []string, mediaCounts map[string]int, mediaType, singular, plural string) []string {
	if count := mediaCounts[mediaType]; count > 0 {
		if count == 1 {
			return append(mediaDesc, singular)
		}
		return append(mediaDesc, fmt.Sprintf("%d %s", count, plural))
	}
	return mediaDesc
}

// appendMediaDescription adds media description to summary
func appendMediaDescription(summary string, mediaDesc []string) string {
	if len(mediaDesc) == 0 {
		return summary
	}

	mediaStr := " [" + fmt.Sprintf("%v", mediaDesc) + "]"
	if summary == "" {
		return "Content includes: " + mediaStr
	}
	return summary + mediaStr
}
