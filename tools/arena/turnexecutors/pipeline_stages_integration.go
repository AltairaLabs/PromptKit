package turnexecutors

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/media"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/chaos"
	"github.com/AltairaLabs/PromptKit/tools/arena/consent"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
)

// PipelineExecutor executes conversations through the pipeline architecture.
// It handles both non-streaming and streaming execution, including multi-round tool calls.
type PipelineExecutor struct {
	toolRegistry               *tools.Registry
	mediaStorage               storage.MediaStorageService // Media storage service for externalization
	preloadedSkillInstructions string                      // Appended to system_prompt when non-empty
}

// NewPipelineExecutor creates a new pipeline executor with the specified tool registry and media storage.
// The mediaStorage parameter enables automatic externalization of large media content to file storage.
func NewPipelineExecutor(toolRegistry *tools.Registry, mediaStorage storage.MediaStorageService) *PipelineExecutor {
	return &PipelineExecutor{
		toolRegistry: toolRegistry,
		mediaStorage: mediaStorage,
	}
}

// SetPreloadedSkillInstructions sets the preloaded-skill instructions block that
// will be appended to the system prompt for every turn. Passing an empty string
// disables the stage.
func (e *PipelineExecutor) SetPreloadedSkillInstructions(instructions string) {
	e.preloadedSkillInstructions = instructions
}

// buildBaseVariables creates base variables map from request
func buildBaseVariables(region string) map[string]string {
	baseVariables := map[string]string{}
	if region != "" {
		baseVariables["region"] = region
	}
	return baseVariables
}

// buildContextPolicy constructs context policy from scenario config
func buildContextPolicy(scenario *config.Scenario) *stage.ContextBuilderPolicy {
	if scenario == nil || scenario.ContextPolicy == nil {
		return nil
	}

	policy := &stage.ContextBuilderPolicy{
		TokenBudget:      scenario.ContextPolicy.TokenBudget,
		ReserveForOutput: scenario.ContextPolicy.ReserveForOutput,
		Strategy:         convertTruncationStrategy(scenario.ContextPolicy.Strategy),
		CacheBreakpoints: scenario.ContextPolicy.CacheBreakpoints,
	}

	// Build relevance config if strategy is relevance and config is provided
	if policy.Strategy == stage.TruncateLeastRelevant && scenario.ContextPolicy.Relevance != nil {
		relevanceConfig := buildRelevanceConfig(scenario.ContextPolicy.Relevance)
		if relevanceConfig != nil {
			policy.RelevanceConfig = relevanceConfig
		}
	}

	return policy
}

// buildRelevanceConfig constructs the runtime RelevanceConfig from YAML config.
func buildRelevanceConfig(cfg *config.RelevanceConfig) *stage.RelevanceConfig {
	if cfg == nil {
		return nil
	}

	// Create embedding provider based on config
	embeddingProvider, err := createEmbeddingProvider(cfg.Provider, cfg.Model)
	if err != nil {
		logger.Warn("failed to create embedding provider, relevance truncation will fall back to oldest",
			"provider", cfg.Provider, "error", err)
		return nil
	}

	relevance := &stage.RelevanceConfig{
		EmbeddingProvider:    embeddingProvider,
		MinRecentMessages:    cfg.MinRecentMessages,
		SimilarityThreshold:  cfg.SimilarityThreshold,
		QuerySource:          convertQuerySource(cfg.QuerySource),
		LastNCount:           cfg.LastNCount,
		CustomQuery:          cfg.CustomQuery,
		CacheEmbeddings:      cfg.CacheEmbeddings,
		AlwaysKeepSystemRole: true, // Default to true
	}

	// Override AlwaysKeepSystemRole if explicitly set
	if cfg.AlwaysKeepSystemRole != nil {
		relevance.AlwaysKeepSystemRole = *cfg.AlwaysKeepSystemRole
	}

	return relevance
}

