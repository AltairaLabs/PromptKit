package turnexecutors

import (
	"context"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// finishReasonStop is the standard finish reason for successful completion
	finishReasonStop = "stop"

	// roleAssistant is the standard role for assistant messages
	roleAssistant = "assistant"

	// httpLoaderTimeout is the timeout for HTTP media requests
	httpLoaderTimeout = 30 * time.Second

	// httpLoaderMaxSize is the maximum file size for HTTP media (50MB)
	httpLoaderMaxSize = 50 * 1024 * 1024

	// mediaExternalizerThresholdKB is the size threshold for media externalization
	mediaExternalizerThresholdKB = 100
)

// ScriptedExecutor executes turns where the user message is scripted (predefined)
type ScriptedExecutor struct {
	pipelineExecutor *PipelineExecutor
	httpLoader       *HTTPMediaLoader // Optional: if nil, a default loader is created
}

// NewScriptedExecutor creates a new executor for scripted user turns
func NewScriptedExecutor(pipelineExecutor *PipelineExecutor) *ScriptedExecutor {
	return &ScriptedExecutor{pipelineExecutor: pipelineExecutor}
}

// ExecuteTurn executes a scripted turn (user message from scenario + AI response)
//
//nolint:gocritic // Public API - changing to pointer would break callers
func (e *ScriptedExecutor) ExecuteTurn(ctx context.Context, req TurnRequest) error {
	// Build user message from scripted content or parts
	userMessage, err := e.buildUserMessage(&req)
	if err != nil {
		return err
	}

	// Execute through pipeline (messages saved to StateStore)
	return e.pipelineExecutor.Execute(ctx, &req, &userMessage)
}

// buildUserMessage creates a user message from either ScriptedContent or ScriptedParts
func (e *ScriptedExecutor) buildUserMessage(req *TurnRequest) (types.Message, error) {
	userMessage := types.Message{
		Role:      "user",
		Timestamp: time.Now(),
	}

	// If Parts are provided, use multimodal content (takes precedence)
	if len(req.ScriptedParts) > 0 {
		// Use the base directory from the request (resolved from config directory)
		baseDir := req.BaseDir

		// Use pre-configured loader or create a default one
		httpLoader := e.httpLoader
		if httpLoader == nil {
			httpLoader = NewHTTPMediaLoader(httpLoaderTimeout, httpLoaderMaxSize)
		}

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
//
//nolint:gocritic // Public API - changing to pointer would break callers
func (e *ScriptedExecutor) ExecuteTurnStream(
	ctx context.Context,
	req TurnRequest,
) (<-chan MessageStreamChunk, error) {
	outChan := make(chan MessageStreamChunk)

	go func() {
		defer close(outChan)

		// Handle non-streaming providers
		if e.handleNonStreamingProvider(ctx, &req, outChan) {
			return
		}

		// Execute streaming pipeline
		e.executeStreamingPipeline(ctx, &req, outChan)
	}()

	return outChan, nil
}

// handleNonStreamingProvider handles providers that don't support streaming
// Returns true if handled (caller should return)
func (e *ScriptedExecutor) handleNonStreamingProvider(
	ctx context.Context,
	req *TurnRequest,
	outChan chan<- MessageStreamChunk,
) bool {
	if req.Provider.SupportsStreaming() {
		return false
	}

	err := e.ExecuteTurn(ctx, *req)
	if err != nil {
		outChan <- MessageStreamChunk{Error: err}
		return true
	}

	finishReason := finishReasonStop
	outChan <- MessageStreamChunk{
		Messages:     []types.Message{},
		FinishReason: &finishReason,
	}
	return true
}

// executeStreamingPipeline builds and executes the streaming stage pipeline
func (e *ScriptedExecutor) executeStreamingPipeline(
	ctx context.Context,
	req *TurnRequest,
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

// buildStreamingStages constructs the stage pipeline for streaming.
// Delegates to the shared buildCommonStreamingStages with scripted-specific options.
func (e *ScriptedExecutor) buildStreamingStages(req *TurnRequest) (*stage.StreamPipeline, error) {
	return e.pipelineExecutor.buildCommonStreamingStages(req, StreamingStagesConfig{
		IncludeScenarioContextExtraction: true,
		IncludeGuardrailEval:             true,
		IncludeMediaExternalizer:         true,
		IncludeAssertions:                true,
		UseArenaStateStoreSave:           true,
	})
}

// extractFinishReason extracts finish reason from element metadata.
func extractFinishReason(metadata map[string]interface{}) *string {
	if metadata == nil {
		return nil
	}
	if fr, ok := metadata["finish_reason"].(string); ok {
		return &fr
	}
	return nil
}

// extractTokenCount extracts token count from element metadata.
func extractTokenCount(metadata map[string]interface{}) int {
	if metadata == nil {
		return 0
	}
	if tc, ok := metadata["token_count"].(int); ok {
		return tc
	}
	return 0
}

// forwardStageElements forwards stage elements from pipeline to output channel
func (e *ScriptedExecutor) forwardStageElements(
	outputChan <-chan stage.StreamElement,
	messages []types.Message,
	outChan chan<- MessageStreamChunk,
) {
	assistantIndex := 1
	var assistantMsg types.Message
	assistantMsg.Role = roleAssistant

	for elem := range outputChan {
		if elem.Error != nil {
			outChan <- MessageStreamChunk{Messages: messages, Error: elem.Error}
			return
		}

		if elem.Message != nil {
			if e.processMessageElement(&elem, &messages, &assistantMsg, assistantIndex, outChan) {
				break
			}
			continue
		}

		e.processStreamingElement(&elem, messages, assistantIndex, outChan)
	}
}

// processMessageElement handles message elements (final messages from provider).
// Returns true if streaming should stop.
func (e *ScriptedExecutor) processMessageElement(
	elem *stage.StreamElement,
	messages *[]types.Message,
	assistantMsg *types.Message,
	assistantIndex int,
	outChan chan<- MessageStreamChunk,
) bool {
	if elem.Message.Role != roleAssistant {
		return false
	}

	*assistantMsg = *elem.Message
	*messages = e.updateMessagesList(*messages, assistantMsg, assistantIndex)
	finishReason := extractFinishReason(elem.Metadata)

	outChan <- MessageStreamChunk{
		Messages:     *messages,
		MessageIndex: assistantIndex,
		FinishReason: finishReason,
	}

	return finishReason != nil
}

// processStreamingElement handles streaming text chunks.
func (e *ScriptedExecutor) processStreamingElement(
	elem *stage.StreamElement,
	messages []types.Message,
	assistantIndex int,
	outChan chan<- MessageStreamChunk,
) {
	if elem.Text == nil || *elem.Text == "" {
		return
	}

	outChan <- MessageStreamChunk{
		Messages:     messages,
		Delta:        *elem.Text,
		MessageIndex: assistantIndex,
		TokenCount:   extractTokenCount(elem.Metadata),
		FinishReason: extractFinishReason(elem.Metadata),
	}
}

// updateMessagesList updates the messages list with current assistant message
func (e *ScriptedExecutor) updateMessagesList(
	messages []types.Message,
	assistantMsg *types.Message,
	assistantIndex int,
) []types.Message {
	if len(messages) == assistantIndex {
		return append(messages, *assistantMsg)
	}
	messages[assistantIndex] = *assistantMsg
	return messages
}
