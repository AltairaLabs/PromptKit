package turnexecutors

import (
	"context"
	"fmt"
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
	// Build user message from scripted content or parts
	userMessage, err := e.buildUserMessage(req)
	if err != nil {
		return err
	}

	// Execute through pipeline (messages saved to StateStore)
	return e.pipelineExecutor.Execute(ctx, req, userMessage)
}

// buildUserMessage creates a user message from either ScriptedContent or ScriptedParts
func (e *ScriptedExecutor) buildUserMessage(req TurnRequest) (types.Message, error) {
	userMessage := types.Message{
		Role:      "user",
		Timestamp: time.Now(),
	}

	// If Parts are provided, use multimodal content (takes precedence)
	if len(req.ScriptedParts) > 0 {
		// Use the base directory from the request (resolved from config directory)
		baseDir := req.BaseDir

		// Create HTTP loader for URL-based media (30 second timeout, 50MB max)
		httpLoader := NewHTTPMediaLoader(30*time.Second, 50*1024*1024)

		parts, err := ConvertTurnPartsToMessageParts(context.Background(), req.ScriptedParts, baseDir, httpLoader, nil)
		if err != nil {
			return types.Message{}, fmt.Errorf("failed to convert multimodal parts: %w", err)
		}
		userMessage.Parts = parts
	} else {
		// Fall back to legacy text-only content
		userMessage.Content = req.ScriptedContent
	}

	return userMessage, nil
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
	// Build user message from scripted content or parts
	userMessage, err := e.buildUserMessage(req)
	if err != nil {
		outChan <- MessageStreamChunk{Error: err}
		return
	}

	messages := []types.Message{userMessage}

	// Build and execute pipeline
	middlewares := e.buildStreamingMiddlewares(req)
	pl := pipeline.NewPipeline(middlewares...)

	streamChan, streamErr := pl.ExecuteStream(ctx, userMessage.Role, userMessage.Content)
	if streamErr != nil {
		outChan <- MessageStreamChunk{Error: streamErr}
		return
	}

	// Forward stream chunks
	e.forwardStreamChunks(streamChan, messages, outChan)
}

// buildStreamingMiddlewares constructs the middleware chain for streaming
func (e *ScriptedExecutor) buildStreamingMiddlewares(req TurnRequest) []pipeline.Middleware {
	baseVariables := buildBaseVariables(req.Region)
	mergedVars := map[string]string{}
	for k, v := range baseVariables {
		mergedVars[k] = v
	}
	for k, v := range req.PromptVars {
		mergedVars[k] = v
	}

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
	middlewares = append(middlewares, &variableInjectionMiddleware{variables: mergedVars})
	if len(req.Metadata) > 0 {
		middlewares = append(middlewares, &metadataInjectionMiddleware{metadata: req.Metadata})
	}

	// Prompt, template, and provider middleware
	middlewares = append(middlewares,
		middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, mergedVars),
		middleware.TemplateMiddleware(),
		middleware.ProviderMiddleware(req.Provider, nil, nil, providerConfig),
	)

	// Media externalization middleware
	if e.pipelineExecutor.mediaStorage != nil {
		mediaConfig := &middleware.MediaExternalizerConfig{
			Enabled:         true,
			StorageService:  e.pipelineExecutor.mediaStorage,
			SizeThresholdKB: 100,
			DefaultPolicy:   "retain",
			RunID:           req.ConversationID,
			ConversationID:  req.ConversationID,
		}
		middlewares = append(middlewares, middleware.MediaExternalizerMiddleware(mediaConfig))
	}

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
