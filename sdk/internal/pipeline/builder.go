// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
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

	// MaxTokens for LLM response
	MaxTokens int

	// Temperature for LLM response
	Temperature float32

	// ResponseFormat for JSON mode output (optional)
	ResponseFormat *providers.ResponseFormat

	// StateStore for conversation history persistence (optional)
	// When provided, StateStoreLoad/Save stages will be added to the pipeline
	StateStore statestore.Store

	// ConversationID for state store operations
	ConversationID string

	// ContextWindow is the hot window size for RAG context assembly.
	// When > 0, ContextAssemblyStage + IncrementalSaveStage replace the
	// standard StateStoreLoad/Save stages.
	ContextWindow int

	// MessageIndex for semantic retrieval of relevant older messages (optional).
	MessageIndex statestore.MessageIndex

	// RetrievalTopK is the number of results to retrieve from the message index.
	RetrievalTopK int

	// Summarizer for auto-summarization (optional).
	Summarizer statestore.Summarizer

	// SummarizeThreshold is the message count above which summarization triggers.
	SummarizeThreshold int

	// SummarizeBatchSize is how many messages to summarize at once.
	SummarizeBatchSize int

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

	// ImagePreprocessConfig configures image preprocessing (resizing, optimization)
	// When non-nil, ImagePreprocessStage is added before the provider stage
	ImagePreprocessConfig *stage.ImagePreprocessConfig

	// EventEmitter for emitting provider call events (optional)
	// When provided, ProviderStage will emit ProviderCallStarted/Completed/Failed events
	EventEmitter *events.Emitter

	// HookRegistry for policy enforcement hooks (optional)
	// When provided, ProviderStage will use hooks for provider call, chunk, and tool interception
	HookRegistry *hooks.Registry
}

// Build creates a stage-based streaming pipeline.
//
// Stage order:
//  1. StateStoreLoadStage - Load conversation history (if configured)
//  2. VariableProviderStage - Dynamic variable resolution (if configured)
//  3. PromptAssemblyStage - Load and assemble prompt from registry
//  4. TemplateStage - Prepare system prompt for provider
//  5. ProviderStage/DuplexProviderStage - LLM call with streaming support
//  6. StateStoreSaveStage - Save conversation state (if configured)
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
//
//nolint:gocognit // Complex pipeline construction logic
func buildStreamPipelineInternal(cfg *Config) (*stage.StreamPipeline, error) {
	logger.Info("Building stage-based pipeline",
		"task_type", cfg.TaskType,
		"has_state_store", cfg.StateStore != nil,
		"has_tool_registry", cfg.ToolRegistry != nil,
		"has_stream_provider", cfg.StreamInputProvider != nil)

	builder := newPipelineBuilder(cfg)

	stateStoreConfig, useRAGContext := buildStateStoreConfig(cfg)

	stages, err := collectPipelineStages(cfg, stateStoreConfig, useRAGContext)
	if err != nil {
		return nil, err
	}

	// Wire event emitter so the pipeline emits PipelineStarted/Completed events.
	if cfg.EventEmitter != nil {
		builder.WithEventEmitter(cfg.EventEmitter)
	}

	// Build and return the StreamPipeline directly
	streamPipeline, err := builder.Chain(stages...).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build stage pipeline: %w", err)
	}

	return streamPipeline, nil
}

// newPipelineBuilder creates the appropriate pipeline builder for the config.
func newPipelineBuilder(cfg *Config) *stage.PipelineBuilder {
	if cfg.StreamInputProvider != nil {
		// For duplex streaming (ASM mode), disable execution timeout
		// since the session runs indefinitely until user ends it
		pipelineConfig := stage.DefaultPipelineConfig()
		pipelineConfig.ExecutionTimeout = 0 // Disable timeout for duplex
		return stage.NewPipelineBuilderWithConfig(pipelineConfig)
	}
	return stage.NewPipelineBuilder()
}

// buildStateStoreConfig creates a state store config if a state store is configured.
func buildStateStoreConfig(cfg *Config) (*rtpipeline.StateStoreConfig, bool) {
	if cfg.StateStore == nil {
		return nil, false
	}
	useRAGContext := cfg.ContextWindow > 0
	return &rtpipeline.StateStoreConfig{
		Store:          cfg.StateStore,
		ConversationID: cfg.ConversationID,
	}, useRAGContext
}

