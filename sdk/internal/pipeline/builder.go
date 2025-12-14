// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// debugLogTruncateLen is the max length for system prompt debug output.
const debugLogTruncateLen = 200

// Config holds configuration for building a pipeline.
type Config struct {
	// Provider for LLM calls
	Provider providers.Provider

	// ToolRegistry for tool execution (optional)
	ToolRegistry *tools.Registry

	// PromptRegistry for loading prompts (required for PromptAssemblyMiddleware)
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
	// When provided, StateStoreLoad/Save middleware will be added to the pipeline
	StateStore statestore.Store

	// ConversationID for state store operations
	ConversationID string

	// StreamInputSession for duplex streaming (ASM mode) (optional)
	// When provided, DuplexProviderMiddleware will be used instead of regular ProviderMiddleware
	StreamInputSession providers.StreamInputSession

	// UseStages when true, builds pipeline using stage architecture instead of middleware
	// Default: false (uses middleware for backward compatibility)
	// Set to true to enable streaming pipeline architecture with concurrent stage execution
	UseStages bool
}

// Build creates a pipeline with the appropriate implementation (middleware or stages).
//
// When cfg.UseStages is false (default), creates a middleware-based pipeline:
//  1. StateStoreLoadMiddleware - Load conversation history (if state store configured)
//  2. PromptAssemblyMiddleware - Load and assemble the prompt from registry
//  3. ProviderMiddleware - LLM call with tool execution
//  4. DynamicValidatorMiddleware - Validate responses (if configured)
//  5. StateStoreSaveMiddleware - Save conversation state (if state store configured)
//
// When cfg.UseStages is true, creates a stage-based streaming pipeline with the same functionality
// but using concurrent stage execution for better performance.
//
// This matches the runtime pipeline used by Arena.
func Build(cfg *Config) (*rtpipeline.Pipeline, error) {
	// Route to stage-based implementation if enabled
	if cfg.UseStages {
		return buildStagePipeline(cfg)
	}

	// Otherwise use legacy middleware implementation
	return buildMiddlewarePipeline(cfg)
}

// buildMiddlewarePipeline creates a middleware-based pipeline (legacy).
func buildMiddlewarePipeline(cfg *Config) (*rtpipeline.Pipeline, error) {
	//nolint:staticcheck // Middleware is deprecated but used for backward compatibility
	var middlewares []rtpipeline.Middleware

	// Debug: log configuration
	logger.Debug("Building pipeline",
		"taskType", cfg.TaskType,
		"variables", cfg.Variables,
		"hasPromptRegistry", cfg.PromptRegistry != nil,
		"hasToolRegistry", cfg.ToolRegistry != nil,
		"hasStateStore", cfg.StateStore != nil)

	// 1. State store load middleware - loads conversation history FIRST
	var stateStoreConfig *rtpipeline.StateStoreConfig
	if cfg.StateStore != nil {
		stateStoreConfig = &rtpipeline.StateStoreConfig{
			Store:          cfg.StateStore,
			ConversationID: cfg.ConversationID,
		}
		middlewares = append(middlewares, middleware.StateStoreLoadMiddleware(stateStoreConfig))
	}

	// 2. Variable provider middleware - resolves dynamic variables BEFORE prompt assembly
	if len(cfg.VariableProviders) > 0 {
		middlewares = append(middlewares, middleware.VariableProviderMiddleware(cfg.VariableProviders...))
	}

	// 3. Prompt assembly middleware - loads prompt, sets system prompt and allowed tools
	// 4. Template middleware - copies SystemPrompt to Prompt (for ProviderMiddleware)
	// 5. Debug middleware to log the assembled prompt
	middlewares = append(middlewares,
		middleware.PromptAssemblyMiddleware(cfg.PromptRegistry, cfg.TaskType, cfg.Variables),
		middleware.TemplateMiddleware(),
		&debugMiddleware{},
	)

	// 5. Provider middleware for LLM calls with tool support
	if cfg.Provider != nil {
		providerConfig := &middleware.ProviderMiddlewareConfig{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		}

		// Use DuplexProviderMiddleware for ASM mode (WebSocket streaming)
		// Use regular ProviderMiddleware for text/VAD mode (HTTP API)
		if cfg.StreamInputSession != nil {
			logger.Debug("Using DuplexProviderMiddleware for ASM mode")
			middlewares = append(middlewares, middleware.DuplexProviderMiddleware(
				cfg.StreamInputSession,
				providerConfig,
			))
		} else {
			middlewares = append(middlewares, middleware.ProviderMiddleware(
				cfg.Provider,
				cfg.ToolRegistry,
				cfg.ToolPolicy,
				providerConfig,
			))
		}
	}

	// 6. Validation middleware (if configured)
	if cfg.ValidatorRegistry != nil && len(cfg.ValidatorConfigs) > 0 {
		middlewares = append(middlewares, middleware.DynamicValidatorMiddlewareWithSuppression(
			cfg.ValidatorRegistry,
			cfg.SuppressValidationErrors,
		))
	}

	// 7. State store save middleware - saves conversation state LAST
	if stateStoreConfig != nil {
		middlewares = append(middlewares, middleware.StateStoreSaveMiddleware(stateStoreConfig))
	}

	// Create pipeline with default config
	return rtpipeline.NewPipelineWithConfigValidated(nil, middlewares...)
}