// createEmbeddingProvider creates an embedding provider based on the provider name.
func createEmbeddingProvider(providerName, model string) (providers.EmbeddingProvider, error) {
	switch providerName {
	case "openai":
		opts := []openai.EmbeddingOption{}
		if model != "" {
			opts = append(opts, openai.WithEmbeddingModel(model))
		}
		return openai.NewEmbeddingProvider(opts...)

	case "gemini":
		opts := []gemini.EmbeddingOption{}
		if model != "" {
			opts = append(opts, gemini.WithGeminiEmbeddingModel(model))
		}
		return gemini.NewEmbeddingProvider(opts...)

	case "voyageai", "voyage":
		opts := []voyageai.EmbeddingOption{}
		if model != "" {
			opts = append(opts, voyageai.WithModel(model))
		}
		return voyageai.NewEmbeddingProvider(opts...)

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s (supported: openai, gemini, voyageai)", providerName)
	}
}

// convertQuerySource converts string query source to stage.QuerySourceType.
func convertQuerySource(source string) stage.QuerySourceType {
	switch source {
	case "last_user":
		return stage.QuerySourceLastUser
	case "last_n":
		return stage.QuerySourceLastN
	case "custom":
		return stage.QuerySourceCustom
	default:
		return stage.QuerySourceLastUser
	}
}

// buildStateStoreConfig creates state store config from request
func buildStateStoreConfig(req *TurnRequest) *pipeline.StateStoreConfig {
	if req.StateStoreConfig == nil {
		return nil
	}

	return &pipeline.StateStoreConfig{
		Store:          req.StateStoreConfig.Store,
		ConversationID: req.ConversationID,
		UserID:         req.StateStoreConfig.UserID,
		Metadata:       req.StateStoreConfig.Metadata,
	}
}

// buildProviderConfig creates provider config from request
func buildProviderConfig(req *TurnRequest) *stage.ProviderConfig {
	return &stage.ProviderConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}
}

// buildToolPolicy constructs tool policy from scenario config
func buildToolPolicy(scenario *config.Scenario) *pipeline.ToolPolicy {
	if scenario == nil || scenario.ToolPolicy == nil {
		return nil
	}

	return &pipeline.ToolPolicy{
		ToolChoice:          scenario.ToolPolicy.ToolChoice,
		MaxRounds:           0, // Not supported in config, using unlimited
		MaxToolCallsPerTurn: scenario.ToolPolicy.MaxToolCallsPerTurn,
		Blocklist:           scenario.ToolPolicy.Blocklist,
	}
}

// buildMediaConfig creates media externalizer config
func buildMediaConfig(conversationID string, mediaStorage storage.MediaStorageService) *stage.MediaExternalizerConfig {
	if mediaStorage == nil {
		return nil
	}

	return &stage.MediaExternalizerConfig{
		Enabled:         true,
		StorageService:  mediaStorage,
		SizeThresholdKB: mediaExternalizerThresholdKB,
		DefaultPolicy:   "retain",
		RunID:           conversationID,
		ConversationID:  conversationID,
	}
}

// buildMediaConvertConfig creates media conversion config from provider capabilities.
// Returns nil if the provider doesn't support multimodal or has no format restrictions.
func buildMediaConvertConfig(provider providers.Provider) *stage.MediaConvertConfig {
	// Check if provider supports multimodal
	mp := providers.GetMultimodalProvider(provider)
	if mp == nil {
		return nil
	}

	caps := mp.GetMultimodalCapabilities()

	// Only create config if provider has format restrictions
	hasFormats := len(caps.AudioFormats) > 0 || len(caps.ImageFormats) > 0 || len(caps.VideoFormats) > 0
	if !hasFormats {
		return nil
	}

	cfg := stage.DefaultMediaConvertConfig()
	cfg.TargetAudioFormats = caps.AudioFormats
	cfg.TargetImageFormats = caps.ImageFormats
	cfg.TargetVideoFormats = caps.VideoFormats
	cfg.AudioConverterConfig = media.DefaultAudioConverterConfig()
	cfg.PassthroughOnError = true // Don't fail if conversion fails, let provider handle it

	logger.Debug("Media conversion configured",
		"audio_formats", caps.AudioFormats,
		"image_formats", caps.ImageFormats,
		"video_formats", caps.VideoFormats,
	)

	return &cfg
}

