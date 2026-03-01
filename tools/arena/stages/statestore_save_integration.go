package stages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	runtimeStatestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
)

const (
	contentPreviewMaxLen = 40 // Maximum length for content preview in logs
)

// ArenaStateStoreSaveStage saves conversation state with telemetry to ArenaStateStore.
// This stage captures validation results, turn metrics, and cost information
// for Arena testing and analysis.
type ArenaStateStoreSaveStage struct {
	stage.BaseStage
	config *pipeline.StateStoreConfig
}

// NewArenaStateStoreSaveStage creates a new Arena state store save stage.
func NewArenaStateStoreSaveStage(config *pipeline.StateStoreConfig) *ArenaStateStoreSaveStage {
	return &ArenaStateStoreSaveStage{
		BaseStage: stage.NewBaseStage("arena_statestore_save", stage.StageTypeSink),
		config:    config,
	}
}

// collectedData holds all data collected from stream elements.
type collectedData struct {
	messages              []types.Message
	metadata              map[string]interface{}
	trace                 *pipeline.ExecutionTrace
	costInfo              *types.CostInfo
	transcriptionsApplied int // Tracks how many transcriptions have been applied (for ordering)
}

// collectFromElement extracts data from a single element and updates collected data.
func (d *collectedData) collectFromElement(elem *stage.StreamElement) {
	if elem.Message != nil {
		// Truncate content for logging
		contentPreview := elem.Message.Content
		if len(contentPreview) > contentPreviewMaxLen {
			contentPreview = contentPreview[:contentPreviewMaxLen] + "..."
		}
		logger.Debug("ArenaStateStoreSaveStage: collecting message",
			"role", elem.Message.Role,
			"content_len", len(elem.Message.Content),
			"content_preview", contentPreview,
			"total_messages", len(d.messages)+1)
		d.messages = append(d.messages, *elem.Message)
	}
	if elem.Metadata == nil {
		return
	}
	if d.metadata == nil {
		d.metadata = make(map[string]interface{})
	}
	for k, v := range elem.Metadata {
		d.metadata[k] = v
	}
	if t, ok := elem.Metadata["execution_trace"].(*pipeline.ExecutionTrace); ok {
		d.trace = t
	}
	if c, ok := elem.Metadata["cost_info"].(*types.CostInfo); ok {
		d.costInfo = c
	}

	// Apply input transcription to the correct user message.
	// If a turn_id is provided, use it to find the exact user message.
	// Otherwise, fall back to order-based matching (N-th transcription to N-th user message).
	if inputTranscript, ok := elem.Metadata["input_transcription"].(string); ok && inputTranscript != "" {
		if turnID, ok := elem.Metadata["transcription_turn_id"].(string); ok && turnID != "" {
			// Use turn_id for reliable matching
			d.applyTranscriptionByTurnID(inputTranscript, turnID)
		} else {
			// Fall back to order-based matching
			d.applyNextTranscription(inputTranscript)
		}
	}
}

// applyTranscriptionByTurnID applies a transcription to the user message with matching turn_id.
// This provides reliable correlation between audio input and its transcription,
// regardless of message arrival order.
//
// For selfplay messages (those with meta.self_play == true), the original LLM-generated
// text is preserved in meta.original_text before being replaced by the transcription.
func (d *collectedData) applyTranscriptionByTurnID(transcript, turnID string) {
	for i := 0; i < len(d.messages); i++ {
		if d.messages[i].Role == roleUser {
			// Check if this message has the matching turn_id
			if d.messages[i].Meta != nil {
				if msgTurnID, ok := d.messages[i].Meta["turn_id"].(string); ok && msgTurnID == turnID {
					d.applyTranscriptionToMessage(i, transcript)
					logger.Debug("ArenaStateStoreSaveStage: applied transcription by turn_id",
						"message_index", i,
						"turn_id", turnID,
						"transcription_len", len(transcript))
					return
				}
			}
		}
	}
	logger.Warn("ArenaStateStoreSaveStage: no user message found with turn_id",
		"turn_id", turnID,
		"total_messages", len(d.messages))
}

