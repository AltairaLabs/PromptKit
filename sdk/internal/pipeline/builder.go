// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// Config holds configuration for building a pipeline.
type Config struct {
	// Provider for LLM calls
	Provider providers.Provider

	// ToolRegistry for tool execution (optional)
	ToolRegistry *tools.Registry

	// PromptRegistry for loading prompts (required for PromptAssemblyStage)
	PromptRegistry *prompt.Registry

	// TaskType is the prompt ID/task type to load from the registry
	TaskType string

	// Variables for template substitution
	Variables map[string]string

	// VariableProviders for dynamic variable resolution (optional)
	VariableProviders []variables.Provider

	// ToolPolicy for tool usage constraints (optional)
	ToolPolicy *rtpipeline.ToolPolicy

	// TokenBudget for context management (0 = no limit)
	TokenBudget int

	// TruncationStrategy for context management ("sliding" or "summarize")
	TruncationStrategy string

	// ValidatorRegistry for creating validators (optional)
	ValidatorRegistry *validators.Registry

	// ValidatorConfigs from the pack (optional)
	ValidatorConfigs []validators.ValidatorConfig

	// SuppressValidationErrors when true, validation failures don't return errors
	SuppressValidationErrors bool

	// MaxTokens for LLM response
	MaxTokens int

	// Temperature for LLM response
	Temperature float32

	// StateStore for conversation history persistence (optional)
	// When provided, StateStoreLoad/Save stages will be added to the pipeline
	StateStore statestore.Store

	// ConversationID for state store operations
	ConversationID string

	// StreamInputSession for duplex streaming (ASM mode) (optional)
	// When provided, DuplexProviderStage will be used instead of regular ProviderStage
	StreamInputSession providers.StreamInputSession

	// UseStages is deprecated and ignored - stages are always used.
	// This field is kept for backward compatibility but has no effect.
	UseStages bool
}

// Build creates a stage-based streaming pipeline wrapped for SDK compatibility.
// Use BuildStreamPipeline for duplex sessions that need direct stage pipeline access.
//
// Stage order:
//  1. StateStoreLoadStage - Load conversation history (if configured)
//  2. VariableProviderStage - Dynamic variable resolution (if configured)
//  3. PromptAssemblyStage - Load and assemble prompt from registry
//  4. TemplateStage - Prepare system prompt for provider
//  5. ProviderStage/DuplexProviderStage - LLM call with streaming support
//  6. ValidationStage - Validate responses (if configured)
//  7. StateStoreSaveStage - Save conversation state (if configured)
//
// This matches the runtime pipeline used by Arena.
func Build(cfg *Config) (*rtpipeline.Pipeline, error) {
	return buildStagePipeline(cfg)
}

// BuildStreamPipeline creates a stage-based streaming pipeline directly.
// This is used by DuplexSession which manages streaming at the session level.
func BuildStreamPipeline(cfg *Config) (*stage.StreamPipeline, error) {
	return buildStreamPipelineInternal(cfg)
}

