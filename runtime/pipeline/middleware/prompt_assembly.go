package middleware

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// promptAssemblyMiddleware loads and assembles prompts from the prompt registry.
type promptAssemblyMiddleware struct {
	promptRegistry *prompt.Registry
	taskType       string
	baseVariables  map[string]string
}

// PromptAssemblyMiddleware loads and assembles prompts from the prompt registry.
// It populates execCtx.SystemPrompt, execCtx.AllowedTools, and base variables.
// This middleware should run BEFORE context extraction and template substitution.
func PromptAssemblyMiddleware(promptRegistry *prompt.Registry, taskType string, baseVariables map[string]string) pipeline.Middleware {
	return &promptAssemblyMiddleware{
		promptRegistry: promptRegistry,
		taskType:       taskType,
		baseVariables:  baseVariables,
	}
}

func (m *promptAssemblyMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// If prompt registry is not configured, use default
	if m.promptRegistry == nil {
		logger.Warn("⚠️  Using default system prompt - no prompt registry configured", "task_type", m.taskType)
		execCtx.SystemPrompt = "You are a helpful AI assistant."
		execCtx.AllowedTools = nil
		return next()
	}

	// Load and assemble prompt from registry
	assembled := m.promptRegistry.LoadWithVars(m.taskType, m.baseVariables, "")
	if assembled == nil {
		logger.Warn("⚠️  Using default system prompt - no prompt found for task type", "task_type", m.taskType)
		execCtx.SystemPrompt = "You are a helpful AI assistant."
		execCtx.AllowedTools = nil
		return next()
	}

	// Populate execution context with assembled prompt
	execCtx.SystemPrompt = assembled.SystemPrompt
	execCtx.AllowedTools = assembled.AllowedTools

	// Store validator configs in metadata for DynamicValidatorMiddleware
	// Filter out disabled validators and extract base ValidatorConfig
	if len(assembled.Validators) > 0 {
		validatorConfigs := make([]validators.ValidatorConfig, 0, len(assembled.Validators))
		for _, v := range assembled.Validators {
			// Skip disabled validators
			if v.Enabled != nil && !*v.Enabled {
				continue
			}
			// Extract the embedded validators.ValidatorConfig
			validatorConfigs = append(validatorConfigs, v.ValidatorConfig)
		}
		if len(validatorConfigs) > 0 {
			execCtx.Metadata["validator_configs"] = validatorConfigs
		}
	}

	// Initialize Variables map with base variables if not already set
	if execCtx.Variables == nil {
		execCtx.Variables = make(map[string]string)
	}
	for k, v := range m.baseVariables {
		if _, exists := execCtx.Variables[k]; !exists {
			execCtx.Variables[k] = v
		}
	}

	logger.Debug("Assembled prompt",
		"task_type", m.taskType,
		"length", len(assembled.SystemPrompt),
		"tools", len(assembled.AllowedTools),
		"base_vars", len(m.baseVariables))

	// Continue to next middleware
	return next()
}

func (m *promptAssemblyMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Prompt assembly middleware doesn't process chunks
	return nil
}