// debugMiddleware logs execution context for debugging.
type debugMiddleware struct{}

// Process logs the execution context state for debugging.
func (m *debugMiddleware) Process(execCtx *rtpipeline.ExecutionContext, next func() error) error {
	if len(execCtx.SystemPrompt) > debugLogTruncateLen {
		logger.Debug("After PromptAssembly",
			"systemPromptLength", len(execCtx.SystemPrompt),
			"systemPromptPreview", execCtx.SystemPrompt[:debugLogTruncateLen]+"...",
			"allowedTools", execCtx.AllowedTools,
			"variables", execCtx.Variables)
	} else {
		logger.Debug("After PromptAssembly",
			"systemPromptLength", len(execCtx.SystemPrompt),
			"systemPrompt", execCtx.SystemPrompt,
			"allowedTools", execCtx.AllowedTools,
			"variables", execCtx.Variables)
	}
	return next()
}

// StreamChunk is a no-op for debug middleware.
func (m *debugMiddleware) StreamChunk(_ *rtpipeline.ExecutionContext, _ *providers.StreamChunk) error {
	return nil
}

// buildStagePipeline creates a pipeline using the stage architecture with streaming support.
//
// Builds a stage-based pipeline that provides the same functionality as middleware but with:
// - Native streaming support via StreamElements
// - Concurrent stage execution for better performance
// - Consistent architecture with Arena test execution
//
// Stage order:
//  1. StateStoreLoadStage - Load conversation history (if configured)
//  2. VariableProviderMiddleware - Dynamic variable resolution (if configured, wrapped as stage)
//  3. PromptAssemblyStage - Load and assemble prompt from registry
//  4. TemplateStage - Prepare system prompt for provider
//  5. ProviderStage - LLM call with streaming support and tool execution
//  6. ValidationStage - Validate responses (if configured)
//  7. StateStoreSaveStage - Save conversation state (if configured)
//
// Note: DuplexProviderMiddleware (WebSocket streaming) is not yet supported with stages.
// Use middleware mode for duplex sessions.
func buildStagePipeline(cfg *Config) (*rtpipeline.Pipeline, error) {
	logger.Info("Building stage-based pipeline (UseStages=true)",
		"taskType", cfg.TaskType,
		"hasStateStore", cfg.StateStore != nil,
		"hasToolRegistry", cfg.ToolRegistry != nil)

	// Duplex streaming not yet supported with stages
	if cfg.StreamInputSession != nil {
		logger.Warn("DuplexProviderMiddleware not supported with stages, falling back to middleware",
			"taskType", cfg.TaskType)
		return buildMiddlewarePipeline(cfg)
	}

	// Create stage pipeline builder
	builder := stage.NewPipelineBuilder()

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

	// 2. Variable provider middleware - wrap as stage if configured
	if len(cfg.VariableProviders) > 0 {
		stages = append(stages, stage.WrapMiddleware("variable_providers",
			middleware.VariableProviderMiddleware(cfg.VariableProviders...)))
	}

	// 3. Prompt assembly stage - loads prompt, sets system prompt and allowed tools
	// 4. Template stage - prepares system prompt for provider
	stages = append(stages,
		stage.NewPromptAssemblyStage(cfg.PromptRegistry, cfg.TaskType, cfg.Variables),
		stage.NewTemplateStage(),
	)

	// 5. Provider stage - LLM calls with streaming and tool support
	if cfg.Provider != nil {
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

	// Build the StreamPipeline
	streamPipeline, err := builder.Chain(stages...).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build stage pipeline: %w", err)
	}

	// Create a stage-based execution adapter
	// The SDK session code will need to be updated to detect and use this properly
	// For now, store the StreamPipeline in a wrapper
	return wrapStreamPipeline(streamPipeline), nil
}