// applyNextTranscription applies a transcription to the N-th user message in order.
// This is the fallback method when turn_id is not available.
// Transcriptions arrive in turn order:
// - First transcription is for the first user turn
// - Second transcription is for the second user turn
// - etc.
//
// We use the transcriptionsApplied counter to find the correct user message,
// which handles race conditions where messages may arrive out of collection order.
//
// For selfplay messages (those with meta.self_play == true), the original LLM-generated
// text is preserved in meta.original_text before being replaced by the transcription.
// This allows comparing what the selfplay LLM generated vs what Gemini heard.
func (d *collectedData) applyNextTranscription(transcript string) {
	// Find the N-th user message (where N = transcriptionsApplied)
	userCount := 0
	for i := 0; i < len(d.messages); i++ {
		if d.messages[i].Role == roleUser {
			if userCount == d.transcriptionsApplied {
				// This is the user message that should receive this transcription
				d.applyTranscriptionToMessage(i, transcript)
				d.transcriptionsApplied++
				logger.Debug("ArenaStateStoreSaveStage: applied input transcription to user message",
					"message_index", i,
					"transcription_number", d.transcriptionsApplied,
					"transcription_len", len(transcript))
				return
			}
			userCount++
		}
	}
	logger.Warn("ArenaStateStoreSaveStage: no user message found to apply transcription",
		"transcription_number", d.transcriptionsApplied+1,
		"user_messages_found", userCount,
		"total_messages", len(d.messages))
}

// applyTranscriptionToMessage applies a transcription to a specific message by index.
func (d *collectedData) applyTranscriptionToMessage(msgIdx int, transcript string) {
	// For selfplay messages, preserve the original generated text in meta
	// before overwriting with transcription
	if d.messages[msgIdx].Meta != nil {
		if _, isSelfplay := d.messages[msgIdx].Meta["self_play"].(bool); isSelfplay {
			// Store original text if not already stored
			if _, hasOriginal := d.messages[msgIdx].Meta["original_text"]; !hasOriginal {
				d.messages[msgIdx].Meta["original_text"] = d.messages[msgIdx].Content
			}
		}
	}

	// Update the Content field with the transcription
	d.messages[msgIdx].Content = transcript

	// Also add/update a text part with the transcription
	textFound := false
	for j := range d.messages[msgIdx].Parts {
		if d.messages[msgIdx].Parts[j].Type == types.ContentTypeText || d.messages[msgIdx].Parts[j].Text != nil {
			d.messages[msgIdx].Parts[j].Text = &transcript
			d.messages[msgIdx].Parts[j].Type = types.ContentTypeText
			textFound = true
			break
		}
	}
	// If no text part exists, prepend one
	if !textFound {
		newPart := types.ContentPart{
			Type: types.ContentTypeText,
			Text: &transcript,
		}
		d.messages[msgIdx].Parts = append([]types.ContentPart{newPart}, d.messages[msgIdx].Parts...)
	}
}

