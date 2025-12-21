package mock

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ToolProvider extends MockProvider to support tool/function calling and duplex streaming.
// It implements the ToolSupport interface to enable tool call simulation
// while maintaining compatibility with the existing MockProvider API.
// By embedding StreamingProvider, it also supports StreamInputSupport for duplex scenarios.
type ToolProvider struct {
	*StreamingProvider
}

// NewToolProvider creates a new mock provider with tool support and duplex streaming.
// This uses default in-memory responses for backward compatibility.
func NewToolProvider(id, model string, includeRawOutput bool, additionalConfig map[string]interface{}) *ToolProvider {
	var streamingProvider *StreamingProvider

	if additionalConfig != nil {
		if mockConfigPath, ok := additionalConfig["mock_config"].(string); ok && mockConfigPath != "" {
			// Create file-based repository and use ToolProvider for tool call simulation
			repository, err := NewFileMockRepository(mockConfigPath)
			if err != nil {
				logger.Warn("failed to load mock config from %s: %w", mockConfigPath, err)
				streamingProvider = NewStreamingProvider(id, model, includeRawOutput)
			} else {
				streamingProvider = NewStreamingProviderWithRepository(id, model, includeRawOutput, repository)
			}
		} else {
			streamingProvider = NewStreamingProvider(id, model, includeRawOutput)
		}

		// Configure auto-respond for duplex streaming tests
		// Handle both bool and string "true" from YAML parsing
		autoRespond := false
		switch v := additionalConfig["auto_respond"].(type) {
		case bool:
			autoRespond = v
		case string:
			autoRespond = v == "true"
		}

		if autoRespond {
			responseText := DefaultMockStreamingResponse
			if text, ok := additionalConfig["response_text"].(string); ok && text != "" {
				responseText = text
			}
			logger.Info("Mock provider: auto-respond enabled", "response_text", responseText)
			streamingProvider.WithAutoRespond(responseText)
		}

		// Configure simulation behaviors for testing duplex failure scenarios
		// YAML parses integers as float64, so handle both types
		if interruptTurn := getIntFromConfig(additionalConfig, "interrupt_on_turn"); interruptTurn > 0 {
			logger.Info("Mock provider: interrupt simulation enabled", "turn", interruptTurn)
			streamingProvider.WithInterruptOnTurn(interruptTurn)
		}
		if closeTurns := getIntFromConfig(additionalConfig, "close_after_turns"); closeTurns > 0 {
			closeNoResponse := getBoolFromConfig(additionalConfig, "close_no_response")
			logger.Info("Mock provider: session closure simulation enabled",
				"after_turns", closeTurns,
				"no_response", closeNoResponse)
			streamingProvider.WithCloseAfterTurns(closeTurns, closeNoResponse)
		}
	} else {
		streamingProvider = NewStreamingProvider(id, model, includeRawOutput)
	}

	return &ToolProvider{
		StreamingProvider: streamingProvider,
	}
}

// NewToolProviderWithRepository creates a mock provider with tool support and duplex streaming
// using a custom response repository for advanced scenarios.
func NewToolProviderWithRepository(id, model string, includeRawOutput bool, repo ResponseRepository) *ToolProvider {
	return &ToolProvider{
		StreamingProvider: NewStreamingProviderWithRepository(id, model, includeRawOutput, repo),
	}
}

