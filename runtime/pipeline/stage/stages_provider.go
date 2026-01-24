package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultMaxRounds = 10
)

// ProviderStage implementation notes:
// - ✅ Multi-round tool execution with automatic tool result handling
// - ✅ Synchronous tool execution via toolRegistry.ExecuteAsync()
// - ⚠️ Limited async/pending tool support (no ExecutionContext for tracking)
// - TODO: Implement full async tool support with approval workflows

// ProviderStage executes LLM calls and handles tool execution.
// This is the request/response mode implementation.
type ProviderStage struct {
	BaseStage
	provider     providers.Provider
	toolRegistry *tools.Registry
	toolPolicy   *pipeline.ToolPolicy
	config       *ProviderConfig
	emitter      *events.Emitter // Optional event emitter for provider call events
}

// ProviderConfig contains configuration for the provider stage.
type ProviderConfig struct {
	MaxTokens      int
	Temperature    float32
	Seed           *int
	ResponseFormat *providers.ResponseFormat // Optional response format (JSON mode)
}

// streamingRoundParams holds parameters for a streaming round execution.
type streamingRoundParams struct {
	messages      []types.Message
	systemPrompt  string
	providerTools interface{}
	toolChoice    string
	round         int
	metadata      map[string]interface{}
}

// NewProviderStage creates a new provider stage for request/response mode.
func NewProviderStage(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
) *ProviderStage {
	return NewProviderStageWithEmitter(provider, toolRegistry, toolPolicy, config, nil)
}

// NewProviderStageWithEmitter creates a new provider stage with event emission support.
// The emitter is used to emit provider.call.started, provider.call.completed, and
// provider.call.failed events for observability and session recording.
func NewProviderStageWithEmitter(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
	emitter *events.Emitter,
) *ProviderStage {
	if config == nil {
		config = &ProviderConfig{}
	}
	return &ProviderStage{
		BaseStage:    NewBaseStage("provider", StageTypeGenerate),
		provider:     provider,
		toolRegistry: toolRegistry,
		toolPolicy:   toolPolicy,
		config:       config,
		emitter:      emitter,
	}
}

// providerInput holds accumulated input data for provider execution.
type providerInput struct {
	messages     []types.Message
	systemPrompt string
	allowedTools []string
	metadata     map[string]interface{}
}

// Process executes the LLM provider call and handles tool execution.
func (s *ProviderStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.provider == nil {
		return errors.New("provider stage: no provider configured")
	}

	accumulated := s.accumulateInput(input)

	logger.Debug("ProviderStage accumulated input",
		"messages", len(accumulated.messages),
		"allowed_tools", accumulated.allowedTools,
		"mock_scenario_id", accumulated.metadata["mock_scenario_id"],
		"mock_turn_number", accumulated.metadata["mock_turn_number"])

	return s.executeAndEmit(ctx, accumulated, output)
}

// accumulateInput collects messages and metadata from input channel.
func (s *ProviderStage) accumulateInput(input <-chan StreamElement) *providerInput {
	acc := &providerInput{
		metadata: make(map[string]interface{}),
	}

	for elem := range input {
		if elem.Message != nil {
			acc.messages = append(acc.messages, *elem.Message)
		}
		s.extractMetadata(&elem, acc)
	}

	return acc
}

// extractMetadata extracts prompt data and merges metadata from element.
func (s *ProviderStage) extractMetadata(elem *StreamElement, acc *providerInput) {
	if elem.Metadata == nil {
		return
	}
	if sp, ok := elem.Metadata["system_prompt"].(string); ok {
		acc.systemPrompt = sp
	}
	if toolsList, ok := elem.Metadata["allowed_tools"].([]string); ok {
		acc.allowedTools = toolsList
		logger.Debug("ProviderStage received allowed_tools", "tools", toolsList, "count", len(toolsList))
	}
	for k, v := range elem.Metadata {
		acc.metadata[k] = v
	}
}

