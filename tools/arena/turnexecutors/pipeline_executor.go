package turnexecutors

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	arenastages "github.com/AltairaLabs/PromptKit/tools/arena/stages"
)

// PipelineExecutor executes conversations through the pipeline architecture.
// It handles both non-streaming and streaming execution, including multi-round tool calls.
type PipelineExecutor struct {
	toolRegistry *tools.Registry
	mediaStorage storage.MediaStorageService // Media storage service for externalization
}

// NewPipelineExecutor creates a new pipeline executor with the specified tool registry and media storage.
// The mediaStorage parameter enables automatic externalization of large media content to file storage.
func NewPipelineExecutor(toolRegistry *tools.Registry, mediaStorage storage.MediaStorageService) *PipelineExecutor {
	return &PipelineExecutor{
		toolRegistry: toolRegistry,
		mediaStorage: mediaStorage,
	}
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

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s (supported: openai, gemini)", providerName)
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
	logger.Info("🔧 buildStagePipeline called", "provider_type", fmt.Sprintf("%T", req.Provider))
	builder := stage.NewPipelineBuilder()

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
	stages = append(stages, arenastages.NewVariableInjectionStage(mergedVars))
	if len(req.Metadata) > 0 {
		stages = append(stages, arenastages.NewMetadataInjectionStage(req.Metadata))
	}

	// 2-4. Prompt assembly, context extraction, template
	stages = append(stages,
		stage.NewPromptAssemblyStage(req.PromptRegistry, req.TaskType, mergedVars),
		arenastages.NewScenarioContextExtractionStage(req.Scenario),
		stage.NewTemplateStage(),
	)

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
		stages = append(stages, stage.NewContextBuilderStage(contextPolicy))
	}

	// 6. Provider stage
	providerConfig := buildProviderConfig(req)
	stages = append(stages, stage.NewProviderStage(
		req.Provider,
		e.toolRegistry,
		buildToolPolicy(req.Scenario),
		providerConfig,
	))

	// 7. Media externalization (if configured)
	if mediaConfig := buildMediaConfig(req.ConversationID, e.mediaStorage); mediaConfig != nil {
		stages = append(stages, stage.NewMediaExternalizerStage(mediaConfig))
	}

	// 8. Dynamic validator
	stages = append(stages, stage.NewValidationStage(validators.DefaultRegistry, true))

	// 9. Assertion stage - must run before state store save
	if len(req.Assertions) > 0 {
		assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
		stages = append(stages, arenastages.NewArenaAssertionStage(assertionRegistry, req.Assertions))
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
