// Package pipeline provides internal pipeline construction for the SDK.
package pipeline

import (
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
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

	// MessageLog for per-round write-through during tool loops (optional).
	// When set, the provider stage persists messages per-round and the
	// save stage skips message append.
	MessageLog statestore.MessageLog

	// CompactionEnabled controls context compaction in tool loops.
	// nil = default (enabled), false = disabled.
	CompactionEnabled *bool

	// CompactionStrategy replaces the default compactor entirely.
	// Mutually exclusive with CompactionRules.
	CompactionStrategy stage.CompactionStrategy

	// CompactionRules configures custom rules on the default ContextCompactor.
	// Mutually exclusive with CompactionStrategy.
	CompactionRules []stage.CompactionRule

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

	// VideoStreamConfig configures frame rate limiting for realtime video streaming.
	// When non-nil and TargetFPS > 0, a FrameRateLimitStage is added before the provider stage.
	VideoStreamConfig *stage.FrameRateLimitConfig

	// EventEmitter for emitting provider call events (optional)
	// When provided, ProviderStage will emit ProviderCallStarted/Completed/Failed events
	EventEmitter *events.Emitter

	// HookRegistry for policy enforcement hooks (optional)
	// When provided, ProviderStage will use hooks for provider call, chunk, and tool interception
	HookRegistry *hooks.Registry

	// MemoryRetriever for automatic memory RAG injection (optional).
	// When set, a MemoryRetrievalStage is added before the provider stage.
	MemoryRetriever memory.Retriever

	// MemoryExtractor for automatic memory extraction (optional).
	// When set, a MemoryExtractionStage is added after the provider stage.
	MemoryExtractor memory.Extractor

	// MemoryStore for memory persistence (required if Retriever or Extractor set).
	MemoryStore memory.Store

	// MemoryScope for memory isolation.
	MemoryScope map[string]string

	// ExecutionTimeout overrides the default pipeline execution timeout.
	// When non-nil, the pointed-to duration is used instead of the default 30s.
	// A zero value disables timeout entirely.
	ExecutionTimeout *time.Duration

	// RecordingConfig enables recording stages in the pipeline.
	// When set, input and output RecordingStages are inserted to capture
	// full binary content for session replay.
	RecordingConfig *stage.RecordingStageConfig

	// RecordingEventBus is the event bus used by recording stages.
	// Required when RecordingConfig is set.
	RecordingEventBus events.Bus
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
	if cfg.ExecutionTimeout != nil {
		pipelineConfig := stage.DefaultPipelineConfig()
		pipelineConfig.ExecutionTimeout = *cfg.ExecutionTimeout
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

	// 4.1 Input recording stage - captures user input with full binary data
	if cfg.RecordingConfig != nil && cfg.RecordingEventBus != nil {
		inputCfg := *cfg.RecordingConfig
		inputCfg.Position = stage.RecordingPositionInput
		stages = append(stages, stage.NewRecordingStage(cfg.RecordingEventBus, inputCfg))
	}

	// 4.5 Context builder stage - manages token budget and truncation
	if cfg.TokenBudget > 0 {
		contextPolicy := buildContextBuilderPolicy(cfg)
		stages = append(stages, stage.NewContextBuilderStage(contextPolicy))
	}

	// 4.6 Image preprocessing stage - resize/optimize images before provider
	if cfg.ImagePreprocessConfig != nil {
		stages = append(stages, stage.NewImagePreprocessStage(*cfg.ImagePreprocessConfig))
	}

	// 4.7 Frame rate limiting stage - drop excess video/image frames before provider
	if cfg.VideoStreamConfig != nil && cfg.VideoStreamConfig.TargetFPS > 0 {
		stages = append(stages, stage.NewFrameRateLimitStage(*cfg.VideoStreamConfig))
	}

	// 4.8 Memory retrieval stage - inject relevant memories before provider
	if cfg.MemoryRetriever != nil && cfg.MemoryStore != nil {
		stages = append(stages, stage.NewMemoryRetrievalStage(
			cfg.MemoryRetriever, cfg.MemoryStore, cfg.MemoryScope))
	}

	// 5. Provider stage - LLM calls with streaming and tool support
	providerStages, err := buildProviderStages(cfg)
	if err != nil {
		return nil, err
	}
	stages = append(stages, providerStages...)

	// 5.5 Output recording stage - captures assistant output with full binary data
	if cfg.RecordingConfig != nil && cfg.RecordingEventBus != nil {
		outputCfg := *cfg.RecordingConfig
		outputCfg.Position = stage.RecordingPositionOutput
		stages = append(stages, stage.NewRecordingStage(cfg.RecordingEventBus, outputCfg))
	}

	// 5.7 Memory extraction stage - extract memories from conversation
	if cfg.MemoryExtractor != nil && cfg.MemoryStore != nil {
		stages = append(stages, stage.NewMemoryExtractionStage(
			cfg.MemoryExtractor, cfg.MemoryStore, cfg.MemoryScope))
	}

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
			HasTokenBudget:   cfg.TokenBudget > 0,
			HasContextWindow: cfg.ContextWindow > 0,
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
			MaxTokens:        cfg.MaxTokens,
			Temperature:      cfg.Temperature,
			ResponseFormat:   cfg.ResponseFormat,
			MessageLog:       cfg.MessageLog,
			MessageLogConvID: cfg.ConversationID,
		}
		// Configure compaction strategy
		if cfg.CompactionEnabled == nil || *cfg.CompactionEnabled {
			providerConfig.Compactor = buildCompactionStrategy(cfg)
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
			MessageLog:         cfg.MessageLog,
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

// buildCompactionStrategy creates the appropriate compaction strategy from config.
func buildCompactionStrategy(cfg *Config) stage.CompactionStrategy {
	// User-provided strategy takes precedence
	if cfg.CompactionStrategy != nil {
		return cfg.CompactionStrategy
	}

	budgetTokens := stage.DefaultBudgetTokens
	if cwp, ok := cfg.Provider.(providers.ContextWindowProvider); ok {
		if v := cwp.MaxContextTokens(); v > 0 {
			budgetTokens = v
		}
	}

	compactor := &stage.ContextCompactor{
		BudgetTokens: budgetTokens,
	}

	// User-provided rules replace the defaults
	if len(cfg.CompactionRules) > 0 {
		compactor.Rules = cfg.CompactionRules
	}

	return compactor
}