// buildStreamPipelineInternal creates a stage pipeline directly without wrapping.
// Used by DuplexSession which handles streaming at the session level.
func buildStreamPipelineInternal(cfg *Config) (*stage.StreamPipeline, error) {
	logger.Info("Building stage-based pipeline",
		"taskType", cfg.TaskType,
		"hasStateStore", cfg.StateStore != nil,
		"hasToolRegistry", cfg.ToolRegistry != nil,
		"hasDuplexSession", cfg.StreamInputSession != nil)

	// Create stage pipeline builder with appropriate config
	var builder *stage.PipelineBuilder
	if cfg.StreamInputSession != nil {
		// For duplex streaming (ASM mode), disable execution timeout
		// since the session runs indefinitely until user ends it
		pipelineConfig := stage.DefaultPipelineConfig()
		pipelineConfig.ExecutionTimeout = 0 // Disable timeout for duplex
		builder = stage.NewPipelineBuilderWithConfig(pipelineConfig)
	} else {
		builder = stage.NewPipelineBuilder()
	}

	var stages []stage.Stage

	// 1. State store load stage - loads conversation history FIRST
	var stateStoreConfig *rtpipeline.StateStoreConfig
	if cfg.StateStore != nil {
		stateStoreConfig = &rtpipeline.StateStoreConfig{
			Store:          cfg.StateStore,
			ConversationID: cfg.ConversationID,
		}
		stages = append(stages, stage.NewStateStoreLoadStage(stateStoreConfig))
	}

	// 2. Variable provider stage - resolves dynamic variables
	if len(cfg.VariableProviders) > 0 {
		stages = append(stages, stage.NewVariableProviderStage(cfg.VariableProviders...))
	}

	// 3. Prompt assembly stage - loads prompt, sets system prompt and allowed tools
	// 4. Template stage - prepares system prompt for provider
	stages = append(stages,
		stage.NewPromptAssemblyStage(cfg.PromptRegistry, cfg.TaskType, cfg.Variables),
		stage.NewTemplateStage(),
	)

	// 5. Provider stage - LLM calls with streaming and tool support
	// Use DuplexProviderStage for ASM mode (WebSocket streaming)
	// Use regular ProviderStage for text/VAD mode (HTTP API)
	if cfg.StreamInputSession != nil {
		logger.Debug("Using DuplexProviderStage for ASM mode")
		stages = append(stages, stage.NewDuplexProviderStage(cfg.StreamInputSession))
	} else if cfg.Provider != nil {
		providerConfig := &stage.ProviderConfig{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		}
		stages = append(stages, stage.NewProviderStage(
			cfg.Provider,
			cfg.ToolRegistry,
			cfg.ToolPolicy,
			providerConfig,
		))
	}

	// 6. Validation stage - validate responses if configured
	if cfg.ValidatorRegistry != nil && len(cfg.ValidatorConfigs) > 0 {
		stages = append(stages, stage.NewValidationStage(
			cfg.ValidatorRegistry,
			cfg.SuppressValidationErrors,
		))
	}

	// 7. State store save stage - saves conversation state LAST
	if stateStoreConfig != nil {
		stages = append(stages, stage.NewStateStoreSaveStage(stateStoreConfig))
	}

	// Build and return the StreamPipeline directly
	streamPipeline, err := builder.Chain(stages...).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build stage pipeline: %w", err)
	}

	return streamPipeline, nil
}

// buildStagePipeline creates a pipeline wrapped for SDK compatibility (unary mode).
func buildStagePipeline(cfg *Config) (*rtpipeline.Pipeline, error) {
	streamPipeline, err := buildStreamPipelineInternal(cfg)
	if err != nil {
		return nil, err
	}
	return wrapStreamPipeline(streamPipeline), nil
}

// wrapStreamPipeline wraps a StreamPipeline for SDK's unary Pipeline interface.
// Note: Duplex sessions use BuildStreamPipeline and manage streaming directly.
func wrapStreamPipeline(sp *stage.StreamPipeline) *rtpipeline.Pipeline {
	adapter := &streamPipelineMiddlewareAdapter{streamPipeline: sp}
	pipeline, _ := rtpipeline.NewPipelineWithConfigValidated(nil, adapter)
	return pipeline
}

// streamPipelineMiddlewareAdapter bridges unary middleware execution to stage execution.
// Duplex streaming is handled directly by DuplexSession using StreamPipeline.Execute().
type streamPipelineMiddlewareAdapter struct {
	streamPipeline *stage.StreamPipeline
}

// Process converts middleware ExecutionContext to StreamElement, executes stages, and converts back.
// Supports both unary and streaming modes.
func (a *streamPipelineMiddlewareAdapter) Process(execCtx *rtpipeline.ExecutionContext, _ func() error) error {
	ctx := execCtx.Context
	if ctx == nil {
		return fmt.Errorf("execution context missing from pipeline")
	}

	// Convert ExecutionContext to StreamElement for stage input
	inputElem := executionContextToStreamElement(execCtx)

	// For streaming mode, use Execute and emit chunks as they arrive
	if execCtx.StreamMode {
		return a.processStreaming(ctx, &inputElem, execCtx)
	}

	// Unary mode: execute synchronously
	result, err := a.streamPipeline.ExecuteSync(ctx, inputElem)
	if err != nil {
		return err
	}

	// Convert output ExecutionResult back to ExecutionContext
	executionResultToExecutionContext(result, execCtx)

	return nil
}