// executeAndEmit runs provider execution and emits results.
func (s *ProviderStage) executeAndEmit(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) error {
	var responseMessages []types.Message
	var err error

	if s.provider.SupportsStreaming() {
		responseMessages, err = s.executeStreamingMultiRound(ctx, acc, output)
	} else {
		responseMessages, err = s.executeMultiRound(ctx, acc)
	}

	if err != nil {
		output <- NewErrorElement(err)
		return err
	}

	return s.emitResponseMessages(ctx, responseMessages, acc.metadata, output)
}

// emitResponseMessages sends response messages to output channel.
func (s *ProviderStage) emitResponseMessages(
	ctx context.Context,
	messages []types.Message,
	metadata map[string]interface{},
	output chan<- StreamElement,
) error {
	for i := range messages {
		elem := NewMessageElement(&messages[i])
		elem.Metadata = metadata

		logger.Debug("ProviderStage emitting response message",
			"role", messages[i].Role,
			"has_validator_configs", metadata["validator_configs"] != nil)

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (s *ProviderStage) executeMultiRound(
	ctx context.Context,
	acc *providerInput,
) ([]types.Message, error) {
	providerTools, toolChoice, err := s.buildProviderTools(acc.allowedTools)
	if err != nil {
		return nil, fmt.Errorf("provider stage: %w", err)
	}

	messages := acc.messages
	maxRounds := s.getMaxRounds()

	for round := 1; round <= maxRounds; round++ {
		response, hasToolCalls, err := s.executeRound(
			ctx, messages, acc.systemPrompt, providerTools, toolChoice, round, acc.metadata)
		if err != nil {
			return messages, err
		}

		messages = append(messages, response)

		if !hasToolCalls {
			break
		}

		toolResults, err := s.executeToolCalls(ctx, response.ToolCalls)
		if err != nil {
			return messages, fmt.Errorf("provider stage: tool execution failed: %w", err)
		}

		messages = append(messages, toolResults...)
		toolChoice = "auto"

		if round == maxRounds {
			return messages, fmt.Errorf("provider stage: max rounds (%d) exceeded", maxRounds)
		}
	}

	return messages, nil
}

// getMaxRounds returns the maximum number of tool call rounds.
func (s *ProviderStage) getMaxRounds() int {
	if s.toolPolicy != nil && s.toolPolicy.MaxRounds > 0 {
		return s.toolPolicy.MaxRounds
	}
	return defaultMaxRounds
}

func (s *ProviderStage) executeStreamingMultiRound(
	ctx context.Context,
	acc *providerInput,
	output chan<- StreamElement,
) ([]types.Message, error) {
	providerTools, toolChoice, err := s.buildProviderTools(acc.allowedTools)
	if err != nil {
		return nil, fmt.Errorf("provider stage: %w", err)
	}

	messages := acc.messages
	maxRounds := s.getMaxRounds()

	for round := 1; round <= maxRounds; round++ {
		params := &streamingRoundParams{
			messages:      messages,
			systemPrompt:  acc.systemPrompt,
			providerTools: providerTools,
			toolChoice:    toolChoice,
			round:         round,
			metadata:      acc.metadata,
		}
		response, hasToolCalls, err := s.executeStreamingRound(ctx, params, output)
		if err != nil {
			return messages, err
		}

		messages = append(messages, response)

		if !hasToolCalls {
			break
		}

		toolResults, err := s.executeToolCalls(ctx, response.ToolCalls)
		if err != nil {
			return messages, fmt.Errorf("provider stage: tool execution failed: %w", err)
		}

		messages = append(messages, toolResults...)
		toolChoice = "auto"

		if round == maxRounds {
			return messages, fmt.Errorf("provider stage: max rounds (%d) exceeded", maxRounds)
		}
	}

	return messages, nil
}

func (s *ProviderStage) executeRound(
	ctx context.Context,
	messages []types.Message,
	systemPrompt string,
	providerTools interface{},
	toolChoice string,
	round int,
	metadata map[string]interface{},
) (types.Message, bool, error) {
	// Build provider request
	req := providers.PredictionRequest{
		System:         systemPrompt,
		Messages:       messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		ResponseFormat: s.config.ResponseFormat,
		Metadata:       metadata,
	}

	// Count tools for event emission
	toolCount := 0
	if providerTools != nil {
		if toolDescs, ok := providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider round starting",
		"round", round,
		"messages", len(messages),
		"tools", providerTools != nil)

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStarted(s.provider.ID(), "", len(messages), toolCount)
	}

	// Call provider (with or without tools)
	startTime := time.Now()
	var resp providers.PredictionResponse
	var toolCalls []types.MessageToolCall
	var err error

	if providerTools != nil {
		// Use tool-aware provider interface
		toolProvider, ok := s.provider.(providers.ToolSupport)
		if !ok {
			return types.Message{}, false, errors.New("provider does not support tools")
		}
		resp, toolCalls, err = toolProvider.PredictWithTools(ctx, req, providerTools, toolChoice)
	} else {
		// Regular prediction
		resp, err = s.provider.Predict(ctx, req)
		toolCalls = resp.ToolCalls
	}

	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Provider call failed", "error", err, "duration", duration)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailed(s.provider.ID(), "", err, duration)
		}
		return types.Message{}, false, fmt.Errorf("provider call failed: %w", err)
	}

	// Emit provider call completed event
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
		}
		if resp.CostInfo != nil {
			completedData.InputTokens = resp.CostInfo.InputTokens
			completedData.OutputTokens = resp.CostInfo.OutputTokens
			completedData.CachedTokens = resp.CostInfo.CachedTokens
			completedData.Cost = resp.CostInfo.TotalCost
		}
		s.emitter.ProviderCallCompleted(completedData)
	}

	// Build response message with latency and cost info
	responseMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Parts:     resp.Parts,
		ToolCalls: toolCalls,
		Timestamp: timeNow(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  resp.CostInfo,
	}

	logger.Debug("Provider round completed",
		"round", round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	// Check for tool calls
	hasToolCalls := len(toolCalls) > 0

	return responseMsg, hasToolCalls, nil
}

