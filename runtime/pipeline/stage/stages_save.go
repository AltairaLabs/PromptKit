package stage

import (
	"context"
	"errors"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Message source values for filtering history messages.
const (
	sourceStateStore = "statestore"
	sourceSummary    = "summary"
	sourceRetrieved  = "retrieved"
)

// IncrementalSaveConfig configures the IncrementalSaveStage.
type IncrementalSaveConfig struct {
	// StateStoreConfig for accessing the store.
	StateStoreConfig *rtpipeline.StateStoreConfig

	// MessageIndex for indexing new messages (Phase 2, optional).
	MessageIndex statestore.MessageIndex

	// Summarizer for auto-summarization (Phase 3, optional).
	Summarizer statestore.Summarizer

	// SummarizeThreshold is the message count above which summarization triggers.
	SummarizeThreshold int

	// SummarizeBatchSize is how many messages to summarize at once.
	SummarizeBatchSize int
}

// IncrementalSaveStage saves only new messages from the current turn using
// MessageAppender, avoiding the full load+replace+save cycle. When the store
// doesn't implement MessageAppender, it falls back to StateStoreSaveStage behavior.
type IncrementalSaveStage struct {
	BaseStage
	config *IncrementalSaveConfig
}

// NewIncrementalSaveStage creates a new incremental save stage.
func NewIncrementalSaveStage(config *IncrementalSaveConfig) *IncrementalSaveStage {
	return &IncrementalSaveStage{
		BaseStage: NewBaseStage("incremental_save", StageTypeSink),
		config:    config,
	}
}

// Process collects new messages and appends them incrementally.
func (s *IncrementalSaveStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.config == nil || s.config.StateStoreConfig == nil || s.config.StateStoreConfig.Store == nil {
		return s.passthrough(ctx, input, output)
	}

	// Collect messages while forwarding elements
	collected, err := s.collectAndForward(ctx, input, output)
	if err != nil {
		return err
	}

	// Identify new messages (those not from history)
	newMessages := s.filterNewMessages(collected.messages)
	if len(newMessages) == 0 {
		return nil
	}

	convID := s.config.StateStoreConfig.ConversationID

	// Try MessageAppender for incremental save
	if appender, ok := s.config.StateStoreConfig.Store.(statestore.MessageAppender); ok {
		if err := appender.AppendMessages(ctx, convID, newMessages); err != nil {
			return fmt.Errorf("incremental save: failed to append messages: %w", err)
		}
	} else {
		// Fallback: full load+save cycle
		if err := s.fullSave(ctx, collected); err != nil {
			return err
		}
	}

	// Index new messages (Phase 2)
	if s.config.MessageIndex != nil {
		s.indexNewMessages(ctx, convID, newMessages)
	}

	// Auto-summarize if needed (Phase 3)
	if s.config.Summarizer != nil && s.config.SummarizeThreshold > 0 {
		s.maybeSummarize(ctx, convID)
	}

	return nil
}

// incrementalCollectedData holds data collected during processing.
type incrementalCollectedData struct {
	messages []types.Message
	metadata map[string]any
}

// collectAndForward collects messages while forwarding all elements.
func (s *IncrementalSaveStage) collectAndForward(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) (*incrementalCollectedData, error) {
	collected := &incrementalCollectedData{}

	for elem := range input {
		if elem.Message != nil {
			collected.messages = append(collected.messages, *elem.Message)
		}

		// Merge metadata
		if elem.Metadata != nil {
			if collected.metadata == nil {
				collected.metadata = make(map[string]any)
			}
			for k, v := range elem.Metadata {
				collected.metadata[k] = v
			}
		}

		// Always forward error elements unconditionally
		if elem.Error != nil {
			output <- elem
			continue
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return collected, nil
}

// filterNewMessages returns only messages that were not loaded from history.
func (s *IncrementalSaveStage) filterNewMessages(messages []types.Message) []types.Message {
	var newMsgs []types.Message
	for i := range messages {
		src := messages[i].Source
		if src != sourceStateStore && src != sourceSummary && src != sourceRetrieved {
			newMsgs = append(newMsgs, messages[i])
		}
	}
	return newMsgs
}

// fullSave performs a full load+replace+save cycle (fallback when MessageAppender unavailable).
func (s *IncrementalSaveStage) fullSave(ctx context.Context, collected *incrementalCollectedData) error {
	store, ok := s.config.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("incremental save: invalid store type")
	}

	convID := s.config.StateStoreConfig.ConversationID
	state, err := store.Load(ctx, convID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return fmt.Errorf("incremental save: failed to load state: %w", err)
	}

	if state == nil {
		state = &statestore.ConversationState{
			ID:       convID,
			UserID:   s.config.StateStoreConfig.UserID,
			Messages: make([]types.Message, 0),
			Metadata: make(map[string]any),
		}
	}

	state.Messages = make([]types.Message, len(collected.messages))
	copy(state.Messages, collected.messages)

	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}
	for k, v := range collected.metadata {
		state.Metadata[k] = v
	}

	return store.Save(ctx, state)
}

// indexNewMessages indexes new messages in the message index (Phase 2).
func (s *IncrementalSaveStage) indexNewMessages(
	ctx context.Context,
	convID string,
	messages []types.Message,
) {
	// Get current message count to determine turn indices
	var baseIndex int
	if reader, ok := s.config.StateStoreConfig.Store.(statestore.MessageReader); ok {
		count, err := reader.MessageCount(ctx, convID)
		if err == nil {
			baseIndex = count - len(messages)
		}
	}

	for i := range messages {
		if err := s.config.MessageIndex.Index(ctx, convID, baseIndex+i, messages[i]); err != nil {
			logger.Warn("Incremental save: failed to index message",
				"conversation", convID, "turnIndex", baseIndex+i, "error", err)
		}
	}
}

// maybeSummarize checks if summarization is needed and triggers it (Phase 3).
func (s *IncrementalSaveStage) maybeSummarize(ctx context.Context, convID string) {
	reader, ok := s.config.StateStoreConfig.Store.(statestore.MessageReader)
	if !ok {
		logger.Warn("Auto-summarize: store does not implement MessageReader, skipping",
			"conversation", convID)
		return
	}

	count, err := reader.MessageCount(ctx, convID)
	if err != nil {
		logger.Warn("Auto-summarize: failed to get message count",
			"conversation", convID, "error", err)
		return
	}
	if count <= s.config.SummarizeThreshold {
		return
	}

	accessor, ok := s.config.StateStoreConfig.Store.(statestore.SummaryAccessor)
	if !ok {
		logger.Warn("Auto-summarize: store does not implement SummaryAccessor, skipping",
			"conversation", convID)
		return
	}

	// Determine how many messages are unsummarized
	summaries, err := accessor.LoadSummaries(ctx, convID)
	if err != nil {
		logger.Warn("Auto-summarize: failed to load summaries",
			"conversation", convID, "error", err)
		return
	}

	lastSummarizedTurn := 0
	for _, sum := range summaries {
		if sum.EndTurn > lastSummarizedTurn {
			lastSummarizedTurn = sum.EndTurn
		}
	}

	unsummarized := count - lastSummarizedTurn
	if unsummarized < s.config.SummarizeBatchSize {
		return
	}

	// Load the batch to summarize
	store, ok := s.config.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return
	}

	state, err := store.Load(ctx, convID)
	if err != nil {
		logger.Warn("Auto-summarize: failed to load state for summarization",
			"conversation", convID, "error", err)
		return
	}

	endTurn := lastSummarizedTurn + s.config.SummarizeBatchSize
	if endTurn > len(state.Messages) {
		endTurn = len(state.Messages)
	}

	batch := state.Messages[lastSummarizedTurn:endTurn]
	content, err := s.config.Summarizer.Summarize(ctx, batch)
	if err != nil {
		logger.Error("Auto-summarize: summarization failed",
			"conversation", convID, "startTurn", lastSummarizedTurn, "endTurn", endTurn, "error", err)
		return
	}

	summary := statestore.Summary{
		StartTurn: lastSummarizedTurn,
		EndTurn:   endTurn,
		Content:   content,
	}

	if err := accessor.SaveSummary(ctx, convID, summary); err != nil {
		logger.Error("Auto-summarize: failed to save summary",
			"conversation", convID, "startTurn", summary.StartTurn, "endTurn", summary.EndTurn, "error", err)
	} else {
		logger.Info("Auto-summarize: created summary",
			"conversation", convID, "startTurn", summary.StartTurn, "endTurn", summary.EndTurn)
	}
}

// passthrough forwards all input elements without saving.
func (s *IncrementalSaveStage) passthrough(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
		if elem.Error != nil {
			output <- elem
			continue
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