// isMockProvider checks if provider is a mock type
func isMockProvider(provider providers.Provider) bool {
	_, isMock := provider.(*mock.Provider)
	_, isMockTool := provider.(*mock.ToolProvider)
	return isMock || isMockTool
}

// convertTruncationStrategy converts string strategy to stage.TruncationStrategy
func convertTruncationStrategy(strategy string) stage.TruncationStrategy {
	switch strategy {
	case "oldest":
		return stage.TruncateOldest
	case "fail":
		return stage.TruncateFail
	case "summarize":
		return stage.TruncateSummarize
	case "relevance":
		return stage.TruncateLeastRelevant
	default:
		return stage.TruncateOldest
	}
}

// buildStagePipeline constructs a stage-based pipeline for execution.
// This replaces the middleware chain with streaming stages for better performance.
func (e *PipelineExecutor) buildStagePipeline(
	req *TurnRequest, baseVariables map[string]string,
) (*stage.StreamPipeline, error) {
	logger.Debug("Building stage pipeline", "provider_type", fmt.Sprintf("%T", req.Provider))
	builder := stage.NewPipelineBuilder()
	turnState := stage.NewTurnState()

	// Merge prompt vars into base variables
	mergedVars := map[string]string{}
	for k, v := range baseVariables {
		mergedVars[k] = v
	}
	for k, v := range req.PromptVars {
		mergedVars[k] = v
	}

	var stages []stage.Stage

	// 0. State store load + turn index
	if storeConfig := buildStateStoreConfig(req); storeConfig != nil {
		stages = append(stages,
			stage.NewStateStoreLoadStage(storeConfig),
			arenastages.NewTurnIndexStage(),
		)
	}

	// 1. Variable and metadata injection stages
	stages = append(stages, stage.NewVariableProviderStageWithVars(mergedVars, nil))
	if len(req.Metadata) > 0 {
		stages = append(stages, arenastages.NewMetadataInjectionStage(req.Metadata))
	}

	// 2-4. Prompt assembly, context extraction, template
	stages = append(stages,
		stage.NewPromptAssemblyStageWithTurnState(req.PromptRegistry, req.TaskType, mergedVars, turnState),
		arenastages.NewScenarioContextExtractionStage(req.Scenario),
		stage.NewTemplateStageWithTurnState(emitterFromRequest(req), turnState),
	)

	// 4b. Append preloaded skill instructions to system_prompt so skills
	// marked preload: true are active from turn 1 without requiring the
	// model to call skill__activate.
	if e.preloadedSkillInstructions != "" {
		stages = append(stages, arenastages.NewSkillInstructionStage(e.preloadedSkillInstructions))
	}

	// 4a. Mock scenario context (for mock providers only)
	if isMockProvider(req.Provider) {
		logger.Debug("Adding MockScenarioContext stage", "scenario_id", req.Scenario.ID)
		stages = append(stages, arenastages.NewMockScenarioContextStage(req.Scenario))
	} else {
		logger.Debug("Skipping MockScenarioContext stage - not a mock provider",
			"provider_type", fmt.Sprintf("%T", req.Provider))
	}

	// 5. Context builder (if policy exists)
	if contextPolicy := buildContextPolicy(req.Scenario); contextPolicy != nil {
		stages = append(stages, stage.NewContextBuilderStageWithTurnState(contextPolicy, turnState))
	}

	// 5a. Media conversion stage - converts media to provider-supported formats
	if mediaConvertConfig := buildMediaConvertConfig(req.Provider); mediaConvertConfig != nil {
		stages = append(stages, stage.NewMediaConvertStage(mediaConvertConfig))
	}

	// 5b. Input recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionInput)

	// 6. Provider stage (with consent simulation hook if overrides are present)
	// 7. Guardrail evaluation (evaluative, not enforcement — records pass/fail for assertions)
	providerConfig := buildProviderConfig(req)
	stages = append(stages,
		e.buildProviderStage(req, providerConfig, turnState),
		arenastages.NewGuardrailEvalStageWithTurnState(turnState),
	)

	// 7a. Output recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionOutput)

	// 7b. Media externalization (if configured)
	if mediaConfig := buildMediaConfig(req.ConversationID, e.mediaStorage); mediaConfig != nil {
		stages = append(stages, stage.NewMediaExternalizerStage(mediaConfig))
	}

	// 8. Assertion stage - must run before state store save
	if len(req.Assertions) > 0 {
		assertionStage := arenastages.NewArenaAssertionStage(req.Assertions)
		if runner, ok := req.TurnEvalRunner.(arenastages.TurnEvalRunner); ok {
			assertionStage.WithTurnEvalRunner(runner, req.ConversationID)
		}
		stages = append(stages, assertionStage)
	}

	// 10. Arena state store save - saves messages with assertion metadata
	if req.StateStoreConfig != nil && req.ConversationID != "" {
		storeConfig := buildStateStoreConfig(req)
		stages = append(stages, arenastages.NewArenaStateStoreSaveStage(storeConfig))
	}

	// Chain all stages together
	return builder.Chain(stages...).Build()
}