func (s *ProviderStage) executeStreamingRound(
	ctx context.Context,
	params *streamingRoundParams,
	output chan<- StreamElement,
) (types.Message, bool, error) {
	// Build provider request
	req := providers.PredictionRequest{
		System:         params.systemPrompt,
		Messages:       params.messages,
		MaxTokens:      s.config.MaxTokens,
		Temperature:    s.config.Temperature,
		Seed:           s.config.Seed,
		Metadata:       params.metadata,
		ResponseFormat: s.config.ResponseFormat,
	}

	// Count tools for event emission
	toolCount := 0
	if params.providerTools != nil {
		if toolDescs, ok := params.providerTools.([]*providers.ToolDescriptor); ok {
			toolCount = len(toolDescs)
		}
	}

	logger.Debug("Provider streaming round starting",
		"round", params.round,
		"messages", len(params.messages),
		"tools", params.providerTools != nil)

	// Emit provider call started event
	if s.emitter != nil {
		s.emitter.ProviderCallStarted(s.provider.ID(), "", len(params.messages), toolCount)
	}

	startTime := time.Now()

	// Start the streaming request
	streamChan, err := s.startStreamingRequest(ctx, req, params.providerTools, params.toolChoice)
	if err != nil {
		duration := time.Since(startTime)
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailed(s.provider.ID(), "", err, duration)
		}
		return types.Message{}, false, err
	}

	// Process all chunks and collect response
	content, toolCalls, costInfo, err := s.processStreamChunks(ctx, streamChan, params.metadata, output)
	duration := time.Since(startTime)

	if err != nil {
		// Emit provider call failed event
		if s.emitter != nil {
			s.emitter.ProviderCallFailed(s.provider.ID(), "", err, duration)
		}
		return types.Message{}, false, err
	}

	// Emit provider call completed event with cost info from streaming response
	if s.emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider:      s.provider.ID(),
			Model:         s.provider.Model(),
			Duration:      duration,
			ToolCallCount: len(toolCalls),
		}
		// Populate token counts from cost info if available (present in final chunk)
		if costInfo != nil {
			completedData.InputTokens = costInfo.InputTokens
			completedData.OutputTokens = costInfo.OutputTokens
			completedData.CachedTokens = costInfo.CachedTokens
			completedData.Cost = costInfo.TotalCost
		}
		s.emitter.ProviderCallCompleted(completedData)
	}

	// Build final response message with latency and cost info
	responseMsg := types.Message{
		Role:      "assistant",
		Content:   content,
		ToolCalls: toolCalls,
		Timestamp: timeNow(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  costInfo,
	}

	logger.Debug("Provider streaming round completed",
		"round", params.round,
		"duration", duration,
		"latencyMs", responseMsg.LatencyMs,
		"tool_calls", len(toolCalls))

	return responseMsg, len(toolCalls) > 0, nil
}

