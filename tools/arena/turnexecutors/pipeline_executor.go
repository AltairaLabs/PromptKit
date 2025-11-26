package turnexecutors

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	arenamiddleware "github.com/AltairaLabs/PromptKit/tools/arena/middleware"
)

// variableInjectionMiddleware injects variables into the execution context
type variableInjectionMiddleware struct {
	variables map[string]string
}

func (m *variableInjectionMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	execCtx.Variables = m.variables
	return next()
}

func (m *variableInjectionMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}

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
func buildContextPolicy(scenario *config.Scenario) *middleware.ContextBuilderPolicy {
	if scenario == nil || scenario.ContextPolicy == nil {
		return nil
	}

	return &middleware.ContextBuilderPolicy{
		TokenBudget:      scenario.ContextPolicy.TokenBudget,
		ReserveForOutput: scenario.ContextPolicy.ReserveForOutput,
		Strategy:         convertTruncationStrategy(scenario.ContextPolicy.Strategy),
		CacheBreakpoints: scenario.ContextPolicy.CacheBreakpoints,
	}
}

// buildStateStoreConfig creates state store config from request
func buildStateStoreConfig(req TurnRequest) *pipeline.StateStoreConfig {
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

// buildProviderConfig creates provider middleware config from request
func buildProviderConfig(req TurnRequest) *middleware.ProviderMiddlewareConfig {
	return &middleware.ProviderMiddlewareConfig{
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
func buildMediaConfig(conversationID string, mediaStorage storage.MediaStorageService) *middleware.MediaExternalizerConfig {
	if mediaStorage == nil {
		return nil
	}

	return &middleware.MediaExternalizerConfig{
		Enabled:         true,
		StorageService:  mediaStorage,
		SizeThresholdKB: 100, // Externalize media larger than 100KB
		DefaultPolicy:   "retain",
		RunID:           conversationID,
		ConversationID:  conversationID,
	}
}

// isMockProvider checks if provider is a mock type
func isMockProvider(provider providers.Provider) bool {
	_, isMock := provider.(*mock.Provider)
	_, isMockTool := provider.(*mock.Provider)
	return isMock || isMockTool
}

// convertTruncationStrategy converts string strategy to middleware.TruncationStrategy
func convertTruncationStrategy(strategy string) middleware.TruncationStrategy {
	switch strategy {
	case "oldest":
		return middleware.TruncateOldest
	case "fail":
		return middleware.TruncateFail
	case "summarize":
		return middleware.TruncateSummarize
	case "relevance":
		return middleware.TruncateLeastRelevant
	default:
		return middleware.TruncateOldest
	}
}

// buildMiddleware constructs the middleware chain for pipeline execution
func (e *PipelineExecutor) buildMiddleware(req TurnRequest, baseVariables map[string]string) []pipeline.Middleware {
	var pipelineMiddleware []pipeline.Middleware

	// 0. StateStore Load middleware
	if storeConfig := buildStateStoreConfig(req); storeConfig != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreLoadMiddleware(storeConfig))
	}

	// 1. Variable injection
	pipelineMiddleware = append(pipelineMiddleware, &variableInjectionMiddleware{variables: baseVariables})

	// 2. Prompt assembly
	pipelineMiddleware = append(pipelineMiddleware, middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, baseVariables))

	// 3. Context extraction
	pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.ScenarioContextExtractionMiddleware(req.Scenario))

	// 4. Template middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.TemplateMiddleware())

	// 5. Context builder (if policy exists)
	if contextPolicy := buildContextPolicy(req.Scenario); contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(contextPolicy))
	}

	// 5a. Mock scenario context (for mock providers only)
	if isMockProvider(req.Provider) {
		logger.Debug("Adding MockScenarioContextMiddleware for mock provider", "scenario_id", req.Scenario.ID)
		pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.MockScenarioContextMiddleware(req.Scenario))
	} else {
		logger.Debug("Provider is not a mock provider, skipping MockScenarioContextMiddleware", "provider_type", fmt.Sprintf("%T", req.Provider))
	}

	// 6. Provider middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
		req.Provider,
		e.toolRegistry,
		buildToolPolicy(req.Scenario),
		buildProviderConfig(req),
	))

	// 7. Media externalization
	if mediaConfig := buildMediaConfig(req.ConversationID, e.mediaStorage); mediaConfig != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.MediaExternalizerMiddleware(mediaConfig))
	}

	// 8. Dynamic validator (with suppression for arena)
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddlewareWithSuppression(validators.DefaultRegistry, true))

	// 9. StateStore Save middleware
	if req.StateStoreConfig != nil && req.ConversationID != "" {
		storeConfig := buildStateStoreConfig(req)
		pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.ArenaStateStoreSaveMiddleware(storeConfig))
	}

	// 10. Assertion middleware
	if len(req.Assertions) > 0 {
		assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
		pipelineMiddleware = append(pipelineMiddleware, arenaassertions.ArenaAssertionMiddleware(assertionRegistry, req.Assertions))
	}

	return pipelineMiddleware
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
	req TurnRequest,
	userMessage types.Message,
) error {
	// Build base variables and middleware chain
	baseVariables := buildBaseVariables(req.Region)
	pipelineMiddleware := e.buildMiddleware(req, baseVariables)
	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Log the call
	logger.LLMCall(req.Provider.ID(), "assistant", 1, req.Temperature, "max_tokens", req.MaxTokens)

	// Execute pipeline
	_, err := p.ExecuteWithMessage(ctx, userMessage)
	if err != nil {
		return e.handleExecutionError(req.Provider, err)
	}

	logger.Debug("Pipeline execution completed successfully")
	return nil
}
