package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ProviderMiddlewareConfig contains configuration for the provider middleware
type ProviderMiddlewareConfig struct {
	MaxTokens    int
	Temperature  float32
	Seed         *int
	DisableTrace bool // Disable execution tracing (default: false = tracing enabled)
}

// toolingConfig bundles tool-related parameters for round execution
type toolingConfig struct {
	providerTools interface{}
	toolChoice    string
	registry      *tools.Registry
	policy        *pipeline.ToolPolicy
}

// providerMiddleware executes LLM calls and handles tool execution via the ToolRegistry.
type providerMiddleware struct {
	provider     providers.Provider
	toolRegistry *tools.Registry
	toolPolicy   *pipeline.ToolPolicy
	config       *ProviderMiddlewareConfig
}

// ProviderMiddleware executes LLM calls and handles tool execution via the ToolRegistry.
// It supports multi-round execution when tools are involved.
// In streaming mode, it forwards chunks to execCtx.StreamOutput.
//
// The provider, toolRegistry, toolPolicy, and configuration are provided at construction time,
// simplifying the ExecutionContext and avoiding the need to pass Config around.
func ProviderMiddleware(provider providers.Provider, toolRegistry *tools.Registry, toolPolicy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) pipeline.Middleware {
	return &providerMiddleware{
		provider:     provider,
		toolRegistry: toolRegistry,
		toolPolicy:   toolPolicy,
		config:       config,
	}
}

func (m *providerMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	if m.provider == nil {
		return errors.New("provider middleware: no provider configured")
	}

	// Execute provider logic (streaming or non-streaming)
	var err error
	if execCtx.StreamMode {
		err = executeStreaming(execCtx, m.provider, m.toolRegistry, m.toolPolicy, m.config)
	} else {
		err = executeNonStreaming(execCtx, m.provider, m.toolRegistry, m.toolPolicy, m.config)
	}

	// Always call next() to allow state to be saved, even if there was an error
	// This ensures we can inspect the conversation history when debugging
	nextErr := next()

	// Return the provider error if there was one, otherwise the next() error
	if err != nil {
		return err
	}
	return nextErr
} // executeNonStreaming handles non-streaming execution (original logic)
func executeNonStreaming(execCtx *pipeline.ExecutionContext, provider providers.Provider, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) error {
	// Build tools for provider (if available)
	providerTools, toolChoice, err := buildProviderTooling(provider, toolRegistry, execCtx, policy)
	if err != nil {
		return fmt.Errorf("provider middleware: %w", err)
	}

	// Bundle tool-related configuration
	tooling := toolingConfig{
		providerTools: providerTools,
		toolChoice:    toolChoice,
		registry:      toolRegistry,
		policy:        policy,
	}

	// Multi-round execution loop for tool calls
	round := 0

	for {
		round++

		// Check max rounds policy with fallback to default
		if err := checkRoundLimit(round, policy); err != nil {
			return err
		}

		// Execute one non-streaming round
		hasMoreRounds, err := executeNonStreamingRound(execCtx, provider, round, tooling, config)
		if err != nil {
			return err
		}
		if !hasMoreRounds {
			break
		}

		// Continue loop for next round (provider will see tool results)
	}

	return nil
}

// executeNonStreamingRound executes a single round of non-streaming execution
func executeNonStreamingRound(execCtx *pipeline.ExecutionContext, provider providers.Provider, round int, tooling toolingConfig, config *ProviderMiddlewareConfig) (bool, error) {
	// Build provider request
	req := buildProviderRequest(execCtx, config)

	// Determine tool choice for this round
	currentToolChoice := tooling.toolChoice
	if round > 1 {
		currentToolChoice = "auto"
	}

	// Call provider and get response
	startTime := time.Now()
	resp, callErr := callProviderNonStreaming(execCtx, provider, req, tooling.providerTools, currentToolChoice)
	duration := time.Since(startTime)

	if callErr != nil {
		return false, fmt.Errorf("provider middleware: generation failed: %w", callErr)
	}

	// Process response and update context
	return processNonStreamingResponse(execCtx, &resp, startTime, duration, tooling.registry, tooling.policy, config)
}

