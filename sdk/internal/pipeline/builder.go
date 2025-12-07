// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// Config holds configuration for building a pipeline.
type Config struct {
	// Provider for LLM calls
	Provider providers.Provider

	// ToolRegistry for tool execution (optional)
	ToolRegistry *tools.Registry

	// SystemPrompt is the rendered system prompt
	SystemPrompt string

	// Tools available to the LLM (optional)
	Tools []*tools.ToolDescriptor

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
//  1. SystemPromptMiddleware - Set the system prompt on context
//  2. ProviderMiddleware - LLM call with tool execution
//  3. DynamicValidatorMiddleware - Validate responses (if configured)
//
// This matches the runtime pipeline used by Arena.
func Build(cfg *Config) (*rtpipeline.Pipeline, error) {
	var middlewares []rtpipeline.Middleware

	// 1. System prompt middleware
	middlewares = append(middlewares, &SystemPromptMiddleware{
		SystemPrompt: cfg.SystemPrompt,
	})

	// 2. Provider middleware for LLM calls with tool support
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

	// 3. Validation middleware (if configured)
	if cfg.ValidatorRegistry != nil && len(cfg.ValidatorConfigs) > 0 {
		middlewares = append(middlewares, middleware.DynamicValidatorMiddlewareWithSuppression(
			cfg.ValidatorRegistry,
			cfg.SuppressValidationErrors,
		))
	}

	// Create pipeline with default config
	return rtpipeline.NewPipelineWithConfigValidated(nil, middlewares...)
}

// SystemPromptMiddleware sets the system prompt on the execution context.
type SystemPromptMiddleware struct {
	SystemPrompt string
}

// Process implements pipeline.Middleware.
func (m *SystemPromptMiddleware) Process(ctx *rtpipeline.ExecutionContext, next func() error) error {
	ctx.SystemPrompt = m.SystemPrompt
	return next()
}

// StreamChunk implements pipeline.Middleware (no-op for this middleware).
func (m *SystemPromptMiddleware) StreamChunk(_ *rtpipeline.ExecutionContext, _ *providers.StreamChunk) error {
	return nil
}