// processStreaming handles streaming execution by emitting chunks as they arrive.
func (a *streamPipelineMiddlewareAdapter) processStreaming(
	ctx context.Context,
	inputElem *stage.StreamElement,
	execCtx *rtpipeline.ExecutionContext,
) error {
	// Create input channel with the element
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- *inputElem
	close(inputChan)

	// Execute as stream
	outputChan, err := a.streamPipeline.Execute(ctx, inputChan)
	if err != nil {
		return err
	}

	// Process output elements - emit text chunks and accumulate result
	var accumulatedContent string
	var messages []types.Message
	var response *stage.Response

	for elem := range outputChan {
		// Handle errors
		if elem.Error != nil {
			return elem.Error
		}

		// Emit text chunks for streaming
		if elem.Text != nil && *elem.Text != "" {
			accumulatedContent += *elem.Text
			// Emit the chunk through the execution context
			chunk := providers.StreamChunk{
				Delta:   *elem.Text,
				Content: accumulatedContent,
			}
			execCtx.EmitStreamChunk(chunk)
		}

		// Collect messages
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
			// Track assistant response
			if elem.Message.Role == "assistant" {
				response = &stage.Response{
					Role:    elem.Message.Role,
					Content: elem.Message.Content,
					Parts:   elem.Message.Parts,
				}
			}
		}
	}

	// Set final response from accumulated data
	if response != nil {
		execCtx.Response = &rtpipeline.Response{
			Role:      response.Role,
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		}
	} else if accumulatedContent != "" {
		// If we only got text chunks without a final message, create response from accumulated text
		execCtx.Response = &rtpipeline.Response{
			Role:    "assistant",
			Content: accumulatedContent,
		}
	}

	if len(messages) > 0 {
		execCtx.Messages = messages
	}

	return nil
}

// StreamChunk is not used since stages handle streaming internally.
func (a *streamPipelineMiddlewareAdapter) StreamChunk(
	_ *rtpipeline.ExecutionContext,
	_ *providers.StreamChunk,
) error {
	return nil
}

// executionContextToStreamElement converts middleware ExecutionContext to stage StreamElement.
func executionContextToStreamElement(execCtx *rtpipeline.ExecutionContext) stage.StreamElement {
	elem := stage.StreamElement{
		Metadata: make(map[string]interface{}),
	}

	// Get the user message from the execution context's Messages list
	if len(execCtx.Messages) > 0 {
		userMsg := execCtx.Messages[len(execCtx.Messages)-1]
		elem.Message = &userMsg
	}

	// Copy metadata
	if execCtx.Metadata != nil {
		for k, v := range execCtx.Metadata {
			elem.Metadata[k] = v
		}
	}

	// Copy execution context fields as metadata
	if execCtx.SystemPrompt != "" {
		elem.Metadata["system_prompt"] = execCtx.SystemPrompt
	}
	if len(execCtx.AllowedTools) > 0 {
		elem.Metadata["allowed_tools"] = execCtx.AllowedTools
	}
	if len(execCtx.Variables) > 0 {
		elem.Metadata["variables"] = execCtx.Variables
	}

	return elem
}

// executionResultToExecutionContext converts stage ExecutionResult back to ExecutionContext.
func executionResultToExecutionContext(result *stage.ExecutionResult, execCtx *rtpipeline.ExecutionContext) {
	if result.Response != nil {
		execCtx.Response = &rtpipeline.Response{
			Role:      result.Response.Role,
			Content:   result.Response.Content,
			ToolCalls: result.Response.ToolCalls,
		}
	}

	if result.Metadata != nil {
		if execCtx.Metadata == nil {
			execCtx.Metadata = make(map[string]interface{})
		}
		for k, v := range result.Metadata {
			execCtx.Metadata[k] = v
		}
	}

	if len(result.Messages) > 0 {
		execCtx.Messages = result.Messages
	}
}