// callProviderNonStreaming calls the appropriate provider method based on capabilities
func callProviderNonStreaming(execCtx *pipeline.ExecutionContext, provider providers.Provider, req providers.ChatRequest, providerTools interface{}, toolChoice string) (providers.ChatResponse, error) {
	ctx := execCtx.Context

	// Log debugging info
	logger.Debug("ProviderMiddleware execution decision",
		"provider_type", fmt.Sprintf("%T", provider),
		"provider_tools_nil", providerTools == nil,
		"allowed_tools_count", len(execCtx.AllowedTools),
		"tool_choice", toolChoice)

	if providerTools != nil {
		return callProviderWithTools(ctx, provider, req, providerTools, toolChoice)
	}

	return callProviderWithoutTools(ctx, provider, req)
}

// callProviderWithTools calls provider with tool support
func callProviderWithTools(ctx context.Context, provider providers.Provider, req providers.ChatRequest, providerTools interface{}, toolChoice string) (providers.ChatResponse, error) {
	logger.Debug("Using ChatWithTools path", "provider_type", fmt.Sprintf("%T", provider))
	toolSupport := provider.(providers.ToolSupport)
	resp, toolCalls, err := toolSupport.ChatWithTools(ctx, req, providerTools, toolChoice)
	if err != nil {
		return resp, err
	}
	// Merge tool calls into response
	resp.ToolCalls = toolCalls
	return resp, nil
}

// callProviderWithoutTools calls provider without tools, handling multimodal if needed
func callProviderWithoutTools(ctx context.Context, provider providers.Provider, req providers.ChatRequest) (providers.ChatResponse, error) {
	isMultimodal := isRequestMultimodal(req)

	if isMultimodal {
		if multimodalProvider, ok := provider.(providers.MultimodalSupport); ok {
			logger.Debug("Using ChatMultimodal path", "provider_type", fmt.Sprintf("%T", provider))
			return multimodalProvider.ChatMultimodal(ctx, req)
		}
		logger.Debug("Using regular Chat path for multimodal request", "provider_type", fmt.Sprintf("%T", provider), "reason", "provider does not support MultimodalSupport interface")
	} else {
		logger.Debug("Using regular Chat path", "provider_type", fmt.Sprintf("%T", provider), "reason", "providerTools is nil")
	}

	return provider.Chat(ctx, req)
}

// processNonStreamingResponse processes the provider response and updates execution context
func processNonStreamingResponse(execCtx *pipeline.ExecutionContext, resp *providers.ChatResponse, startTime time.Time, duration time.Duration, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) (bool, error) {
	// Store raw response
	execCtx.RawResponse = *resp

	// Accumulate costs
	accumulateCost(execCtx, resp.CostInfo)

	// Convert provider response to pipeline response
	pipelineResp := convertProviderResponse(resp)
	execCtx.Response = &pipelineResp

	// Record LLM call in trace before adding message
	execCtx.RecordLLMCall(config != nil && config.DisableTrace, &pipelineResp, startTime, duration, resp.CostInfo, convertToolCalls(resp.ToolCalls))

	// Add assistant message to conversation history
	assistantMsg := createAssistantMessage(resp.Content, resp.Parts, convertToolCalls(resp.ToolCalls), resp.CostInfo, resp.Latency)
	execCtx.Messages = append(execCtx.Messages, assistantMsg)

	// Process tool calls (if any) and determine if we need another round
	return processToolCallRound(execCtx, toolRegistry, policy, resp.ToolCalls)
}

// executeStreaming handles streaming execution with tool support
func executeStreaming(execCtx *pipeline.ExecutionContext, provider providers.Provider, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) error {
	// Check if provider supports streaming
	if !provider.SupportsStreaming() {
		return errors.New("provider middleware: provider does not support streaming")
	}

	// Multi-round execution loop for tool calls
	round := 0

	for {
		round++

		// Check max rounds policy with fallback to default
		if err := checkRoundLimit(round, policy); err != nil {
			return err
		}

		// Execute one streaming round
		hasMoreRounds, err := executeStreamingRound(execCtx, provider, toolRegistry, policy, config)
		if err != nil {
			return err
		}
		if !hasMoreRounds {
			break
		}

		// Continue loop for next round (will stream the final response after tools)
	}

	return nil
}

