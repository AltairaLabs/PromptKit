package turnexecutors

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/media"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"
	runtimestatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
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

// buildProviderConfig creates provider config from request.
// When the state store implements runtimestatestore.MessageLog, it is wired for
// per-round write-through so tool-loop messages survive a mid-loop error.
func buildProviderConfig(req *TurnRequest) *stage.ProviderConfig {
	cfg := &stage.ProviderConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil && req.ConversationID != "" {
		if ml, ok := req.StateStoreConfig.Store.(runtimestatestore.MessageLog); ok {
			cfg.MessageLog = ml
			cfg.MessageLogConvID = req.ConversationID
		}
	}
	return cfg
}

// defaultArenaMaxCostUSD is the cost cap applied to every Arena turn when no
// explicit max_cost_usd is configured. 0/unset in the scenario always maps to
// this value — Arena never allows unlimited cost runs.
const defaultArenaMaxCostUSD = 2.00

// buildToolPolicy constructs tool policy from scenario config.
// It always returns a non-nil *pipeline.ToolPolicy with finite safety caps:
//   - MaxRounds: scenario value if >0, else 50
//   - MaxCostUSD: scenario value if >0, else defaultArenaMaxCostUSD ($2.00)
//   - MaxIdenticalToolCalls: scenario value if >0, else 3
func buildToolPolicy(scenario *config.Scenario) *pipeline.ToolPolicy {
	const defaultRounds = 50
	const defaultIdentical = 3

	policy := &pipeline.ToolPolicy{
		MaxRounds:             defaultRounds,
		MaxCostUSD:            defaultArenaMaxCostUSD,
		MaxIdenticalToolCalls: defaultIdentical,
	}

	if scenario == nil || scenario.ToolPolicy == nil {
		return policy
	}

	sp := scenario.ToolPolicy
	policy.ToolChoice = sp.ToolChoice
	policy.MaxToolCallsPerTurn = sp.MaxToolCallsPerTurn
	policy.Blocklist = sp.Blocklist

	if sp.MaxRounds > 0 {
		policy.MaxRounds = sp.MaxRounds
	}
	if sp.MaxCostUSD > 0 {
		policy.MaxCostUSD = sp.MaxCostUSD
	}
	if sp.MaxIdenticalToolCalls > 0 {
		policy.MaxIdenticalToolCalls = sp.MaxIdenticalToolCalls
	}

	return policy
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
			stage.NewStateStoreLoadStageWithTurnState(storeConfig, turnState),
			arenastages.NewTurnIndexStageWithTurnState(turnState),
		)
	}

	// 1-4. Variable provider, prompt assembly, context extraction, template.
	// For composition turns (RFC 0010), these stages are skipped: the
	// CompositionStage builds its own per-step sub-pipelines and there is no
	// top-level prompt_task to assemble.
	if req.ActiveComposition == nil {
		stages = append(stages,
			stage.NewVariableProviderStageWithVarsAndTurnState(mergedVars, nil, turnState),
			stage.NewPromptAssemblyStageWithTurnState(req.PromptRegistry, req.TaskType, mergedVars, turnState),
			arenastages.NewScenarioContextExtractionStageWithTurnState(req.Scenario, turnState),
			stage.NewTemplateStageWithTurnState(emitterFromRequest(req), turnState),
		)
	}

	if req.ActiveComposition == nil {
		// 4b. Append preloaded skill instructions to system_prompt so skills
		// marked preload: true are active from turn 1 without requiring the
		// model to call skill__activate.
		if e.preloadedSkillInstructions != "" {
			stages = append(stages,
				arenastages.NewSkillInstructionStageWithTurnState(e.preloadedSkillInstructions, turnState),
			)
		}

		// 4a. Mock scenario context (for mock providers only)
		if isMockProvider(req.Provider) {
			logger.Debug("Adding MockScenarioContext stage", "scenario_id", req.Scenario.ID)
			stages = append(stages, arenastages.NewMockScenarioContextStageWithTurnState(req.Scenario, turnState))
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
	}

	// 5b. Input recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionInput)
	// 5c. Input audio monitor tap (opt-in via AudioRouter); after recording so
	// the recording stage always sees every chunk before observers do.
	stages = appendMonitorTap(stages, req, stage.RecordingPositionInput)

	// 6. Provider/composition stage.
	// Composition turns (RFC 0010): run a CompositionStage that executes the
	// composition graph instead of the normal LLM ProviderStage.
	// Normal turns: consent + chaos tool hooks and pack-declared guardrail
	// provider hooks all live here. Guardrails run inline in ProviderStage's
	// hook chain, identically to SDK; no separate stage.
	if req.ActiveComposition != nil {
		emitter := emitterFromRequest(req)
		guardrailHooks := loadGuardrailHooks(req, mergedVars)
		hookReg := buildHookRegistry(req, guardrailHooks)
		// BaseMetadata propagates mock_scenario_id (and future per-turn metadata)
		// into composition sub-pipelines so the mock provider can key per-step
		// responses against the right scenario.
		baseMetadata := map[string]interface{}{}
		if req.Scenario != nil && req.Scenario.ID != "" && isMockProvider(req.Provider) {
			baseMetadata["mock_scenario_id"] = req.Scenario.ID
		}
		deps := stage.CompositionExecutorDeps{
			PromptRegistry: req.PromptRegistry,
			Provider:       req.Provider,
			ToolRegistry:   e.toolRegistry,
			Emitter:        emitter,
			HookRegistry:   hookReg,
			BaseVariables:  mergedVars,
			SchemaResolver: stage.NewFileSchemaResolver(req.BaseDir),
			BaseMetadata:   baseMetadata,
		}
		// RFC 0010 Task 5: when a per-run recorder is wired, build the
		// CompositionStage with it so step outputs, branch targets, and
		// parallel statuses are captured for composition_* assertions.
		if req.CompositionRecorder != nil {
			stages = append(stages, stage.NewCompositionStageWithRecorder(
				"composition", req.ActiveComposition, deps, req.CompositionRecorder,
			))
		} else {
			stages = append(stages, stage.NewCompositionStage("composition", req.ActiveComposition, deps))
		}
	} else {
		providerConfig := buildProviderConfig(req)
		guardrailHooks := loadGuardrailHooks(req, mergedVars)
		stages = append(stages,
			e.buildProviderStage(req, providerConfig, turnState, guardrailHooks),
		)
	}

	// 7a. Output recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionOutput)
	// 7b. Output audio monitor tap (opt-in via AudioRouter); after recording.
	stages = appendMonitorTap(stages, req, stage.RecordingPositionOutput)

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
		stages = append(stages, arenastages.NewArenaStateStoreSaveStageWithTurnState(storeConfig, turnState))
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
// Recording is gated on a non-nil EventStore — without one, the stage
// has no destination for its writes.
func appendRecordingStage(stages []stage.Stage, req *TurnRequest, position stage.RecordingPosition) []stage.Stage {
	if req.RecordingConfig == nil || req.EventStore == nil {
		return stages
	}
	cfg := *req.RecordingConfig
	cfg.Position = position
	cfg.SessionID = req.RunID
	cfg.ConversationID = req.ConversationID
	return append(stages, stage.NewRecordingStage(req.EventStore, cfg))
}

// appendMonitorTap adds a MonitorTap stage at the given position when an
// AudioRouter is wired into the request. No-op when req.AudioRouter is nil.
//
// Callers should invoke this AFTER appendRecordingStage so that recording
// always observes every audio chunk first; the monitor tap is observational
// and a misbehaving consumer (slow LocalSink, dropped SSE) cannot affect
// recording correctness.
func appendMonitorTap(stages []stage.Stage, req *TurnRequest, position stage.RecordingPosition) []stage.Stage {
	if req == nil || req.AudioRouter == nil {
		return stages
	}
	return append(stages, arenaaudio.NewMonitorTap(req.AudioRouter, arenaaudio.MonitorTapConfig{
		Position: position,
	}))
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

// buildHookRegistry assembles a *hooks.Registry from consent/chaos tool hooks
// and the caller-supplied guardrail provider hooks. Returns nil when there are
// no hooks (ProviderStage and CompositionStage treat nil as "no hooks").
func buildHookRegistry(req *TurnRequest, guardrailHooks []hooks.ProviderHook) *hooks.Registry {
	var toolHooks []hooks.ToolHook
	if len(req.ConsentOverrides) > 0 {
		toolHooks = append(toolHooks, consent.NewSimulationHook())
	}
	if req.ChaosConfig != nil {
		toolHooks = append(toolHooks, chaos.NewHook())
	}
	if len(toolHooks) == 0 && len(guardrailHooks) == 0 {
		return nil
	}
	opts := make([]hooks.Option, 0, len(toolHooks)+len(guardrailHooks))
	for _, h := range guardrailHooks {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	for _, h := range toolHooks {
		opts = append(opts, hooks.WithToolHook(h))
	}
	return hooks.NewRegistry(opts...)
}

// buildProviderStage creates a provider stage, attaching hooks for consent
// simulation, chaos injection, and pack-declared guardrails when configured.
// guardrailHooks are produced by guardrails.ValidatorsToHooks in the caller —
// passing them here keeps template lookup co-located with the rest of the
// pipeline construction.
func (e *PipelineExecutor) buildProviderStage(
	req *TurnRequest, providerConfig *stage.ProviderConfig, turnState *stage.TurnState,
	guardrailHooks []hooks.ProviderHook,
) stage.Stage {
	toolPolicy := buildToolPolicy(req.Scenario)
	emitter := emitterFromRequest(req)
	hookReg := buildHookRegistry(req, guardrailHooks)
	return stage.NewProviderStageWithTurnState(
		req.Provider, e.toolRegistry, toolPolicy, providerConfig, emitter, hookReg, turnState,
	)
}

// loadGuardrailHooks resolves pack-declared validators for this turn and
// wraps them as ProviderHooks via the shared factory. Returns nil (no hooks)
// when there's no prompt registry, no template, or the template has no
// validators — those are normal cases, not errors. A template-load failure
// is logged and treated the same as "no validators": the run continues with
// guardrails disabled rather than failing the entire pipeline construction,
// matching the previous GuardrailEvalStage's silent-no-op behavior.
func loadGuardrailHooks(req *TurnRequest, vars map[string]string) []hooks.ProviderHook {
	if req.PromptRegistry == nil {
		return nil
	}
	tmpl, err := req.PromptRegistry.LoadTemplate(req.TaskType, vars, "")
	if err != nil {
		logger.Warn("Skipping pack guardrails: template load failed",
			"task_type", req.TaskType, "error", err)
		return nil
	}
	if tmpl == nil || len(tmpl.Validators) == 0 {
		return nil
	}
	return guardrails.ValidatorsToHooks(tmpl.Validators)
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
		stages = append(stages, stage.NewStateStoreLoadStageWithTurnState(storeConfig, turnState))
		if cfg.IncludeTurnIndex {
			stages = append(stages, arenastages.NewTurnIndexStageWithTurnState(turnState))
		}
	}

	// Variable injection + prompt assembly
	stages = append(stages,
		stage.NewVariableProviderStageWithVarsAndTurnState(mergedVars, nil, turnState),
		stage.NewPromptAssemblyStageWithTurnState(req.PromptRegistry, req.TaskType, mergedVars, turnState),
	)

	// Scenario context extraction (scripted only)
	if cfg.IncludeScenarioContextExtraction {
		stages = append(stages, arenastages.NewScenarioContextExtractionStageWithTurnState(req.Scenario, turnState))
	}

	// Template
	stages = append(stages, stage.NewTemplateStageWithTurnState(emitterFromRequest(req), turnState))

	// Strip tool messages (self-play only)
	if cfg.IncludeStripToolMessages {
		stages = append(stages, arenastages.NewStripToolMessagesStage())
	}

	// Mock scenario context (for mock providers only)
	if isMockProvider(req.Provider) {
		stages = append(stages, arenastages.NewMockScenarioContextStageWithTurnState(req.Scenario, turnState))
	}

	// Context builder (if policy exists) — scripted path
	if cfg.IncludeScenarioContextExtraction {
		if contextPolicy := buildContextPolicy(req.Scenario); contextPolicy != nil {
			stages = append(stages, stage.NewContextBuilderStageWithTurnState(contextPolicy, turnState))
		}
	}

	// Input recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionInput)
	// Input audio monitor tap (opt-in via AudioRouter); after recording.
	stages = appendMonitorTap(stages, req, stage.RecordingPositionInput)

	// Provider stage — pack guardrails run inline as ProviderHooks (same as
	// SDK), so there is no separate guardrail-eval stage anymore. We always
	// resolve guardrail hooks here; buildProviderStage no-ops when there
	// are no tool hooks AND no guardrail hooks.
	providerConfig := buildProviderConfig(req)
	guardrailHooks := loadGuardrailHooks(req, mergedVars)
	if cfg.UseHooksProvider || len(guardrailHooks) > 0 {
		stages = append(stages, e.buildProviderStage(req, providerConfig, turnState, guardrailHooks))
	} else {
		stages = append(stages, stage.NewProviderStageWithTurnState(
			req.Provider, e.toolRegistry, buildToolPolicy(req.Scenario),
			providerConfig, emitterFromRequest(req), nil, turnState,
		))
	}

	// Output recording stage (opt-in via RecordingConfig)
	stages = appendRecordingStage(stages, req, stage.RecordingPositionOutput)
	// Output audio monitor tap (opt-in via AudioRouter); after recording.
	stages = appendMonitorTap(stages, req, stage.RecordingPositionOutput)

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
			stages = append(stages, arenastages.NewArenaStateStoreSaveStageWithTurnState(storeConfig, turnState))
		} else {
			stages = append(stages, stage.NewIncrementalSaveStageWithTurnState(
				&stage.IncrementalSaveConfig{StateStoreConfig: storeConfig},
				turnState,
			))
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