// collectPipelineStages assembles the ordered list of pipeline stages.
func collectPipelineStages(
	cfg *Config,
	stateStoreConfig *rtpipeline.StateStoreConfig,
	useRAGContext bool,
) ([]stage.Stage, error) {
	var stages []stage.Stage

	// 1. State store load stage - loads conversation history FIRST
	stages = appendStateStoreLoadStages(stages, cfg, stateStoreConfig, useRAGContext)

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

	// 4.6 Image preprocessing stage - resize/optimize images before provider
	if cfg.ImagePreprocessConfig != nil {
		stages = append(stages, stage.NewImagePreprocessStage(*cfg.ImagePreprocessConfig))
	}

	// 5. Provider stage - LLM calls with streaming and tool support
	providerStages, err := buildProviderStages(cfg)
	if err != nil {
		return nil, err
	}
	stages = append(stages, providerStages...)

	// 6. State store save stage - saves conversation state LAST
	stages = appendStateStoreSaveStages(stages, cfg, stateStoreConfig, useRAGContext)

	return stages, nil
}

// appendStateStoreLoadStages adds the appropriate state store load stage.
func appendStateStoreLoadStages(
	stages []stage.Stage,
	cfg *Config,
	stateStoreConfig *rtpipeline.StateStoreConfig,
	useRAGContext bool,
) []stage.Stage {
	if stateStoreConfig == nil {
		return stages
	}
	if useRAGContext {
		return append(stages, stage.NewContextAssemblyStage(&stage.ContextAssemblyConfig{
			StateStoreConfig: stateStoreConfig,
			RecentMessages:   cfg.ContextWindow,
			MessageIndex:     cfg.MessageIndex,
			RetrievalTopK:    cfg.RetrievalTopK,
		}))
	}
	return append(stages, stage.NewStateStoreLoadStage(stateStoreConfig))
}

// buildProviderStages returns the appropriate provider stage(s) based on config.
func buildProviderStages(cfg *Config) ([]stage.Stage, error) {
	if cfg.StreamInputProvider != nil {
		// ASM mode: Direct audio streaming to LLM
		logger.Debug("Using DuplexProviderStage for ASM mode")
		return []stage.Stage{stage.NewDuplexProviderStage(cfg.StreamInputProvider, cfg.StreamInputConfig)}, nil
	}
	if cfg.VADConfig != nil && cfg.STTService != nil && cfg.TTSService != nil {
		// VAD mode: build audio pipeline
		return buildVADPipelineStages(cfg)
	}
	if cfg.Provider != nil {
		// Text mode: standard LLM call
		providerConfig := &stage.ProviderConfig{
			MaxTokens:      cfg.MaxTokens,
			Temperature:    cfg.Temperature,
			ResponseFormat: cfg.ResponseFormat,
		}
		return []stage.Stage{stage.NewProviderStageWithHooks(
			cfg.Provider,
			cfg.ToolRegistry,
			cfg.ToolPolicy,
			providerConfig,
			cfg.EventEmitter,
			cfg.HookRegistry,
		)}, nil
	}
	return nil, nil
}

// appendStateStoreSaveStages adds the appropriate state store save stage.
func appendStateStoreSaveStages(
	stages []stage.Stage,
	cfg *Config,
	stateStoreConfig *rtpipeline.StateStoreConfig,
	useRAGContext bool,
) []stage.Stage {
	if stateStoreConfig == nil {
		return stages
	}
	if useRAGContext {
		return append(stages, stage.NewIncrementalSaveStage(&stage.IncrementalSaveConfig{
			StateStoreConfig:   stateStoreConfig,
			MessageIndex:       cfg.MessageIndex,
			Summarizer:         cfg.Summarizer,
			SummarizeThreshold: cfg.SummarizeThreshold,
			SummarizeBatchSize: cfg.SummarizeBatchSize,
		}))
	}
	return append(stages, stage.NewStateStoreSaveStage(stateStoreConfig))
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

	// Add summarizer if strategy is summarize
	if policy.Strategy == stage.TruncateSummarize && cfg.Summarizer != nil {
		policy.Summarizer = cfg.Summarizer
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
