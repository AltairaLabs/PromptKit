package turnexecutors

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
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

// executeStreamingPipeline builds and executes the streaming stage pipeline
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

	// Build and execute stage pipeline
	pl, err := e.buildStreamingStages(req)
	if err != nil {
		outChan <- MessageStreamChunk{Error: fmt.Errorf("failed to build streaming pipeline: %w", err)}
		return
	}

	// Create input element
	inputElem := stage.StreamElement{
		Message: &userMessage,
		Metadata: map[string]interface{}{
			"run_id":          req.RunID,
			"conversation_id": req.ConversationID,
		},
	}

	// Create input channel
	inputChan := make(chan stage.StreamElement, 1)
	inputChan <- inputElem
	close(inputChan)

	// Execute pipeline (returns streaming output channel)
	outputChan, streamErr := pl.Execute(ctx, inputChan)
	if streamErr != nil {
		outChan <- MessageStreamChunk{Error: streamErr}
		return
	}

	// Convert stage stream to provider chunks
	e.forwardStageElements(outputChan, messages, outChan)
}

// buildStreamingStages constructs the stage pipeline for streaming
func (e *ScriptedExecutor) buildStreamingStages(req TurnRequest) (*stage.StreamPipeline, error) {
	baseVariables := buildBaseVariables(req.Region)
	mergedVars := map[string]string{}
	for k, v := range baseVariables {
		mergedVars[k] = v
	}
	for k, v := range req.PromptVars {
		mergedVars[k] = v
	}

	builder := stage.NewPipelineBuilder()
	var stages []stage.Stage

	// StateStore Load stage
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		stages = append(stages, stage.NewStateStoreLoadStage(storeConfig))
	}

	// Variable injection
	stages = append(stages, stage.WrapMiddleware("variable_injection", &variableInjectionMiddleware{variables: mergedVars}))
	if len(req.Metadata) > 0 {
		stages = append(stages, stage.WrapMiddleware("metadata_injection", &metadataInjectionMiddleware{metadata: req.Metadata}))
	}

	// Prompt, template, and provider stages
	providerConfig := &stage.ProviderConfig{
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Seed:        req.Seed,
	}

	stages = append(stages,
		stage.NewPromptAssemblyStage(req.PromptRegistry, req.TaskType, mergedVars),
		stage.NewTemplateStage(),
		stage.NewProviderStage(
			req.Provider, e.pipelineExecutor.toolRegistry, buildToolPolicy(req.Scenario), providerConfig),
	)

	// Media externalization stage
	if e.pipelineExecutor.mediaStorage != nil {
		mediaConfig := &middleware.MediaExternalizerConfig{
			Enabled:         true,
			StorageService:  e.pipelineExecutor.mediaStorage,
			SizeThresholdKB: 100,
			DefaultPolicy:   "retain",
			RunID:           req.ConversationID,
			ConversationID:  req.ConversationID,
		}
		stages = append(stages, stage.WrapMiddleware("media_externalizer", middleware.MediaExternalizerMiddleware(mediaConfig)))
	}

	// Dynamic validator stage
	stages = append(stages, stage.NewValidationStage(validators.DefaultRegistry, true))

	// StateStore Save stage
	if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
		storeConfig := &pipeline.StateStoreConfig{
			Store:          req.StateStoreConfig.Store,
			ConversationID: req.ConversationID,
			UserID:         req.StateStoreConfig.UserID,
			Metadata:       req.StateStoreConfig.Metadata,
		}
		stages = append(stages, stage.NewStateStoreSaveStage(storeConfig))
	}

	// Assertion stage - validates turn-level assertions from scenario config
	if len(req.Assertions) > 0 {
		assertionRegistry := arenaassertions.NewArenaAssertionRegistry()
		stages = append(stages, stage.WrapMiddleware("arena_assertions",
			arenaassertions.ArenaAssertionMiddleware(assertionRegistry, req.Assertions)))
	}

	return builder.Chain(stages...).Build()
}

// forwardStageElements forwards stage elements from pipeline to output channel
func (e *ScriptedExecutor) forwardStageElements(
	outputChan <-chan stage.StreamElement,
	messages []types.Message,
	outChan chan<- MessageStreamChunk,
) {
	assistantIndex := 1
	var assistantMsg types.Message
	assistantMsg.Role = "assistant"

	for elem := range outputChan {
		// Check for error
		if elem.Error != nil {
			outChan <- MessageStreamChunk{Messages: messages, Error: elem.Error}
			return
		}

		// Handle streaming chunks (from ProviderStage)
		if elem.Metadata != nil {
			// Check if this is a streaming chunk with delta
			if delta, ok := elem.Metadata["delta"].(string); ok && delta != "" {
				// This is a streaming chunk
				var finishReason *string
				if fr, ok := elem.Metadata["finish_reason"].(string); ok {
					finishReason = &fr
				}

				var tokenCount int
				if tc, ok := elem.Metadata["token_count"].(int); ok {
					tokenCount = tc
				}

				outChan <- MessageStreamChunk{
					Messages:     messages,
					Delta:        delta,
					MessageIndex: assistantIndex,
					TokenCount:   tokenCount,
					FinishReason: finishReason,
				}

				// Continue processing - we'll get the final complete message next
				continue
			}
		}

		// Collect assistant messages (final complete message)
		if elem.Message != nil && elem.Message.Role == "assistant" {
			assistantMsg = *elem.Message
			messages = e.updateMessagesList(messages, assistantMsg, assistantIndex)

			// Extract finish reason from metadata if available
			var finishReason *string
			if elem.Metadata != nil {
				if fr, ok := elem.Metadata["finish_reason"].(string); ok {
					finishReason = &fr
				}
			}

			outChan <- MessageStreamChunk{
				Messages:     messages,
				MessageIndex: assistantIndex,
				FinishReason: finishReason,
			}

			if finishReason != nil {
				break
			}
		}
	}
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
