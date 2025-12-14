// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
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
}

// Build creates a pipeline with the appropriate middleware chain.
//
// The pipeline is structured as follows:
//  1. StateStoreLoadMiddleware - Load conversation history (if state store configured)
//  2. PromptAssemblyMiddleware - Load and assemble the prompt from registry
//  3. ProviderMiddleware - LLM call with tool execution
//  4. DynamicValidatorMiddleware - Validate responses (if configured)
//  5. StateStoreSaveMiddleware - Save conversation state (if state store configured)
//
// This matches the runtime pipeline used by Arena.
func Build(cfg *Config) (*rtpipeline.Pipeline, error) {
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