// executeStreamingRound executes a single round of streaming
func executeStreamingRound(execCtx *pipeline.ExecutionContext, provider providers.Provider, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) (bool, error) {
	// Build provider request
	req := buildProviderRequest(execCtx, config)

	// Call provider streaming
	startTime := time.Now()
	stream, err := provider.ChatStream(execCtx.Context, req)
	if err != nil {
		return false, fmt.Errorf("provider middleware: streaming failed: %w", err)
	}

	// Process stream chunks
	streamResult, err := processStreamChunks(execCtx, stream)
	if err != nil {
		return false, err
	}

	duration := time.Since(startTime)

	// Handle interrupted streams
	if streamResult.interrupted {
		return false, handleStreamInterruption(execCtx, provider, req, streamResult, duration, config)
	}

	// Handle completed streams
	return handleStreamCompletion(execCtx, streamResult, duration, toolRegistry, policy, config)
}

// streamProcessResult holds the results of processing stream chunks
type streamProcessResult struct {
	finalContent string
	toolCalls    []types.MessageToolCall
	interrupted  bool
	finalChunk   *providers.StreamChunk
}

// processStreamChunks processes all chunks from a stream
func processStreamChunks(execCtx *pipeline.ExecutionContext, stream <-chan providers.StreamChunk) (*streamProcessResult, error) {
	result := &streamProcessResult{}

	for chunk := range stream {
		if chunk.Error != nil {
			// Forward error chunk to output
			if execCtx.StreamOutput != nil {
				execCtx.EmitStreamChunk(chunk)
			}
			return nil, fmt.Errorf("provider middleware: stream error: %w", chunk.Error)
		}

		// Track final content and tool calls
		result.finalContent = chunk.Content
		result.toolCalls = chunk.ToolCalls

		// Track the final chunk (for cost info)
		if chunk.FinishReason != nil {
			finalChunk := chunk
			result.finalChunk = &finalChunk
		}

		// Forward chunk to output (will run middleware StreamChunk hooks via EmitStreamChunk)
		if execCtx.StreamOutput != nil {
			if !execCtx.EmitStreamChunk(chunk) {
				// Stream was interrupted (e.g., by validation failure)
				result.interrupted = true
				break
			}
		}
	}

	return result, nil
}

// handleStreamInterruption handles interrupted streams by saving partial messages
func handleStreamInterruption(execCtx *pipeline.ExecutionContext, provider providers.Provider, req providers.ChatRequest, result *streamProcessResult, duration time.Duration, config *ProviderMiddlewareConfig) error {
	// Calculate approximate cost for interrupted stream
	approxCost := calculateApproximateCost(provider, req, result.finalContent)

	// Build partial pipeline response
	pipelineResp := pipeline.Response{
		Content: result.finalContent,
	}

	// Add cost info to pipeline response metadata
	if approxCost != nil {
		pipelineResp.Metadata = pipeline.ResponseMetadata{
			TokensInput:  approxCost.InputTokens,
			TokensOutput: approxCost.OutputTokens,
			Cost:         approxCost.TotalCost,
		}
	}

	execCtx.Response = &pipelineResp

	// Record LLM call in trace before adding message
	execCtx.RecordLLMCall(config != nil && config.DisableTrace, &pipelineResp, time.Now().Add(-duration), duration, nil, convertToolCalls(result.toolCalls))

	// Create assistant message with approximate cost
	assistantMsg := createAssistantMessage(result.finalContent, nil, nil, approxCost, duration)
	assistantMsg.Meta = map[string]interface{}{
		"raw_response": map[string]interface{}{
			"cost_estimate_type": "approximate",
		},
	}
	execCtx.Messages = append(execCtx.Messages, assistantMsg)

	return nil
}

