// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
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

	// TruncationStrategy for context management ("sliding", "summarize", or "relevance")
	TruncationStrategy string

	// RelevanceConfig for embedding-based truncation (optional, used with "relevance" strategy)
	RelevanceConfig *stage.RelevanceConfig

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

	// StreamInputProvider for duplex streaming (ASM mode) (optional)
	// When provided with StreamInputConfig, DuplexProviderStage will be used.
	// The stage creates the session lazily using system_prompt from element metadata.
	StreamInputProvider providers.StreamInputSupport

	// StreamInputConfig for duplex streaming session creation (ASM mode) (optional)
	// Base config for session - system prompt is added from pipeline element metadata.
	StreamInputConfig *providers.StreamingInputConfig

	// UseStages is deprecated and ignored - stages are always used.
	// This field is kept for backward compatibility but has no effect.
	UseStages bool

	// VAD mode configuration (alternative to ASM mode)
	// When set, enables VAD pipeline: AudioTurnStage → STTStage → ProviderStage → TTSStage

	// VADConfig configures the AudioTurnStage for VAD mode
	VADConfig *stage.AudioTurnConfig

	// STTService for speech-to-text in VAD mode
	STTService stt.Service

	// STTConfig configures the STTStage
	STTConfig *stage.STTStageConfig

	// TTSService for text-to-speech in VAD mode
	TTSService tts.Service

	// TTSConfig configures the TTSStageWithInterruption
	TTSConfig *stage.TTSStageWithInterruptionConfig

	// InterruptionHandler shared between AudioTurnStage and TTSStage for barge-in support
	InterruptionHandler *audio.InterruptionHandler
}

// Build creates a stage-based streaming pipeline.
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
func Build(cfg *Config) (*stage.StreamPipeline, error) {
	return buildStreamPipelineInternal(cfg)
}

// BuildStreamPipeline is deprecated, use Build instead.
// Kept for backward compatibility.
func BuildStreamPipeline(cfg *Config) (*stage.StreamPipeline, error) {
	return Build(cfg)
}

// buildStreamPipelineInternal creates a stage pipeline directly without wrapping.
// Used by DuplexSession which handles streaming at the session level.
func buildStreamPipelineInternal(cfg *Config) (*stage.StreamPipeline, error) {
	logger.Info("Building stage-based pipeline",
		"taskType", cfg.TaskType,
		"hasStateStore", cfg.StateStore != nil,
		"hasToolRegistry", cfg.ToolRegistry != nil,
		"hasStreamProvider", cfg.StreamInputProvider != nil)

	// Create stage pipeline builder with appropriate config
	var builder *stage.PipelineBuilder
	if cfg.StreamInputProvider != nil {
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

	// 4.5 Context builder stage - manages token budget and truncation
	if cfg.TokenBudget > 0 {
		contextPolicy := buildContextBuilderPolicy(cfg)
		stages = append(stages, stage.NewContextBuilderStage(contextPolicy))
	}

	// 5. Provider stage - LLM calls with streaming and tool support
	// Use DuplexProviderStage for ASM mode (WebSocket streaming)
	// Use VAD pipeline for VAD mode (extracted to builder_vad.go - integration tested)
	// Use regular ProviderStage for text mode (HTTP API)
	if cfg.StreamInputProvider != nil {
		// ASM mode: Direct audio streaming to LLM
		// DuplexProviderStage creates session lazily using system_prompt from element metadata
		logger.Debug("Using DuplexProviderStage for ASM mode")
		stages = append(stages, stage.NewDuplexProviderStage(cfg.StreamInputProvider, cfg.StreamInputConfig))
	} else if cfg.VADConfig != nil && cfg.STTService != nil && cfg.TTSService != nil {
		// VAD mode: build audio pipeline (AudioTurnStage → STTStage → ProviderStage → TTSStage)
		vadStages, err := buildVADPipelineStages(cfg)
		if err != nil {
			return nil, err
		}
		stages = append(stages, vadStages...)
	} else if cfg.Provider != nil {
		// Text mode: standard LLM call
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

// buildContextBuilderPolicy creates a ContextBuilderPolicy from pipeline config.
func buildContextBuilderPolicy(cfg *Config) *stage.ContextBuilderPolicy {
	policy := &stage.ContextBuilderPolicy{
		TokenBudget: cfg.TokenBudget,
		Strategy:    convertTruncationStrategy(cfg.TruncationStrategy),
	}

	// Add relevance config if strategy is relevance
	if policy.Strategy == stage.TruncateLeastRelevant && cfg.RelevanceConfig != nil {
		policy.RelevanceConfig = cfg.RelevanceConfig
	}

	return policy
}

// convertTruncationStrategy converts string strategy to stage.TruncationStrategy.
func convertTruncationStrategy(strategy string) stage.TruncationStrategy {
	switch strategy {
	case "sliding", "oldest", "":
		return stage.TruncateOldest
	case "summarize":
		return stage.TruncateSummarize
	case "relevance":
		return stage.TruncateLeastRelevant
	case "fail":
		return stage.TruncateFail
	default:
		return stage.TruncateOldest
	}
}
