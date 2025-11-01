package turnexecutors

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
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
func (e *ScriptedExecutor) ExecuteTurnStream(ctx context.Context, req TurnRequest) (<-chan MessageStreamChunk, error) {
	outChan := make(chan MessageStreamChunk)

	go func() {
		defer close(outChan)

		// Check if provider supports streaming
		if !req.Provider.SupportsStreaming() {
			// Fallback to non-streaming - just execute and send error/success
			err := e.ExecuteTurn(ctx, req)
			if err != nil {
				outChan <- MessageStreamChunk{Error: err}
				return
			}

			// Send final chunk indicating completion (messages are in StateStore)
			finishReason := "stop"
			outChan <- MessageStreamChunk{
				Messages:     []types.Message{}, // Messages in StateStore, not returned
				FinishReason: &finishReason,
			}
			return
		}

		// Initialize with user message
		userMessage := types.Message{
			Role:      "user",
			Content:   req.ScriptedContent,
			Timestamp: time.Now(),
		}
		messages := []types.Message{userMessage}

		// Build base variables
		baseVariables := buildBaseVariables(req.Region)

		// Build provider config
		providerConfig := &middleware.ProviderMiddlewareConfig{
			MaxTokens:   req.MaxTokens,
			Temperature: float32(req.Temperature),
			Seed:        req.Seed,
		}

		// Build pipeline with middleware
		var middlewares []pipeline.Middleware

		// 0. StateStore Load middleware (if configured)
		if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
			storeConfig := &pipeline.StateStoreConfig{
				Store:          req.StateStoreConfig.Store,
				ConversationID: req.ConversationID,
				UserID:         req.StateStoreConfig.UserID,
				Metadata:       req.StateStoreConfig.Metadata,
			}
			middlewares = append(middlewares, middleware.StateStoreLoadMiddleware(storeConfig))
		}

		// 1. Inject variables (StateStore handles history)
		middlewares = append(middlewares, &variableInjectionMiddleware{variables: baseVariables})

		// 2-3. Prompt and template middleware
		middlewares = append(middlewares,
			middleware.PromptAssemblyMiddleware(req.PromptRegistry, req.TaskType, baseVariables),
			middleware.TemplateMiddleware(),
			middleware.ProviderMiddleware(req.Provider, nil, nil, providerConfig), // No tools for scripted executor
		)

		// 4. Dynamic validator middleware - validates response against prompt-level validators
		middlewares = append(middlewares, middleware.DynamicValidatorMiddleware(validators.DefaultRegistry))

		// 5. StateStore Save middleware (if configured)
		if req.StateStoreConfig != nil && req.StateStoreConfig.Store != nil {
			storeConfig := &pipeline.StateStoreConfig{
				Store:          req.StateStoreConfig.Store,
				ConversationID: req.ConversationID,
				UserID:         req.StateStoreConfig.UserID,
				Metadata:       req.StateStoreConfig.Metadata,
			}
			middlewares = append(middlewares, middleware.StateStoreSaveMiddleware(storeConfig))
		}

		pl := pipeline.NewPipeline(middlewares...)

		// Execute pipeline in background (streaming)
		streamChan, err := pl.ExecuteStream(ctx, userMessage.Role, userMessage.Content)
		if err != nil {
			outChan <- MessageStreamChunk{Error: err}
			return
		}

		// Forward stream chunks to output
		assistantIndex := 1 // Index where assistant message will be
		var assistantMsg types.Message
		assistantMsg.Role = "assistant"

		for chunk := range streamChan {
			if chunk.Error != nil {
				outChan <- MessageStreamChunk{
					Messages: messages,
					Error:    chunk.Error,
				}
				return
			}

			// Check for final result in last chunk
			if chunk.FinalResult != nil {
				// Pipeline is complete - final chunk received
				break
			}

			// Update assistant message with accumulated content
			assistantMsg.Content = chunk.Content

			// Convert tool calls if present
			if len(chunk.ToolCalls) > 0 {
				assistantMsg.ToolCalls = make([]types.MessageToolCall, len(chunk.ToolCalls))
				for i, tc := range chunk.ToolCalls {
					assistantMsg.ToolCalls[i] = types.MessageToolCall{
						ID:   tc.ID,
						Name: tc.Name,
						Args: tc.Args,
					}
				}
			}

			// Update messages list
			if len(messages) == assistantIndex {
				messages = append(messages, assistantMsg)
			} else {
				messages[assistantIndex] = assistantMsg
			}

			// Send message chunk
			outChan <- MessageStreamChunk{
				Messages:     messages,
				Delta:        chunk.Delta,
				MessageIndex: assistantIndex,
				TokenCount:   chunk.TokenCount,
				FinishReason: chunk.FinishReason,
			}

			// Stop if we got finish reason
			if chunk.FinishReason != nil {
				break
			}
		}
	}()

	return outChan, nil
}

func buildBaseVariables(region string) map[string]string {
	baseVars := map[string]string{}
	if region != "" {
		baseVars["region"] = region
	}
	return baseVars
}