// handleStreamCompletion handles completed streams and determines if more rounds are needed
func handleStreamCompletion(execCtx *pipeline.ExecutionContext, result *streamProcessResult, duration time.Duration, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, config *ProviderMiddlewareConfig) (bool, error) {
	// Build pipeline response from final chunk
	pipelineResp := pipeline.Response{
		Content: result.finalContent,
	}

	// Add cost info from final chunk if available
	var costInfo *types.CostInfo
	if result.finalChunk != nil && result.finalChunk.CostInfo != nil {
		costInfo = result.finalChunk.CostInfo
		pipelineResp.Metadata = pipeline.ResponseMetadata{
			TokensInput:  costInfo.InputTokens,
			TokensOutput: costInfo.OutputTokens,
			Cost:         costInfo.TotalCost,
		}
	}

	execCtx.Response = &pipelineResp
	startTime := time.Now().Add(-duration)

	// Check if there were tool calls
	if len(result.toolCalls) == 0 {
		// No tools to execute, we're done
		execCtx.RecordLLMCall(config != nil && config.DisableTrace, &pipelineResp, startTime, duration, costInfo, convertToolCalls(result.toolCalls))

		assistantMsg := createAssistantMessage(result.finalContent, nil, nil, costInfo, duration)
		if costInfo != nil {
			assistantMsg.Meta = map[string]interface{}{
				"raw_response": map[string]interface{}{
					"cost_estimate_type": "exact",
				},
			}
		}
		execCtx.Messages = append(execCtx.Messages, assistantMsg)

		return false, nil // No more rounds needed
	}

	// Record LLM call in trace and add assistant message with tool calls
	execCtx.RecordLLMCall(config != nil && config.DisableTrace, &pipelineResp, startTime, duration, costInfo, convertToolCalls(result.toolCalls))

	assistantMsg := createAssistantMessage(result.finalContent, nil, convertToolCalls(result.toolCalls), costInfo, duration)
	if costInfo != nil {
		assistantMsg.Meta = map[string]interface{}{
			"raw_response": map[string]interface{}{
				"cost_estimate_type": "exact",
			},
		}
	}
	execCtx.Messages = append(execCtx.Messages, assistantMsg)

	// Process tool calls and determine if we need another round
	return processToolCallRound(execCtx, toolRegistry, policy, result.toolCalls)
}

// buildProviderTooling extracts the duplicated tool setup logic
func buildProviderTooling(provider providers.Provider, toolRegistry *tools.Registry, execCtx *pipeline.ExecutionContext, policy *pipeline.ToolPolicy) (interface{}, string, error) {
	// Early returns for cases where we don't need tools
	if toolRegistry == nil || len(execCtx.AllowedTools) == 0 {
		return nil, "", nil
	}

	// Get only the allowed tools from the registry
	allowedToolDescriptors, err := toolRegistry.GetToolsByNames(execCtx.AllowedTools)
	// If some tools are not found, silently continue without tool support
	if err != nil || len(allowedToolDescriptors) == 0 {
		return nil, "", nil
	}

	// Check if provider supports tools
	toolSupport, ok := provider.(providers.ToolSupport)
	if !ok {
		return nil, "", nil
	}

	// Convert tools.ToolDescriptor to providers.ToolDescriptor
	var providerToolDescriptors []*providers.ToolDescriptor
	for _, tool := range allowedToolDescriptors {
		providerToolDescriptors = append(providerToolDescriptors, &providers.ToolDescriptor{
			Name:         tool.Name,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
		})
	}

	providerTools, err := toolSupport.BuildTooling(providerToolDescriptors)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build tools: %w", err)
	}

	// Set tool choice from policy, default to auto
	toolChoice := "auto"
	if policy != nil && policy.ToolChoice != "" {
		toolChoice = policy.ToolChoice
	}

	return providerTools, toolChoice, nil
}

// createAssistantMessage creates an assistant message with consistent fields
func createAssistantMessage(content string, parts []types.ContentPart, toolCalls []types.MessageToolCall, costInfo *types.CostInfo, duration time.Duration) types.Message {
	msg := types.Message{
		Role:      "assistant",
		Content:   content,
		Parts:     parts,
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
		LatencyMs: duration.Milliseconds(),
		CostInfo:  costInfo,
		Source:    "pipeline",
	}
	return msg
}

// accumulateCost adds cost info to the execution context's cumulative cost
func accumulateCost(execCtx *pipeline.ExecutionContext, costInfo *types.CostInfo) {
	if costInfo == nil {
		return
	}

	execCtx.CostInfo.InputTokens += costInfo.InputTokens
	execCtx.CostInfo.OutputTokens += costInfo.OutputTokens
	execCtx.CostInfo.CachedTokens += costInfo.CachedTokens
	execCtx.CostInfo.InputCostUSD += costInfo.InputCostUSD
	execCtx.CostInfo.OutputCostUSD += costInfo.OutputCostUSD
	execCtx.CostInfo.CachedCostUSD += costInfo.CachedCostUSD
	execCtx.CostInfo.TotalCost += costInfo.TotalCost
}