// handleExecutionError processes pipeline execution errors
func (e *PipelineExecutor) handleExecutionError(provider providers.Provider, err error) error {
	if valErr, ok := err.(*pipeline.ValidationError); ok {
		logger.LLMError(provider.ID(), "assistant", valErr)
		return fmt.Errorf("validation failed: %w", valErr)
	}

	logger.LLMError(provider.ID(), "assistant", err)
	return fmt.Errorf("pipeline execution failed: %w", err)
}

// appendRecordingStage adds a RecordingStage if recording is configured.
func appendRecordingStage(stages []stage.Stage, req *TurnRequest, position stage.RecordingPosition) []stage.Stage {
	if req.RecordingConfig == nil || req.EventBus == nil {
		return stages
	}
	cfg := *req.RecordingConfig
	cfg.Position = position
	cfg.SessionID = req.RunID
	cfg.ConversationID = req.ConversationID
	return append(stages, stage.NewRecordingStage(req.EventBus, cfg))
}

// emitterFromRequest creates an event emitter from a TurnRequest's event bus.
// Returns nil if no event bus is configured, which is safe — ProviderStage
// treats a nil emitter as "no telemetry".
func emitterFromRequest(req *TurnRequest) *events.Emitter {
	if req.EventBus == nil {
		return nil
	}
	return events.NewEmitter(req.EventBus, req.RunID, req.RunID, req.ConversationID)
}

// buildProviderStage creates a provider stage, attaching hooks
// for consent simulation and/or chaos injection when configured.
func (e *PipelineExecutor) buildProviderStage(
	req *TurnRequest, providerConfig *stage.ProviderConfig, turnState *stage.TurnState,
) stage.Stage {
	toolPolicy := buildToolPolicy(req.Scenario)
	emitter := emitterFromRequest(req)

	var toolHooks []hooks.ToolHook
	if len(req.ConsentOverrides) > 0 {
		toolHooks = append(toolHooks, consent.NewSimulationHook())
	}
	if req.ChaosConfig != nil {
		toolHooks = append(toolHooks, chaos.NewHook())
	}

	var hookReg *hooks.Registry
	if len(toolHooks) > 0 {
		var opts []hooks.Option
		for _, h := range toolHooks {
			opts = append(opts, hooks.WithToolHook(h))
		}
		hookReg = hooks.NewRegistry(opts...)
	}
	return stage.NewProviderStageWithTurnState(
		req.Provider, e.toolRegistry, toolPolicy, providerConfig, emitter, hookReg, turnState,
	)
}