// wrapStreamPipeline wraps a StreamPipeline to work with SDK's Pipeline interface.
// This is a bridge solution until SDK fully migrates to stage-based execution.
func wrapStreamPipeline(sp *stage.StreamPipeline) *rtpipeline.Pipeline {
	// Create a pipeline that uses a special middleware adapter
	// The adapter will convert between middleware ExecutionContext and stage StreamElements
	adapter := &streamPipelineMiddlewareAdapter{streamPipeline: sp}

	// Build a pipeline with just this one middleware that delegates to stages
	pipeline, _ := rtpipeline.NewPipelineWithConfigValidated(nil, adapter)
	return pipeline
}

// streamPipelineMiddlewareAdapter bridges middleware execution to stage execution.
type streamPipelineMiddlewareAdapter struct {
	streamPipeline *stage.StreamPipeline
}

// Process converts middleware ExecutionContext to StreamElement, executes stages, and converts back.
func (a *streamPipelineMiddlewareAdapter) Process(execCtx *rtpipeline.ExecutionContext, next func() error) error {
	// TODO: Get proper context from pipeline execution
	// For now, use background context - this will be fixed when we have full SDK integration
	ctx := execCtx.Context
	if ctx == nil {
		return fmt.Errorf("execution context missing from pipeline")
	}

	// Convert ExecutionContext to StreamElement for stage input
	inputElem := executionContextToStreamElement(execCtx)

	// Execute the stage pipeline synchronously
	result, err := a.streamPipeline.ExecuteSync(ctx, inputElem)
	if err != nil {
		return err
	}

	// Convert output ExecutionResult back to ExecutionContext
	executionResultToExecutionContext(result, execCtx)

	// Skip calling next() since stages already did all the work
	return nil
}

// StreamChunk is not used since stages handle streaming internally.
func (a *streamPipelineMiddlewareAdapter) StreamChunk(
	execCtx *rtpipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	return nil
}

// executionContextToStreamElement converts middleware ExecutionContext to stage StreamElement.
func executionContextToStreamElement(execCtx *rtpipeline.ExecutionContext) stage.StreamElement {
	elem := stage.StreamElement{
		Metadata: make(map[string]interface{}),
	}

	// Get the user message from the execution context's Messages list
	// The last message in the list should be the user message
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
	// Extract the final response from the result and convert to pipeline Response
	if result.Response != nil {
		execCtx.Response = &rtpipeline.Response{
			Role:      result.Response.Role,
			Content:   result.Response.Content,
			ToolCalls: result.Response.ToolCalls,
		}
	}

	// Extract metadata
	if result.Metadata != nil {
		if execCtx.Metadata == nil {
			execCtx.Metadata = make(map[string]interface{})
		}
		for k, v := range result.Metadata {
			execCtx.Metadata[k] = v
		}
	}

	// Update messages if present
	if len(result.Messages) > 0 {
		execCtx.Messages = result.Messages
	}
}