// BuildTooling implements the ToolSupport interface.
// For mock providers, we just return the tools as-is since we don't need
// to transform them into a provider-specific format.
func (m *ToolProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (interface{}, error) {
	logger.Debug("ToolProvider BuildTooling",
		"provider_id", m.id,
		"tool_count", len(descriptors))

	// For mocking purposes, we return the descriptors unchanged
	return descriptors, nil
}

// PredictWithTools implements the ToolSupport interface.
// This method handles the initial predict request with tools available,
// potentially returning tool calls based on the mock configuration.
func (m *ToolProvider) PredictWithTools(ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string) (providers.PredictionResponse, []types.MessageToolCall, error) {
	logger.Debug("ToolProvider PredictWithTools",
		"provider_id", m.id,
		"tool_choice", toolChoice,
		"message_count", len(req.Messages))

	// Detect turn number based on conversation history and tool results
	turnNumber := m.detectTurnFromConversation(req)

	// Get mock turn from repository
	params := ResponseParams{
		ScenarioID: m.getScenarioID(req),
		TurnNumber: turnNumber,
		ProviderID: m.id,
		ModelName:  m.model,
		PersonaID:  m.getPersonaID(req),
		ArenaRole:  m.getArenaRole(req),
	}

	logger.Debug("ToolProvider PredictWithTools using turn",
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

		logger.Debug("ToolProvider returning tool calls",
			"provider_id", m.id,
			"tool_call_count", len(toolCalls))

		return providers.PredictionResponse{
			Content:   mockTurn.Content,
			ToolCalls: toolCalls,
			CostInfo:  &costInfo,
		}, toolCalls, nil
	}

	// Otherwise return normal text response
	logger.Debug("ToolProvider returning text response",
		"provider_id", m.id,
		"response_length", len(mockTurn.Content))

	return providers.PredictionResponse{
		Content:  mockTurn.Content,
		CostInfo: &costInfo,
	}, nil, nil
}

// generateMockCostInfo creates cost information for mock responses.
func (m *ToolProvider) generateMockCostInfo(inputTokens, outputTokens int) types.CostInfo {
	return types.CostInfo{
		InputTokens:   inputTokens,
		OutputTokens:  outputTokens,
		InputCostUSD:  float64(inputTokens) * 0.00001,  // $0.01 per 1K tokens
		OutputCostUSD: float64(outputTokens) * 0.00001, // $0.01 per 1K tokens
		TotalCost:     float64(inputTokens+outputTokens) * 0.00001,
	}
}

// calculateInputTokens estimates input tokens from messages (rough approximation).
func (m *ToolProvider) calculateInputTokens(messages []types.Message) int {
	tokenCount := 0
	for i := range messages {
		tokenCount += len(messages[i].Content) / 4 // Rough approximation: ~4 chars per token
	}
	if tokenCount == 0 {
		tokenCount = 10
	}
	return tokenCount
}

// calculateOutputTokens estimates output tokens from response text (rough approximation).
func (m *ToolProvider) calculateOutputTokens(responseText string) int {
	tokens := len(responseText) / 4
	if tokens == 0 {
		tokens = 20
	}
	return tokens
}

// detectTurnFromConversation analyzes the conversation history to determine the current turn number
func (m *ToolProvider) detectTurnFromConversation(req providers.PredictionRequest) int {
	// Start with base turn number from metadata (if provided)
	baseTurnNumber := 0
	if req.Metadata != nil {
		if turn, ok := req.Metadata["mock_turn_number"].(int); ok {
			baseTurnNumber = turn
		}
	}

	// Also derive from assistant messages (each assistant reply advances a turn)
	assistantMsgs := 0
	for i := range req.Messages {
		if req.Messages[i].Role == "assistant" {
			assistantMsgs++
		}
	}
	if assistantMsgs > baseTurnNumber {
		baseTurnNumber = assistantMsgs
	}
	if baseTurnNumber < 1 {
		baseTurnNumber = 1
	}

	// Count tool result messages to detect continuation
	toolResultCount := 0
	for i := range req.Messages {
		if req.Messages[i].Role == "tool" {
			toolResultCount++
		}
	}

	adjustedTurnNumber := baseTurnNumber
	if toolResultCount > 0 {
		adjustedTurnNumber = baseTurnNumber + 1
	}

	logger.Debug("ToolProvider turn detection",
		"provider_id", m.id,
		"base_turn", baseTurnNumber,
		"tool_result_count", toolResultCount,
		"adjusted_turn", adjustedTurnNumber)

	return adjustedTurnNumber
}

// getScenarioID extracts the scenario ID from request metadata
func (m *ToolProvider) getScenarioID(req providers.PredictionRequest) string {
	if req.Metadata != nil {
		if sid, ok := req.Metadata["mock_scenario_id"].(string); ok {
			return sid
		}
	}
	return ""
}

// getPersonaID extracts the persona ID from request metadata for selfplay user turns
func (m *ToolProvider) getPersonaID(req providers.PredictionRequest) string {
	if req.Metadata != nil {
		if pid, ok := req.Metadata["mock_persona_id"].(string); ok {
			return pid
		}
	}
	return ""
}

// getArenaRole extracts the arena role from request metadata
func (m *ToolProvider) getArenaRole(req providers.PredictionRequest) string {
	if req.Metadata != nil {
		if role, ok := req.Metadata["arena_role"].(string); ok {
			return role
		}
	}
	return ""
}

// PredictStreamWithTools performs a streaming predict request with tool support.
// For mock providers, this delegates to PredictWithTools and wraps the response in chunks.
func (m *ToolProvider) PredictStreamWithTools(
	ctx context.Context,
	req providers.PredictionRequest,
	tools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	// Get the non-streaming response
	resp, toolCalls, err := m.PredictWithTools(ctx, req, tools, toolChoice)
	if err != nil {
		return nil, err
	}

	// Create a channel and send the response as chunks
	outChan := make(chan providers.StreamChunk)

	go func() {
		defer close(outChan)

		// Send content in chunks (simulate streaming)
		content := resp.Content
		chunkSize := 20 // Characters per chunk

		for i := 0; i < len(content); i += chunkSize {
			end := i + chunkSize
			if end > len(content) {
				end = len(content)
			}
			outChan <- providers.StreamChunk{
				Delta:   content[i:end],
				Content: content[:end],
			}
		}

		// Send final chunk with tool calls and finish reason
		finishReason := "stop"
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}
		outChan <- providers.StreamChunk{
			Content:      content,
			ToolCalls:    toolCalls,
			FinishReason: &finishReason,
			CostInfo:     resp.CostInfo,
		}
	}()

	return outChan, nil
}

// getIntFromConfig extracts an integer from additionalConfig, handling YAML's float64 parsing.
func getIntFromConfig(config map[string]interface{}, key string) int {
	if val, ok := config[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		}
	}
	return 0
}

// getBoolFromConfig extracts a boolean from additionalConfig.
func getBoolFromConfig(config map[string]interface{}, key string) bool {
	if val, ok := config[key]; ok {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			return v == "true"
		}
	}
	return false
}
