package turnexecutors

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	arenaassertions "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// ScriptedExecutor executes turns where the user message is scripted (predefined)
type ScriptedExecutor struct {
	pipelineExecutor *PipelineExecutor
}

// NewScriptedExecutor creates a new executor for scripted user turns
func NewScriptedExecutor(pipelineExecutor *PipelineExecutor) *ScriptedExecutor {
	return &ScriptedExecutor{pipelineExecutor: pipelineExecutor}
}

// ExecuteTurn executes a scripted turn (user message from scenario + AI response)
func (e *ScriptedExecutor) ExecuteTurn(ctx context.Context, req TurnRequest) error {
	// Create user message with timestamp
	userMessage := types.Message{
		Role:      "user",
		Content:   req.ScriptedContent,
		Timestamp: time.Now(),
	}

	// Execute through pipeline (messages saved to StateStore)
	return e.pipelineExecutor.Execute(ctx, req, userMessage)
}

// ExecuteTurnStream executes a scripted turn with streaming
func (e *ScriptedExecutor) ExecuteTurnStream(
	ctx context.Context,
	req TurnRequest,
) (<-chan MessageStreamChunk, error) {
	outChan := make(chan MessageStreamChunk)

	go func() {
		defer close(outChan)

		// Handle non-streaming providers
		if e.handleNonStreamingProvider(ctx, req, outChan) {
			return
		}

		// Execute streaming pipeline
		e.executeStreamingPipeline(ctx, req, outChan)
	}()

	return outChan, nil
}

// handleNonStreamingProvider handles providers that don't support streaming
// Returns true if handled (caller should return)
func (e *ScriptedExecutor) handleNonStreamingProvider(
	ctx context.Context,
	req TurnRequest,
	outChan chan<- MessageStreamChunk,
) bool {
	if req.Provider.SupportsStreaming() {
		return false
	}

	err := e.ExecuteTurn(ctx, req)
	if err != nil {
		outChan <- MessageStreamChunk{Error: err}
		return true
	}

	finishReason := "stop"
	outChan <- MessageStreamChunk{
		Messages:     []types.Message{},
		FinishReason: &finishReason,
	}
	return true
}

// executeStreamingPipeline builds and executes the streaming pipeline
func (e *ScriptedExecutor) executeStreamingPipeline(
	ctx context.Context,
	req TurnRequest,
	outChan chan<- MessageStreamChunk,
) {
	userMessage := types.Message{
		Role:      "user",
		Content:   req.ScriptedContent,
		Timestamp: time.Now(),
	}
	messages := []types.Message{userMessage}

	// Build and execute pipeline
	middlewares := e.buildStreamingMiddlewares(req)
	pl := pipeline.NewPipeline(middlewares...)

	streamChan, err := pl.ExecuteStream(ctx, userMessage.Role, userMessage.Content)
	if err != nil {
		outChan <- MessageStreamChunk{Error: err}
		return
	}

	// Forward stream chunks
	e.forwardStreamChunks(streamChan, messages, outChan)
}

// buildStreamingMiddlewares constructs the middleware chain for streaming
func (e *ScriptedExecutor) buildStreamingMiddlewares(req TurnRequest) []pipeline.Middleware {
	baseVariables := buildBaseVariables(req.Region)

	providerConfig := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}

	var middlewares []pipeline.Middleware

	// StateStore Load middleware
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		middlewares = append(middlewares, middleware.StateStoreLoadMiddleware(storeConfig))
	}

	// Variable injection
	middlewares = append(middlewares, &variableInjectionMiddleware{variables: baseVariables})

	// Prompt, template, and provider middleware
	middlewares = append(middlewares,
		middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, baseVariables),
		middleware.TemplateMiddleware(),
		middleware.ProviderMiddleware(req.Provider, nil, nil, providerConfig),
	)

	// Dynamic validator middleware with suppression
	middlewares = append(middlewares,
		middleware.DynamicValidatorMiddlewareWithSuppression(validators.DefaultRegistry, true),
	)

	// StateStore Save middleware
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		middlewares = append(middlewares, middleware.StateStoreSaveMiddleware(storeConfig))
	}

	// Assertion middleware - validates turn-level assertions from scenario config
	// Listed after StateStore so assertions run first (middleware executes in reverse order)
	if len(req.Assertions) > 0 {
		assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
		middlewares = append(middlewares, arenaassertions.ArenaAssertionMiddleware(assertionRegistry, req.Assertions))
	}

	return middlewares
}

// forwardStreamChunks forwards stream chunks from pipeline to output channel
func (e *ScriptedExecutor) forwardStreamChunks(
	streamChan <-chan providers.StreamChunk,
	messages []types.Message,
	outChan chan<- MessageStreamChunk,
) {
	assistantIndex := 1
	var assistantMsg types.Message
	assistantMsg.Role = "assistant"

	for chunk := range streamChan {
		if chunk.Error != nil {
			outChan <- MessageStreamChunk{Messages: messages, Error: chunk.Error}
			return
		}

		if chunk.FinalResult != nil {
			break
		}

		assistantMsg = e.updateAssistantMessage(assistantMsg, chunk)
		messages = e.updateMessagesList(messages, assistantMsg, assistantIndex)

		outChan <- MessageStreamChunk{
			Messages:     messages,
			Delta:        chunk.Delta,
			MessageIndex: assistantIndex,
			TokenCount:   chunk.TokenCount,
			FinishReason: chunk.FinishReason,
		}

		if chunk.FinishReason != nil {
			break
		}
	}
}

// updateAssistantMessage updates assistant message with chunk data
func (e *ScriptedExecutor) updateAssistantMessage(
	msg types.Message,
	chunk providers.StreamChunk,
) types.Message {
	msg.Content = chunk.Content

	if len(chunk.ToolCalls) > 0 {
		msg.ToolCalls = make([]types.MessageToolCall, len(chunk.ToolCalls))
		for i, tc := range chunk.ToolCalls {
			msg.ToolCalls[i] = types.MessageToolCall{
				ID:   tc.ID,
				Name: tc.Name,
				Args: tc.Args,
			}
		}
	}

	return msg
}

// updateMessagesList updates the messages list with current assistant message
func (e *ScriptedExecutor) updateMessagesList(
	messages []types.Message,
	assistantMsg types.Message,
	assistantIndex int,
) []types.Message {
	if len(messages) == assistantIndex {
		return append(messages, assistantMsg)
	}
	messages[assistantIndex] = assistantMsg
	return messages
}

func buildBaseVariables(region string) map[string]string {
	baseVars := map[string]string{}
	if region != "" {
		baseVars["region"] = region
	}
	return baseVars
}