// Execute runs the conversation through the pipeline and returns the new messages generated.
// This is the new flattened API that works directly with message lists.
//
// Input: history (existing conversation) + userMessage (new user input)
// Output: all new messages generated (assistant messages, tool results, etc.)
func (e *PipelineExecutor) Execute(
	ctx context.Context,
	req *TurnRequest,
	userMessage *types.Message,
) error {
	// Inject consent overrides into context if present
	if len(req.ConsentOverrides) > 0 {
		ctx = consent.WithConsentOverrides(ctx, req.ConsentOverrides)
		ctx = consent.WithToolRegistry(ctx, e.toolRegistry)
	}

	// Inject chaos config into context if present
	if req.ChaosConfig != nil {
		ctx = chaos.WithConfig(ctx, req.ChaosConfig)
	}

	// Build base variables and stage pipeline
	baseVariables := buildBaseVariables(req.Region)
	p, err := e.buildStagePipeline(req, baseVariables)
	if err != nil {
		return fmt.Errorf("failed to build stage pipeline: %w", err)
	}

	// Log the call
	logger.LLMCall(req.Provider.ID(), "assistant", 1, req.Temperature, "max_tokens", req.MaxTokens)

	// Create input element from user message
	inputElem := stage.StreamElement{
		Message: userMessage,
		Metadata: map[string]interface{}{
			"run_id":          req.RunID,
			"conversation_id": req.ConversationID,
		},
	}

	// Execute pipeline synchronously
	_, err = p.ExecuteSync(ctx, inputElem)
	if err != nil {
		return e.handleExecutionError(req.Provider, err)
	}

	logger.Debug("Stage pipeline execution completed successfully")
	return nil
}

// StreamingStagesConfig controls which optional stages are included in the
// streaming pipeline built by buildCommonStreamingStages.
type StreamingStagesConfig struct {
	// IncludeTurnIndex adds a TurnIndex stage after StateStore load (self-play).
	IncludeTurnIndex bool
	// IncludeScenarioContextExtraction adds ScenarioContextExtraction after PromptAssembly (scripted).
	IncludeScenarioContextExtraction bool
	// IncludeStripToolMessages adds StripToolMessages after Template (self-play).
	IncludeStripToolMessages bool
	// IncludeGuardrailEval adds a GuardrailEval stage after the provider (scripted).
	IncludeGuardrailEval bool
	// IncludeMediaExternalizer adds media externalization (scripted).
	IncludeMediaExternalizer bool
	// IncludeAssertions adds the assertion stage (scripted).
	IncludeAssertions bool
	// UseArenaStateStoreSave uses ArenaStateStoreSave instead of StateStoreSave.
	UseArenaStateStoreSave bool
	// UseHooksProvider uses buildProviderStage (which wires hooks) instead of plain NewProviderStage.
	UseHooksProvider bool
}