// checkRoundLimit validates that the current round doesn't exceed policy limits
func checkRoundLimit(round int, policy *pipeline.ToolPolicy) error {
	const defaultMaxRounds = 10
	maxRounds := defaultMaxRounds
	if policy != nil && policy.MaxRounds > 0 {
		maxRounds = policy.MaxRounds
	}

	if round > maxRounds {
		return fmt.Errorf("provider middleware: exceeded max rounds (%d)", maxRounds)
	}
	return nil
}

// checkToolCallLimit validates that tool calls don't exceed policy limits
func checkToolCallLimit(numCalls int, policy *pipeline.ToolPolicy) error {
	if policy != nil && policy.MaxToolCallsPerTurn > 0 && numCalls > policy.MaxToolCallsPerTurn {
		return fmt.Errorf("provider middleware: exceeded max tool calls per turn (%d)", policy.MaxToolCallsPerTurn)
	}
	return nil
}

// addToolResultMessages adds tool result messages to the conversation history
func addToolResultMessages(execCtx *pipeline.ExecutionContext, toolResults []types.MessageToolResult) {
	for _, result := range toolResults {
		toolMsg := types.Message{
			Role:      "tool",
			Content:   result.Content,
			Timestamp: time.Now(),
			ToolResult: &types.MessageToolResult{
				ID:        result.ID,
				Name:      result.Name,
				Content:   result.Content,
				Error:     result.Error,
				LatencyMs: result.LatencyMs,
			},
			Source: "pipeline",
		}
		execCtx.Messages = append(execCtx.Messages, toolMsg)
	}
}

// processToolCallRound executes tool calls and returns whether more rounds are needed
// Returns (hasMoreRounds, error)
func processToolCallRound(execCtx *pipeline.ExecutionContext, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, toolCalls []types.MessageToolCall) (bool, error) {
	// No tool calls means we're done
	if len(toolCalls) == 0 {
		return false, nil
	}

	// Check max calls per turn policy
	if err := checkToolCallLimit(len(toolCalls), policy); err != nil {
		return false, err
	}

	// Execute tool calls via ToolRegistry
	toolResults, err := executeToolCalls(execCtx, toolRegistry, policy, toolCalls)
	if err != nil {
		return false, fmt.Errorf("provider middleware: tool execution failed: %w", err)
	}

	// Add tool results to context
	execCtx.ToolResults = append(execCtx.ToolResults, toolResults...)

	// Add tool result messages to conversation history
	addToolResultMessages(execCtx, toolResults)

	// Check if any tools are pending - if so, stop execution
	if execCtx.HasPendingToolCalls() {
		return false, fmt.Errorf("execution paused: pending tool calls require approval")
	}

	// Continue for next round
	return true, nil
}

// buildProviderRequest constructs the provider request from ExecutionContext
func buildProviderRequest(execCtx *pipeline.ExecutionContext, config *ProviderMiddlewareConfig) providers.ChatRequest {
	// Convert pipeline messages to provider messages
	providerMsgs := make([]types.Message, 0, len(execCtx.Messages))

	// Skip system message since it goes in ChatRequest.System
	for _, msg := range execCtx.Messages {
		if msg.Role == "system" {
			continue
		}

		providerMsg := types.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Parts:     msg.Parts,     // Preserve multimodal parts
			ToolCalls: msg.ToolCalls, // Already []types.MessageToolCall
		}

		// Handle tool result messages (convert to legacy format)
		if msg.Role == "tool" && msg.ToolResult != nil {
			providerMsg.ToolResult = msg.ToolResult
		}

		providerMsgs = append(providerMsgs, providerMsg)
	}

	req := providers.ChatRequest{
		System:   execCtx.Prompt, // Use the assembled prompt from TemplateMiddleware
		Messages: providerMsgs,
	}

	// Add config if provided
	if config != nil {
		req.MaxTokens = config.MaxTokens
		req.Temperature = config.Temperature
		req.Seed = config.Seed
	}

	// Copy all ExecutionContext.Metadata to ChatRequest.Metadata by value
	if len(execCtx.Metadata) > 0 {
		req.Metadata = make(map[string]interface{})
		for key, value := range execCtx.Metadata {
			req.Metadata[key] = value
		}
	}

	return req
}