// startStreamingRequest initiates a streaming request with or without tools.
func (s *ProviderStage) startStreamingRequest(
	ctx context.Context,
	req providers.PredictionRequest,
	providerTools interface{},
	toolChoice string,
) (<-chan providers.StreamChunk, error) {
	if providerTools != nil {
		toolProvider, ok := s.provider.(providers.ToolSupport)
		if !ok {
			return nil, errors.New("provider does not support tools")
		}
		streamChan, err := toolProvider.PredictStreamWithTools(ctx, req, providerTools, toolChoice)
		if err != nil {
			logger.Error("Provider stream failed", "error", err)
			return nil, fmt.Errorf("provider stream failed: %w", err)
		}
		return streamChan, nil
	}

	streamChan, err := s.provider.PredictStream(ctx, req)
	if err != nil {
		logger.Error("Provider stream failed", "error", err)
		return nil, fmt.Errorf("provider stream failed: %w", err)
	}
	return streamChan, nil
}

// processStreamChunks processes streaming chunks and emits elements to output.
// Returns accumulated content, tool calls, cost info (from final chunk), and any error.
func (s *ProviderStage) processStreamChunks(
	ctx context.Context,
	streamChan <-chan providers.StreamChunk,
	metadata map[string]interface{},
	output chan<- StreamElement,
) (string, []types.MessageToolCall, *types.CostInfo, error) {
	var content string
	var toolCalls []types.MessageToolCall
	var costInfo *types.CostInfo

	for chunk := range streamChan {
		if chunk.Error != nil {
			logger.Error("Stream chunk error", "error", chunk.Error)
			return "", nil, nil, fmt.Errorf("stream chunk error: %w", chunk.Error)
		}

		content = chunk.Content
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		// Capture cost info from final chunk (only present when FinishReason != nil)
		if chunk.CostInfo != nil {
			costInfo = chunk.CostInfo
		}

		if err := s.emitChunkElement(ctx, &chunk, metadata, output); err != nil {
			return "", nil, nil, err
		}
	}

	return content, toolCalls, costInfo, nil
}

// emitChunkElement creates and emits a streaming element for a chunk.
func (s *ProviderStage) emitChunkElement(
	ctx context.Context,
	chunk *providers.StreamChunk,
	metadata map[string]interface{},
	output chan<- StreamElement,
) error {
	if chunk.Delta == "" {
		return nil
	}

	elem := NewTextElement(chunk.Delta)
	elem.Timestamp = timeNow()
	elem.Priority = PriorityNormal

	for k, v := range metadata {
		elem.Metadata[k] = v
	}

	elem.Metadata["token_count"] = chunk.TokenCount
	if chunk.FinishReason != nil {
		elem.Metadata["finish_reason"] = *chunk.FinishReason
	}

	select {
	case output <- elem:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *ProviderStage) executeToolCalls(ctx context.Context, toolCalls []types.MessageToolCall) ([]types.Message, error) {
	if s.toolRegistry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	results := make([]types.Message, 0, len(toolCalls))

	for _, toolCall := range toolCalls {
		// Check if tool is blocked by policy
		if s.toolPolicy != nil && isToolBlocked(toolCall.Name, s.toolPolicy.Blocklist) {
			errMsg := fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name)
			results = append(results, types.NewToolResultMessage(types.MessageToolResult{
				ID:      toolCall.ID,
				Name:    toolCall.Name,
				Content: errMsg,
				Error:   errMsg,
			}))
			continue
		}

		// Execute tool via registry (handles both sync and async tools)
		asyncResult, err := s.toolRegistry.ExecuteAsync(toolCall.Name, toolCall.Args)
		if err != nil {
			// Tool not found or execution setup failed
			results = append(results, types.NewToolResultMessage(types.MessageToolResult{
				ID:      toolCall.ID,
				Name:    toolCall.Name,
				Content: fmt.Sprintf("Error: %v", err),
				Error:   err.Error(),
			}))
			continue
		}

		// Convert tool execution result to message
		result := s.handleToolResult(toolCall, asyncResult)
		results = append(results, types.NewToolResultMessage(result))
	}

	return results, nil
}

