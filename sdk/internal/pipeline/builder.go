// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"fmt"
	"os"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// debugLogTruncateLen is the max length for system prompt debug output.
const debugLogTruncateLen = 200

// debugLogf prints debug output if SDK_DEBUG is set.
func debugLogf(format string, args ...any) {
	if os.Getenv("SDK_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[SDK DEBUG] "+format+"\n", args...)
	}
}

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
	debugLogf("Building pipeline for taskType=%q", cfg.TaskType)
	debugLogf("Variables: %v", cfg.Variables)
	debugLogf("PromptRegistry: %v", cfg.PromptRegistry != nil)
	debugLogf("ToolRegistry: %v", cfg.ToolRegistry != nil)

	// 1. Prompt assembly middleware - loads prompt, sets system prompt and allowed tools
	// 2. Debug middleware to log the assembled prompt (only active when SDK_DEBUG is set)
	middlewares = append(middlewares,
		middleware.PromptAssemblyMiddleware(cfg.PromptRegistry, cfg.TaskType, cfg.Variables),
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

// debugMiddleware logs execution context for debugging when SDK_DEBUG is set.
type debugMiddleware struct{}

// Process logs the execution context state for debugging.
func (m *debugMiddleware) Process(execCtx *rtpipeline.ExecutionContext, next func() error) error {
	debugLogf("After PromptAssembly:")
	debugLogf("  SystemPrompt length: %d", len(execCtx.SystemPrompt))
	if len(execCtx.SystemPrompt) > debugLogTruncateLen {
		debugLogf("  SystemPrompt (first %d): %s...", debugLogTruncateLen, execCtx.SystemPrompt[:debugLogTruncateLen])
	} else {
		debugLogf("  SystemPrompt: %s", execCtx.SystemPrompt)
	}
	debugLogf("  AllowedTools: %v", execCtx.AllowedTools)
	debugLogf("  Variables: %v", execCtx.Variables)
	return next()
}

// StreamChunk is a no-op for debug middleware.
func (m *debugMiddleware) StreamChunk(_ *rtpipeline.ExecutionContext, _ *providers.StreamChunk) error {
	return nil
}
