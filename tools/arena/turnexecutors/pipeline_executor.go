package turnexecutors

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
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
}

// NewPipelineExecutor creates a new pipeline executor
func NewPipelineExecutor(toolRegistry *tools.Registry) *PipelineExecutor {
	return &PipelineExecutor{
		toolRegistry: toolRegistry,
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
	// Build base variables for prompt assembly
	baseVariables := map[string]string{}
	if req.Region != "" {
		baseVariables["region"] = req.Region
	}

	// Build context policy from scenario if present
	var contextPolicy *middleware.ContextBuilderPolicy
	if req.Scenario.ContextPolicy != nil {
		contextPolicy = &middleware.ContextBuilderPolicy{
			TokenBudget:      req.Scenario.ContextPolicy.TokenBudget,
			ReserveForOutput: req.Scenario.ContextPolicy.ReserveForOutput,
			Strategy:         convertTruncationStrategy(req.Scenario.ContextPolicy.Strategy),
			CacheBreakpoints: req.Scenario.ContextPolicy.CacheBreakpoints,
		}
	}

	// Build pipeline with middleware
	var pipelineMiddleware []pipeline.Middleware

	// 0. StateStore Load middleware - loads conversation state (if configured)
	if req.StateStoreConfig != nil {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		pipelineMiddleware = append(pipelineMiddleware, middleware.StateStoreLoadMiddleware(storeConfig))
	}

	// 1. Middleware to inject variables (StateStore handles history when configured)
	pipelineMiddleware = append(pipelineMiddleware, &variableInjectionMiddleware{variables: baseVariables})

	// 2. Prompt assembly runs to load prompt config and populate SystemPrompt + AllowedTools
	pipelineMiddleware = append(pipelineMiddleware, middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, baseVariables))

	// 3. Context extraction populates Variables from scenario metadata + message analysis
	pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.ScenarioContextExtractionMiddleware(req.Scenario))

	// 4. Template middleware uses Variables populated by context extraction
	pipelineMiddleware = append(pipelineMiddleware, middleware.TemplateMiddleware())

	// 5. Add context builder middleware if we have a context policy
	if contextPolicy != nil {
		pipelineMiddleware = append(pipelineMiddleware, middleware.ContextBuilderMiddleware(contextPolicy))
	}

	// 5a. Mock scenario context middleware (only for MockProvider and MockToolProvider)
	// This adds scenario context to the execution context so mock providers can use scenario-specific responses
	if _, isMockProvider := req.Provider.(*mock.MockProvider); isMockProvider {
		logger.Debug("Adding MockScenarioContextMiddleware for MockProvider", "scenario_id", req.Scenario.ID)
		pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.MockScenarioContextMiddleware(req.Scenario))
	} else if _, isMockToolProvider := req.Provider.(*mock.MockToolProvider); isMockToolProvider {
		logger.Debug("Adding MockScenarioContextMiddleware for MockToolProvider", "scenario_id", req.Scenario.ID)
		pipelineMiddleware = append(pipelineMiddleware, arenamiddleware.MockScenarioContextMiddleware(req.Scenario))
	} else {
		logger.Debug("Provider is not a mock provider, skipping MockScenarioContextMiddleware", "provider_type", fmt.Sprintf("%T", req.Provider))
	}

	// 6. Provider middleware executes LLM and handles tool execution
	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}

	// Tool policy from scenario
	var toolPolicy *pipeline.ToolPolicy
	if req.Scenario != nil && req.Scenario.ToolPolicy != nil {
		toolPolicy = &pipeline.ToolPolicy{
			ToolChoice:          req.Scenario.ToolPolicy.ToolChoice,
			MaxRounds:           0, // Not supported in config, using unlimited
			MaxToolCallsPerTurn: req.Scenario.ToolPolicy.MaxToolCallsPerTurn,
			Blocklist:           req.Scenario.ToolPolicy.Blocklist,
		}
	}

	pipelineMiddleware = append(pipelineMiddleware, middleware.ProviderMiddleware(
		req.Provider,
		e.toolRegistry,
		toolPolicy,
		providerConfig,
	))

	// 7. Dynamic validator middleware - validates response against prompt-level validators (guardrails)
	// This runs after the provider creates the assistant message, validates it, and attaches results
	// Arena uses suppression mode so validators record failures without halting, enabling guardrail assertions
	pipelineMiddleware = append(pipelineMiddleware, middleware.DynamicValidatorMiddlewareWithSuppression(validators.DefaultRegistry, true))

	// 8. StateStore Save middleware (if configured) - saves conversation state
	// Listed before assertions so StateStore's next() is called first, but StateStore's save work
	// happens AFTER assertions (since work after next() executes in reverse order)
	var stateStoreSaveMiddleware pipeline.Middleware
	if req.StateStoreConfig != nil && req.ConversationID != "" {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		stateStoreSaveMiddleware = arenamiddleware.ArenaStateStoreSaveMiddleware(storeConfig)
		pipelineMiddleware = append(pipelineMiddleware, stateStoreSaveMiddleware)
	}

	// 9. Assertion middleware - validates turn-level assertions from scenario config
	// Listed after StateStore so assertions run first (after next() work executes in reverse order)
	// This ensures assertions modify message Meta before StateStore saves
	if len(req.Assertions) > 0 {
		assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
		pipelineMiddleware = append(pipelineMiddleware, arenaassertions.ArenaAssertionMiddleware(assertionRegistry, req.Assertions))
	}

	p := pipeline.NewPipeline(pipelineMiddleware...)

	// Log the call (StateStore manages history, we just log with message count 1)
	logger.LLMCall(req.Provider.ID(), "assistant", 1, req.Temperature, "max_tokens", req.MaxTokens)

	// Execute pipeline with the complete message (including Meta field with self-play metadata)
	// This preserves all message fields through execution without needing middleware injection
	_, err := p.ExecuteWithMessage(ctx, userMessage)
	if err != nil {
		// Check if this is a validation error
		if valErr, ok := err.(*pipeline.ValidationError); ok {
			// For now, we'll treat validation errors as fatal
			// In the future, we might want to return partial results with validation info
			logger.LLMError(req.Provider.ID(), "assistant", valErr)
			return fmt.Errorf("validation failed: %w", valErr)
		}

		// Other errors are fatal
		logger.LLMError(req.Provider.ID(), "assistant", err)
		return fmt.Errorf("pipeline execution failed: %w", err)
	}

	// Log response (cost info is in StateStore, but not easily accessible here)
	// For now, just log success
	logger.Debug("Pipeline execution completed successfully")

	// Messages are now in StateStore, no need to return them
	return nil
}
