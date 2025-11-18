package turnexecutors

import (
	"context"
	"fmt"

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
	mediaStorage storage.MediaStorageService
}

// NewPipelineExecutor creates a new pipeline executor
func NewPipelineExecutor(toolRegistry *tools.Registry, mediaStorage storage.MediaStorageService) *PipelineExecutor {
	return &PipelineExecutor{
		toolRegistry: toolRegistry,
		mediaStorage: mediaStorage,
	}
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
	// Build base variables and context policy
	baseVariables := e.buildBaseVariables(req)
	contextPolicy := e.buildContextPolicy(req)

	// Build complete middleware pipeline
	pipelineMiddleware := e.buildMiddlewarePipeline(req, baseVariables, contextPolicy)

	// Create and execute pipeline
	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Log the call
	logger.LLMCall(req.Provider.ID(), "assistant", 1, req.Temperature, "max_tokens", req.MaxTokens)

	// Execute pipeline
	_, err := p.ExecuteWithMessage(ctx, userMessage)
	if err != nil {
		return e.handlePipelineError(req, err)
	}

	logger.Debug("Pipeline execution completed successfully")
	return nil
}

// buildBaseVariables constructs the base variables map from request
func (e *PipelineExecutor) buildBaseVariables(req TurnRequest) map[string]string {
	baseVariables := make(map[string]string)

	// Add prompt config variable overrides from arena.yaml (highest priority)
	for k, v := range req.PromptVars {
		baseVariables[k] = v
	}

	// Add region (if not already set)
	if req.Region != "" {
		if _, exists := baseVariables["region"]; !exists {
			baseVariables["region"] = req.Region
		}
	}

	return baseVariables
}

// buildContextPolicy creates context policy from scenario configuration
func (e *PipelineExecutor) buildContextPolicy(req TurnRequest) *middleware.ContextBuilderPolicy {
	if req.Scenario.ContextPolicy == nil {
		return nil
	}

	return &middleware.ContextBuilderPolicy{
		TokenBudget:      req.Scenario.ContextPolicy.TokenBudget,
		ReserveForOutput: req.Scenario.ContextPolicy.ReserveForOutput,
		Strategy:         convertTruncationStrategy(req.Scenario.ContextPolicy.Strategy),
		CacheBreakpoints: req.Scenario.ContextPolicy.CacheBreakpoints,
	}
}

// buildMiddlewarePipeline constructs the complete middleware pipeline
func (e *PipelineExecutor) buildMiddlewarePipeline(
	req TurnRequest,
	baseVariables map[string]string,
	contextPolicy *middleware.ContextBuilderPolicy,
) []pipeline.Middleware {
	var pipelineMiddleware []pipeline.Middleware

	// 0. StateStore Load middleware
	e.addStateStoreLoadMiddleware(&pipelineMiddleware, req)

	// 1-4. Core prompt assembly and template middleware
	e.addPromptMiddleware(&pipelineMiddleware, req, baseVariables)

	// 5. Context builder middleware
	if contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(contextPolicy))
	}

	// 5a. Mock scenario context middleware
	e.addMockMiddleware(&pipelineMiddleware, req)

	// 6. Provider middleware
	e.addProviderMiddleware(&pipelineMiddleware, req)

	// 7. Validator middleware
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddlewareWithSuppression(validators.DefaultRegistry, true))

	// 8. StateStore Save middleware
	e.addStateStoreSaveMiddleware(&pipelineMiddleware, req)

	// 9. Assertion middleware
	e.addAssertionMiddleware(&pipelineMiddleware, req)

	return pipelineMiddleware
}

// addStateStoreLoadMiddleware adds StateStore load middleware if configured
func (e *PipelineExecutor) addStateStoreLoadMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest) {
	if req.StateStoreConfig == nil {
		return
	}

	storeConfig := &pipeline.StateStoreConfig{
		Store:          req.StateStoreConfig.Store,
		ConversationID: req.ConversationID,
		UserID:         req.StateStoreConfig.UserID,
		Metadata:       req.StateStoreConfig.Metadata,
	}
	*middlewares = append(*middlewares, middleware.StateStoreLoadMiddleware(storeConfig))
}