// executeToolCalls routes tool calls through ToolRegistry and tracks pending tools in ExecutionContext
func executeToolCalls(execCtx *pipeline.ExecutionContext, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, toolCalls []types.MessageToolCall) ([]types.MessageToolResult, error) {
	if toolRegistry == nil {
		return nil, errors.New("no tool registry configured")
	}

	var results []types.MessageToolResult
	var pendingToolInfos []interface{} // Store pending tool info for metadata

	for _, call := range toolCalls {
		result, pendingInfo, err := executeToolCall(execCtx, toolRegistry, policy, call)
		if err != nil {
			return nil, err
		}

		results = append(results, result)

		if pendingInfo != nil {
			pendingToolInfos = append(pendingToolInfos, pendingInfo)
		}
	}

	// Store pending tool infos in ExecutionContext metadata for middleware to use
	storePendingToolInfos(execCtx, pendingToolInfos)

	return results, nil
}

// executeToolCall executes a single tool call and returns the result
func executeToolCall(execCtx *pipeline.ExecutionContext, toolRegistry *tools.Registry, policy *pipeline.ToolPolicy, call types.MessageToolCall) (types.MessageToolResult, interface{}, error) {
	// Check blocklist
	if policy != nil && isToolBlocked(call.Name, policy.Blocklist) {
		return createBlockedToolResult(call), nil, nil
	}

	// Try async execution first (handles both async and sync tools)
	asyncResult, err := toolRegistry.ExecuteAsync(call.Name, call.Args)
	if err != nil {
		return createErrorToolResult(call, err), nil, nil
	}

	// Handle based on status
	return handleAsyncToolResult(execCtx, call, asyncResult)
}

// createBlockedToolResult creates a result for blocked tools
func createBlockedToolResult(call types.MessageToolCall) types.MessageToolResult {
	return types.MessageToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: fmt.Sprintf("Tool %s is blocked by policy", call.Name),
		Error:   fmt.Sprintf("Tool %s is blocked by policy", call.Name),
	}
}

// createErrorToolResult creates a result for failed tool execution
func createErrorToolResult(call types.MessageToolCall, err error) types.MessageToolResult {
	return types.MessageToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: fmt.Sprintf("Error: %v", err),
		Error:   err.Error(),
	}
}

// handleAsyncToolResult handles the result of async tool execution
func handleAsyncToolResult(execCtx *pipeline.ExecutionContext, call types.MessageToolCall, asyncResult *tools.ToolExecutionResult) (types.MessageToolResult, interface{}, error) {
	switch asyncResult.Status {
	case tools.ToolStatusPending:
		return handlePendingTool(execCtx, call, asyncResult)
	case tools.ToolStatusFailed:
		return handleFailedTool(call, asyncResult), nil, nil
	case tools.ToolStatusComplete:
		return handleCompleteTool(call, asyncResult), nil, nil
	default:
		return createErrorToolResult(call, fmt.Errorf("unknown tool status: %v", asyncResult.Status)), nil, nil
	}
}

// handlePendingTool handles pending tool execution
func handlePendingTool(execCtx *pipeline.ExecutionContext, call types.MessageToolCall, asyncResult *tools.ToolExecutionResult) (types.MessageToolResult, interface{}, error) {
	// Tool is pending - add to ExecutionContext and store metadata
	pendingMsg := asyncResult.PendingInfo.Message
	if pendingMsg == "" {
		pendingMsg = fmt.Sprintf("Tool %s is pending approval", call.Name)
	}

	// Add to pending tool calls in ExecutionContext
	execCtx.AddPendingToolCall(call)

	result := types.MessageToolResult{
		ID:        call.ID,
		Name:      call.Name,
		Content:   pendingMsg,
		Error:     "", // Not an error, just pending
		LatencyMs: 0,
	}

	return result, asyncResult.PendingInfo, nil
}

