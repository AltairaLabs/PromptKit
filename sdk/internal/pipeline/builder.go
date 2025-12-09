// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
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
}

// Build creates a pipeline with the appropriate middleware chain.
//
// The pipeline is structured as follows:
//  1. PromptAssemblyMiddleware - Load and assemble the prompt from registry
//  2. ProviderMiddleware - LLM call with tool execution
//  3. DynamicValidatorMiddleware - Validate responses (if configured)
//
// This matches the runtime pipeline used by Arena.
func Build(cfg *Config) (*rtpipeline.Pipeline, error) {
	var middlewares []rtpipeline.Middleware

	// Debug: log configuration
	logger.Debug("Building pipeline",
		"taskType", cfg.TaskType,
		"variables", cfg.Variables,
		"hasPromptRegistry", cfg.PromptRegistry != nil,
		"hasToolRegistry", cfg.ToolRegistry != nil)

	// 1. Prompt assembly middleware - loads prompt, sets system prompt and allowed tools
	// 2. Template middleware - copies SystemPrompt to Prompt (for ProviderMiddleware)
	// 3. Debug middleware to log the assembled prompt
	middlewares = append(middlewares,
		middleware.PromptAssemblyMiddleware(cfg.PromptRegistry, cfg.TaskType, cfg.Variables),
		middleware.TemplateMiddleware(),
		&debugMiddleware{},
	)

	// 3. Provider middleware for LLM calls with tool support
	if cfg.Provider != nil {
		providerConfig := &middleware.ProviderMiddlewareConfig{
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		}

		middlewares = append(middlewares, middleware.ProviderMiddleware(
			cfg.Provider,
			cfg.ToolRegistry,
			cfg.ToolPolicy,
			providerConfig,
		))
	}

	// 4. Validation middleware (if configured)
	if cfg.ValidatorRegistry != nil && len(cfg.ValidatorConfigs) > 0 {
		middlewares = append(middlewares, middleware.DynamicValidatorMiddlewareWithSuppression(
			cfg.ValidatorRegistry,
			cfg.SuppressValidationErrors,
		))
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