// addPromptMiddleware adds prompt assembly and template middleware
func (e *PipelineExecutor) addPromptMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest, baseVariables map[string]string) {
	// 1. Variable injection
	*middlewares = append(*middlewares, &variableInjectionMiddleware{variables: baseVariables})

	// 2. Prompt assembly
	*middlewares = append(*middlewares, middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, baseVariables))

	// 3. Context extraction
	*middlewares = append(*middlewares, arenamiddleware.ScenarioContextExtractionMiddleware(req.Scenario))

	// 4. Template middleware
	*middlewares = append(*middlewares, middleware.TemplateMiddleware())
}

// addMockMiddleware adds mock scenario context middleware for mock providers
func (e *PipelineExecutor) addMockMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest) {
	if _, isMockProvider := req.Provider.(*mock.MockProvider); isMockProvider {
		logger.Debug("Adding MockScenarioContextMiddleware for MockProvider", "scenario_id", req.Scenario.ID)
		*middlewares = append(*middlewares, arenamiddleware.MockScenarioContextMiddleware(req.Scenario))
		return
	}

	if _, isMockToolProvider := req.Provider.(*mock.MockToolProvider); isMockToolProvider {
		logger.Debug("Adding MockScenarioContextMiddleware for MockToolProvider", "scenario_id", req.Scenario.ID)
		*middlewares = append(*middlewares, arenamiddleware.MockScenarioContextMiddleware(req.Scenario))
		return
	}

	logger.Debug("Provider is not a mock provider, skipping MockScenarioContextMiddleware", "provider_type", fmt.Sprintf("%T", req.Provider))
}

// addProviderMiddleware adds provider middleware with tool policy
func (e *PipelineExecutor) addProviderMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest) {
	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}

	toolPolicy := e.buildToolPolicy(req)

	*middlewares = append(*middlewares, middleware.ProviderMiddleware(
		req.Provider,
		e.toolRegistry,
		toolPolicy,
		providerConfig,
	))

	// Media externalization middleware - externalizes large media to storage
	if e.mediaStorage != nil {
		mediaConfig := &middleware.MediaExternalizerConfig{
			Enabled:         true,
			StorageService:  e.mediaStorage,
			SizeThresholdKB: 100, // Externalize media larger than 100KB
			DefaultPolicy:   "retain",
			RunID:           req.ConversationID, // Use conversation ID as run ID
			ConversationID:  req.ConversationID,
		}
		*middlewares = append(*middlewares, middleware.MediaExternalizerMiddleware(mediaConfig))
	}
}

// buildToolPolicy creates tool policy from scenario configuration
func (e *PipelineExecutor) buildToolPolicy(req TurnRequest) *pipeline.ToolPolicy {
	if req.Scenario == nil || req.Scenario.ToolPolicy == nil {
		return nil
	}

	return &pipeline.ToolPolicy{
		ToolChoice:          req.Scenario.ToolPolicy.ToolChoice,
		MaxRounds:           0, // Not supported in config, using unlimited
		MaxToolCallsPerTurn: req.Scenario.ToolPolicy.MaxToolCallsPerTurn,
		Blocklist:           req.Scenario.ToolPolicy.Blocklist,
	}
}

// addStateStoreSaveMiddleware adds StateStore save middleware if configured
func (e *PipelineExecutor) addStateStoreSaveMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest) {
	if req.StateStoreConfig == nil || req.ConversationID == "" {
		return
	}

	storeConfig := &pipeline.StateStoreConfig{
		Store:          req.StateStoreConfig.Store,
		ConversationID: req.ConversationID,
		UserID:         req.StateStoreConfig.UserID,
		Metadata:       req.StateStoreConfig.Metadata,
	}
	*middlewares = append(*middlewares, arenamiddleware.ArenaStateStoreSaveMiddleware(storeConfig))
}

// addAssertionMiddleware adds assertion middleware if assertions are configured
func (e *PipelineExecutor) addAssertionMiddleware(middlewares *[]pipeline.Middleware, req TurnRequest) {
	if len(req.Assertions) == 0 {
		return
	}

	assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
	*middlewares = append(*middlewares, arenaassertions.ArenaAssertionMiddleware(assertionRegistry, req.Assertions))
}

// handlePipelineError handles errors from pipeline execution
func (e *PipelineExecutor) handlePipelineError(req TurnRequest, err error) error {
	if valErr, ok := err.(*pipeline.ValidationError); ok {
		logger.LLMError(req.Provider.ID(), "assistant", valErr)
		return fmt.Errorf("validation failed: %w", valErr)
	}

	logger.LLMError(req.Provider.ID(), "assistant", err)
	return fmt.Errorf("pipeline execution failed: %w", err)
}