// forwardElements just forwards elements without collecting.
func forwardElements(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Process collects messages and saves them incrementally to Arena state store.
// Messages are saved after each turn completion (when an element contains a Message).
// This ensures conversation state is captured in real-time as turns complete.
func (s *ArenaStateStoreSaveStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	if s.config == nil || s.config.Store == nil {
		return forwardElements(ctx, input, output)
	}

	arenaStore, ok := s.config.Store.(*statestore.ArenaStateStore)
	if !ok {
		//nolint:lll // Error message with type name - acceptable long line
		return fmt.Errorf("arena state store save: invalid store type, expected *statestore.ArenaStateStore, got %T", s.config.Store)
	}

	data := &collectedData{}
	ctxCanceled := false
	for elem := range input {
		data.collectFromElement(&elem)

		// Save incrementally when we receive a Message (turn completion)
		// This ensures each turn is saved to state store immediately
		if elem.Message != nil {
			logger.Debug("ArenaStateStoreSaveStage: saving after turn completion",
				"message_count", len(data.messages),
				"last_role", elem.Message.Role,
				"last_content_len", len(elem.Message.Content))

			// Use background context for saves - we want to capture data even if main ctx is canceled
			// NOSONAR: Intentional background context - ensures data persistence on cancellation
			saveCtx := ctx
			if ctxCanceled {
				saveCtx = context.Background()
			}
			err := s.saveToArenaStateStore(
				saveCtx, arenaStore, data.messages, data.metadata, data.trace, data.costInfo)
			if err != nil {
				logger.Error("ArenaStateStoreSaveStage: failed to save after turn", "error", err)
				// Continue processing - don't fail the entire pipeline for a save error
			}
		}

		// Try to forward element, but if context is canceled, just drain remaining input
		// This ensures we capture all messages (including partial responses) even on timeout
		select {
		case output <- elem:
		case <-ctx.Done():
			if !ctxCanceled {
				logger.Debug("ArenaStateStoreSaveStage: context canceled, will drain remaining elements")
				ctxCanceled = true
			}
			// Continue draining - don't return early
		}
	}

	// Final save at end (in case any metadata/trace was collected after last message)
	logger.Debug("ArenaStateStoreSaveStage: final save",
		"message_count", len(data.messages))

	// Use background context for final save if main context was canceled
	// This ensures we save all collected data including partial responses
	// NOSONAR: Intentional background context - ensures final data persistence on cancellation
	saveCtx := ctx
	if ctxCanceled {
		saveCtx = context.Background()
	}
	err := s.saveToArenaStateStore(
		saveCtx, arenaStore, data.messages, data.metadata, data.trace, data.costInfo)
	if err != nil {
		return fmt.Errorf("arena state store save: %w", err)
	}

	return nil
}

// createNewState creates a new conversation state with config defaults.
func (s *ArenaStateStoreSaveStage) createNewState() *runtimeStatestore.ConversationState {
	state := &runtimeStatestore.ConversationState{
		ID:       s.config.ConversationID,
		UserID:   s.config.UserID,
		Messages: make([]types.Message, 0),
		Metadata: make(map[string]interface{}),
	}
	for k, v := range s.config.Metadata {
		state.Metadata[k] = v
	}
	return state
}

// updateStateMetadata updates state metadata with execution data and cost info.
func updateStateMetadata(
	state *runtimeStatestore.ConversationState,
	metadata map[string]interface{},
	costInfo *types.CostInfo,
) {
	if state.Metadata == nil {
		state.Metadata = make(map[string]interface{})
	}
	for k, v := range metadata {
		state.Metadata[k] = v
	}
	if costInfo != nil && costInfo.TotalCost > 0 {
		state.Metadata["total_cost_usd"] = costInfo.TotalCost
		state.Metadata["total_tokens"] = costInfo.InputTokens + costInfo.OutputTokens
	}
}

// ensureTrace returns the provided trace or creates a default one.
func ensureTrace(trace *pipeline.ExecutionTrace) *pipeline.ExecutionTrace {
	if trace != nil {
		return trace
	}
	return &pipeline.ExecutionTrace{
		StartedAt: time.Now(),
		LLMCalls:  []pipeline.LLMCall{},
		Events:    []pipeline.TraceEvent{},
	}
}

// saveToArenaStateStore saves conversation state with telemetry.
func (s *ArenaStateStoreSaveStage) saveToArenaStateStore(
	ctx context.Context,
	arenaStore *statestore.ArenaStateStore,
	messages []types.Message,
	metadata map[string]interface{},
	trace *pipeline.ExecutionTrace,
	costInfo *types.CostInfo,
) error {
	state, err := arenaStore.Load(ctx, s.config.ConversationID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("failed to load state: %w", err)
	}

	if state == nil {
		state = s.createNewState()
	}

	// Look for system_prompt in element metadata first, then in config metadata
	// This allows system_prompt to be passed through either path
	systemPrompt := ""
	if sp, ok := metadata["system_prompt"].(string); ok && sp != "" {
		systemPrompt = sp
	} else if s.config != nil && s.config.Metadata != nil {
		if sp, ok := s.config.Metadata["system_prompt"].(string); ok && sp != "" {
			systemPrompt = sp
		}
	}

	// Set messages with system prompt if present
	if systemPrompt != "" {
		state.Messages = prependSystemMessage(messages, systemPrompt)
	} else {
		state.Messages = make([]types.Message, len(messages))
		copy(state.Messages, messages)
	}

	updateStateMetadata(state, metadata, costInfo)

	if err := arenaStore.SaveWithTrace(ctx, state, ensureTrace(trace)); err != nil {
		return fmt.Errorf("failed to save with trace: %w", err)
	}

	return nil
}

// createSystemMessage creates a system message with the given prompt and timestamp.
func createSystemMessage(systemPrompt string, timestamp time.Time) types.Message {
	textContent := systemPrompt
	return types.Message{
		Role:    "system",
		Content: systemPrompt,
		Parts: []types.ContentPart{
			{
				Type: "text",
				Text: &textContent,
			},
		},
		Timestamp: timestamp,
	}
}

// prependSystemMessage prepends a system message if not already present.
func prependSystemMessage(messages []types.Message, systemPrompt string) []types.Message {
	// Check if first message is already a system message
	if len(messages) > 0 && messages[0].Role == "system" {
		// Already has system message, return as-is
		result := make([]types.Message, len(messages))
		copy(result, messages)
		return result
	}

	// Determine timestamp for system message
	var timestamp time.Time
	if len(messages) > 0 {
		timestamp = messages[0].Timestamp
	} else {
		timestamp = time.Now()
	}

	// Create new slice with system message prepended
	systemMsg := createSystemMessage(systemPrompt, timestamp)
	result := make([]types.Message, 0, len(messages)+1)
	result = append(result, systemMsg)
	result = append(result, messages...)
	return result
}
