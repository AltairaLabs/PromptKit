package stage

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

const (
	defaultMaxRounds = 10
)

// ProviderStage implementation notes:
// - ✅ Multi-round tool execution with automatic tool result handling
// - ✅ Synchronous tool execution via toolRegistry.ExecuteAsync()
// - ⚠️ Limited async/pending tool support (no ExecutionContext for tracking)
// - Note: For full async tool support with approval workflows, use MiddlewareAdapter

// ProviderStage executes LLM calls and handles tool execution.
// This is the request/response mode implementation.
type ProviderStage struct {
	BaseStage
	provider     providers.Provider
	toolRegistry *tools.Registry
	toolPolicy   *pipeline.ToolPolicy
	config       *ProviderConfig
}

// ProviderConfig contains configuration for the provider stage.
type ProviderConfig struct {
	MaxTokens    int
	Temperature  float32
	Seed         *int
	DisableTrace bool
}

// NewProviderStage creates a new provider stage for request/response mode.
func NewProviderStage(
	provider providers.Provider,
	toolRegistry *tools.Registry,
	toolPolicy *pipeline.ToolPolicy,
	config *ProviderConfig,
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
	}
}

// Process executes the LLM provider call and handles tool execution.
//
//nolint:lll,gocognit // Channel signature cannot be shortened, complexity inherent to provider logic
func (s *ProviderStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)

	if s.provider == nil {
		return errors.New("provider stage: no provider configured")
	}

	// Accumulate input messages and metadata
	var messages []types.Message
	var systemPrompt string
	var allowedTools []string
	metadata := make(map[string]interface{})

	for elem := range input {
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
		if elem.Metadata != nil {
			// Extract prompt assembly data
			if sp, ok := elem.Metadata["system_prompt"].(string); ok {
				systemPrompt = sp
			}
			if tools, ok := elem.Metadata["allowed_tools"].([]string); ok {
				allowedTools = tools
			}
			// Merge all metadata
			for k, v := range elem.Metadata {
				metadata[k] = v
			}
		}
	}

	// Execute provider with multi-round support for tools
	responseMessages, err := s.executeMultiRound(ctx, messages, systemPrompt, allowedTools, metadata)
	if err != nil {
		output <- NewErrorElement(err)
		return err
	}

	// Emit response messages
	for i := range responseMessages {
		elem := NewMessageElement(&responseMessages[i])
		elem.Metadata = metadata

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

//nolint:gocognit // Complexity inherent to multi-round tool execution
func (s *ProviderStage) executeMultiRound(
	ctx context.Context,
	messages []types.Message,
	systemPrompt string,
	allowedTools []string,
	metadata map[string]interface{},
) ([]types.Message, error) {
	// Build tools for provider
	providerTools, toolChoice, err := s.buildProviderTools(allowedTools)
	if err != nil {
		return nil, fmt.Errorf("provider stage: %w", err)
	}

	// Multi-round execution loop for tool calls
	round := 0
	maxRounds := defaultMaxRounds
	if s.toolPolicy != nil && s.toolPolicy.MaxRounds > 0 {
		maxRounds = s.toolPolicy.MaxRounds
	}

	for {
		round++

		// Check max rounds
		if round > maxRounds {
			return messages, fmt.Errorf("provider stage: max rounds (%d) exceeded", maxRounds)
		}

		// Execute one round
		response, hasToolCalls, err := s.executeRound(ctx, messages, systemPrompt, providerTools, toolChoice, round)
		if err != nil {
			return messages, err
		}

		// Add response to messages
		messages = append(messages, response)

		// If no tool calls, we're done
		if !hasToolCalls {
			break
		}

		// Execute tool calls and add results to messages
		toolResults, err := s.executeToolCalls(ctx, response.ToolCalls)
		if err != nil {
			return messages, fmt.Errorf("provider stage: tool execution failed: %w", err)
		}

		messages = append(messages, toolResults...)

		// For subsequent rounds, use "auto" tool choice
		toolChoice = "auto"
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
) (types.Message, bool, error) {
	// Build provider request
	req := providers.PredictionRequest{
		System:      systemPrompt,
		Messages:    messages,
		MaxTokens:   s.config.MaxTokens,
		Temperature: s.config.Temperature,
		Seed:        s.config.Seed,
	}

	logger.Debug("Provider round starting",
		"round", round,
		"messages", len(messages),
		"tools", providerTools != nil)

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
		return types.Message{}, false, fmt.Errorf("provider call failed: %w", err)
	}

	// Build response message
	responseMsg := types.Message{
		Role:      "assistant",
		Content:   resp.Content,
		Parts:     resp.Parts,
		ToolCalls: toolCalls,
	}

	logger.Debug("Provider round completed",
		"round", round,
		"duration", duration,
		"tool_calls", len(toolCalls))

	// Check for tool calls
	hasToolCalls := len(toolCalls) > 0

	return responseMsg, hasToolCalls, nil
}

func (s *ProviderStage) executeToolCalls(ctx context.Context, toolCalls []types.MessageToolCall) ([]types.Message, error) {
	if s.toolRegistry == nil {
		return nil, errors.New("tool registry not configured but tool calls present")
	}

	results := make([]types.Message, 0, len(toolCalls))

	for _, toolCall := range toolCalls {
		// Check if tool is blocked by policy
		if s.toolPolicy != nil && isToolBlocked(toolCall.Name, s.toolPolicy.Blocklist) {
			results = append(results, types.Message{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:      toolCall.ID,
					Name:    toolCall.Name,
					Content: fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name),
					Error:   fmt.Sprintf("Tool %s is blocked by policy", toolCall.Name),
				},
			})
			continue
		}

		// Execute tool via registry (handles both sync and async tools)
		asyncResult, err := s.toolRegistry.ExecuteAsync(toolCall.Name, toolCall.Args)
		if err != nil {
			// Tool not found or execution setup failed
			results = append(results, types.Message{
				Role: "tool",
				ToolResult: &types.MessageToolResult{
					ID:      toolCall.ID,
					Name:    toolCall.Name,
					Content: fmt.Sprintf("Error: %v", err),
					Error:   err.Error(),
				},
			})
			continue
		}

		// Convert tool execution result to message
		result := s.handleToolResult(toolCall, asyncResult)
		results = append(results, types.Message{
			Role:       "tool",
			ToolResult: &result,
		})
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
			ID:   call.ID,
			Name: call.Name,
			Content: pendingMsg + " (Note: Async tool support in stages is limited - " +
				"consider using MiddlewareAdapter for full async support)",
			Error: "",
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
		if err := json.Unmarshal(asyncResult.Content, &resultValue); err == nil {
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