// handleToolResult converts tool execution result to MessageToolResult
func (s *ProviderStage) handleToolResult(
	call types.MessageToolCall,
	asyncResult *tools.ToolExecutionResult,
) types.MessageToolResult {
	switch asyncResult.Status {
	case tools.ToolStatusPending:
		// Tool requires approval - for stages we don't have ExecutionContext for tracking pending tools
		// Return a message indicating approval is needed
		pendingMsg := asyncResult.PendingInfo.Message
		if pendingMsg == "" {
			pendingMsg = fmt.Sprintf("Tool %s requires approval", call.Name)
		}
		logger.Warn("Tool requires approval in ProviderStage - pending tool support not yet implemented",
			"tool", call.Name, "call_id", call.ID)
		return types.MessageToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: pendingMsg + " (Note: Async tool approval workflows not yet implemented in stages)",
			Error:   "",
		}

	case tools.ToolStatusFailed:
		return types.MessageToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: fmt.Sprintf("Tool execution failed: %s", asyncResult.Error),
			Error:   asyncResult.Error,
		}

	case tools.ToolStatusComplete:
		// Tool completed successfully
		content := string(asyncResult.Content)

		// Try to format nicely if it's JSON
		var resultValue interface{}
		if json.Unmarshal(asyncResult.Content, &resultValue) == nil {
			content = formatToolResult(resultValue)
		}

		return types.MessageToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: content,
			Error:   "",
		}

	default:
		return types.MessageToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: fmt.Sprintf("Unknown tool status: %v", asyncResult.Status),
			Error:   fmt.Sprintf("Unknown tool status: %v", asyncResult.Status),
		}
	}
}

// isToolBlocked checks if a tool is in the blocklist
func isToolBlocked(toolName string, blocklist []string) bool {
	for _, blocked := range blocklist {
		if blocked == toolName {
			return true
		}
	}
	return false
}

// formatToolResult formats tool result for display
func formatToolResult(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		// Pretty print JSON objects
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	case []interface{}:
		// Pretty print JSON arrays
		bytes, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (s *ProviderStage) buildProviderTools(allowedTools []string) (interface{}, string, error) {
	if s.toolRegistry == nil || len(allowedTools) == 0 {
		return nil, "", nil
	}

	// Check if provider supports tools
	toolProvider, ok := s.provider.(providers.ToolSupport)
	if !ok {
		return nil, "", nil
	}

	// Build tool descriptors from registry
	descriptors := make([]*providers.ToolDescriptor, 0, len(allowedTools))
	for _, toolName := range allowedTools {
		tool, err := s.toolRegistry.GetTool(toolName)
		if err != nil {
			logger.Warn("Tool not found in registry", "tool", toolName, "error", err)
			continue
		}

		descriptor := &providers.ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		}
		descriptors = append(descriptors, descriptor)
	}

	// Build provider-specific tools
	providerTools, err := toolProvider.BuildTooling(descriptors)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build tools: %w", err)
	}

	// Determine tool choice from policy
	toolChoice := "auto" // default
	if s.toolPolicy != nil && s.toolPolicy.ToolChoice != "" {
		toolChoice = s.toolPolicy.ToolChoice
	}

	return providerTools, toolChoice, nil
}