// handleFailedTool handles failed tool execution
func handleFailedTool(call types.MessageToolCall, asyncResult *tools.ToolExecutionResult) types.MessageToolResult {
	return types.MessageToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: fmt.Sprintf("Tool execution failed: %s", asyncResult.Error),
		Error:   asyncResult.Error,
	}
}

// handleCompleteTool handles completed tool execution
func handleCompleteTool(call types.MessageToolCall, asyncResult *tools.ToolExecutionResult) types.MessageToolResult {
	// Tool completed successfully - decode content
	var resultValue interface{}
	if err := json.Unmarshal(asyncResult.Content, &resultValue); err != nil {
		return types.MessageToolResult{
			ID:      call.ID,
			Name:    call.Name,
			Content: string(asyncResult.Content),
		}
	}

	content := formatToolResult(resultValue)

	return types.MessageToolResult{
		ID:      call.ID,
		Name:    call.Name,
		Content: content,
	}
}

// formatToolResult formats the tool result content
func formatToolResult(resultValue interface{}) string {
	switch v := resultValue.(type) {
	case string:
		return v
	case float64, int, int64, bool, nil:
		return fmt.Sprintf("%v", v)
	default:
		jsonBytes, _ := json.Marshal(v)
		return string(jsonBytes)
	}
}

// storePendingToolInfos stores pending tool infos in ExecutionContext metadata
func storePendingToolInfos(execCtx *pipeline.ExecutionContext, pendingToolInfos []interface{}) {
	if len(pendingToolInfos) > 0 {
		if execCtx.Metadata == nil {
			execCtx.Metadata = make(map[string]interface{})
		}
		execCtx.Metadata["pending_tools"] = pendingToolInfos
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

// convertProviderResponse converts providers.ChatResponse to pipeline.Response
func convertProviderResponse(resp *providers.ChatResponse) pipeline.Response {
	finalResponse := ""
	if len(resp.ToolCalls) == 0 {
		finalResponse = resp.Content
	}

	pipelineResp := pipeline.Response{
		Role:          "assistant",
		Content:       resp.Content,
		Parts:         resp.Parts, // Include multimodal parts (images, audio, video)
		ToolCalls:     convertToolCalls(resp.ToolCalls),
		FinalResponse: finalResponse,
		Metadata: pipeline.ResponseMetadata{
			Latency: resp.Latency,
		},
	}

	if resp.CostInfo != nil {
		pipelineResp.Metadata.TokensInput = resp.CostInfo.InputTokens
		pipelineResp.Metadata.TokensOutput = resp.CostInfo.OutputTokens
		pipelineResp.Metadata.Cost = resp.CostInfo.TotalCost
	}

	return pipelineResp
}

// convertToolCalls converts provider tool calls to pipeline tool calls
func convertToolCalls(calls []types.MessageToolCall) []types.MessageToolCall {
	// No conversion needed - already the correct type
	return calls
}

// estimateRequestTokens estimates input token count from request
func estimateRequestTokens(req providers.ChatRequest) int {
	tokens := 0

	// Count system message tokens (~4 chars per token)
	if req.System != "" {
		tokens += len(req.System) / 4
	}

	// Count message tokens
	for _, msg := range req.Messages {
		tokens += len(msg.Content) / 4
	}

	// Add some overhead for message formatting
	tokens += len(req.Messages) * 4

	return tokens
}

// calculateApproximateCost calculates approximate cost for interrupted streams
func calculateApproximateCost(provider providers.Provider, req providers.ChatRequest, outputContent string) *types.CostInfo {
	// Estimate input tokens from request
	approxInputTokens := estimateRequestTokens(req)

	// Estimate output tokens from content length (~4 chars per token)
	approxOutputTokens := len(outputContent) / 4
	if approxOutputTokens == 0 {
		approxOutputTokens = 1 // Ensure at least 1 token
	}

	// Use provider's cost calculation
	costBreakdown := provider.CalculateCost(approxInputTokens, approxOutputTokens, 0)

	return &costBreakdown
}

func (m *providerMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Provider middleware doesn't intercept its own chunks
	// It generates them, so no processing needed here
	return nil
}

// isRequestMultimodal checks if any message in the request contains multimodal content
func isRequestMultimodal(req providers.ChatRequest) bool {
	for _, msg := range req.Messages {
		if msg.IsMultimodal() {
			return true
		}
	}
	return false
}