// buildCommonStreamingStages constructs the stage pipeline for streaming execution.
// Both ScriptedExecutor and SelfPlayExecutor share the same core sequence;
// the StreamingStagesConfig toggles executor-specific stages.
//
//nolint:gocognit // Consolidates two near-duplicate methods; linear config-driven branching is intentional
func (e *PipelineExecutor) buildCommonStreamingStages(
	req *TurnRequest,
	cfg StreamingStagesConfig,
) (*stage.StreamPipeline, error) {
	mergedVars := mergePromptVars(req)
	builder := stage.NewPipelineBuilder()
	turnState := stage.NewTurnState()
	var stages []stage.Stage

	// StateStore Load stage
	if hasStateStore(req) {
		storeConfig := buildStateStoreConfig(req)
		stages = append(stages, stage.NewStateStoreLoadStage(storeConfig))
		if cfg.IncludeTurnIndex {
			stages = append(stages, arenastages.NewTurnIndexStage())
		}
	}

	// Variable injection
	stages = append(stages, stage.NewVariableProviderStageWithVars(mergedVars, nil))
	if len(req.Metadata) > 0 {
		stages = append(stages, arenastages.NewMetadataInjectionStage(req.Metadata))
	}

	// Prompt assembly
	stages = append(stages,
		stage.NewPromptAssemblyStageWithTurnState(req.PromptRegistry, req.TaskType, mergedVars, turnState))

	// Scenario context extraction (scripted only)
	if cfg.IncludeScenarioContextExtraction {
		stages = append(stages, arenastages.NewScenarioContextExtractionStage(req.Scenario))
	}

	// Template
	stages = append(stages, stage.NewTemplateStageWithTurnState(emitterFromRequest(req), turnState))

	// Strip tool messages (self-play only)
	if cfg.IncludeStripToolMessages {
		stages = append(stages, arenastages.NewStripToolMessagesStage())
	}

	// Mock scenario context (for mock providers only)
	if isMockProvider(req.Provider) {
		stages = append(stages, arenastages.NewMockScenarioContextStage(req.Scenario))
	}

	// Context builder (if policy exists) — scripted path
	if cfg.IncludeScenarioContextExtraction {
		if contextPolicy := buildContextPolicy(req.Scenario); contextPolicy != nil {
			stages = append(stages, stage.NewContextBuilderStageWithTurnState(contextPolicy, turnState))
		}
	}

	// Input recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionInput)

	// Provider stage
	providerConfig := buildProviderConfig(req)
	if cfg.UseHooksProvider {
		stages = append(stages, e.buildProviderStage(req, providerConfig, turnState))
	} else {
		stages = append(stages, stage.NewProviderStageWithTurnState(
			req.Provider, e.toolRegistry, buildToolPolicy(req.Scenario),
			providerConfig, emitterFromRequest(req), nil, turnState,
		))
	}

	// Guardrail evaluation (scripted only)
	if cfg.IncludeGuardrailEval {
		stages = append(stages, arenastages.NewGuardrailEvalStageWithTurnState(turnState))
	}

	// Output recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionOutput)

	// Media externalization (scripted only)
	if cfg.IncludeMediaExternalizer && e.mediaStorage != nil {
		mediaConfig := buildMediaConfig(req.ConversationID, e.mediaStorage)
		stages = append(stages, stage.NewMediaExternalizerStage(mediaConfig))
	}

	// Assertion stage (scripted only)
	if cfg.IncludeAssertions && len(req.Assertions) > 0 {
		assertionStage := arenastages.NewArenaAssertionStage(req.Assertions)
		if runner, ok := req.TurnEvalRunner.(arenastages.TurnEvalRunner); ok {
			assertionStage.WithTurnEvalRunner(runner, req.ConversationID)
		}
		stages = append(stages, assertionStage)
	}

	// State store save
	if hasStateStore(req) {
		storeConfig := buildStateStoreConfig(req)
		if cfg.UseArenaStateStoreSave {
			stages = append(stages, arenastages.NewArenaStateStoreSaveStage(storeConfig))
		} else {
			stages = append(stages, stage.NewStateStoreSaveStage(storeConfig))
		}
	}

	return builder.Chain(stages...).Build()
}

// mergePromptVars merges base variables (from region) with request prompt vars.
func mergePromptVars(req *TurnRequest) map[string]string {
	baseVariables := buildBaseVariables(req.Region)
	mergedVars := make(map[string]string, len(baseVariables)+len(req.PromptVars))
	for k, v := range baseVariables {
		mergedVars[k] = v
	}
	for k, v := range req.PromptVars {
		mergedVars[k] = v
	}
	return mergedVars
}

// hasStateStore returns true if the request has a valid state store configuration.
func hasStateStore(req *TurnRequest) bool {
	return req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != ""
}
