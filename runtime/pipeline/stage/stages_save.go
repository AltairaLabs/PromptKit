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

	// MessageLog, when set, indicates that messages are already persisted per-round
	// by the provider stage's write-through. The save stage skips message append
	// but still handles indexing and summarization.
	MessageLog statestore.MessageLog
}

// IncrementalSaveStage saves only new messages from the current turn using
// MessageAppender, avoiding the full load+replace+save cycle. When the store
// doesn't implement MessageAppender, it falls back to StateStoreSaveStage behavior.
type IncrementalSaveStage struct {
	BaseStage
	config    *IncrementalSaveConfig
	turnState *TurnState
}

// NewIncrementalSaveStage creates a new incremental save stage.
func NewIncrementalSaveStage(config *IncrementalSaveConfig) *IncrementalSaveStage {
	return &IncrementalSaveStage{
		BaseStage: NewBaseStage("incremental_save", StageTypeSink),
		config:    config,
	}
}

// NewIncrementalSaveStageWithTurnState creates an incremental save stage that
// also merges TurnState.ProviderRequestMetadata into the persisted state on
// the fallback fullSave path.
func NewIncrementalSaveStageWithTurnState(
	config *IncrementalSaveConfig,
	turnState *TurnState,
) *IncrementalSaveStage {
	return &IncrementalSaveStage{
		BaseStage: NewBaseStage("incremental_save", StageTypeSink),
		config:    config,
		turnState: turnState,
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

	// Collect new (non-history) messages while forwarding elements. History
	// messages are dropped from the in-memory slice — they're already in
	// the store and don't need to be re-persisted. Holding them here would
	// scale O(conversation-length) per Send for long conversations.
	collected, err := s.collectAndForward(ctx, input, output)
	if err != nil {
		return err
	}

	if len(collected.messages) == 0 {
		return nil
	}

	convID := s.config.StateStoreConfig.ConversationID

	// When MessageLog is NOT active, persist messages via the store.
	// When MessageLog IS active, messages are already persisted per-round
	// by the provider stage write-through — skip message append.
	if s.config.MessageLog == nil {
		if err := s.persistMessages(ctx, convID, collected.messages); err != nil {
			return err
		}
	}

	// Index and summarize regardless of persistence path
	if s.config.MessageIndex != nil {
		s.indexNewMessages(ctx, convID, collected.messages)
	}
	if s.config.Summarizer != nil && s.config.SummarizeThreshold > 0 {
		s.maybeSummarize(ctx, convID)
	}

	return nil
}

// persistMessages saves new messages to the store via MessageAppender or
// the BulkWriter fallback (Load + append-to-state + Save).
func (s *IncrementalSaveStage) persistMessages(
	ctx context.Context,
	convID string,
	newMessages []types.Message,
) error {
	if appender, ok := s.config.StateStoreConfig.Store.(statestore.MessageAppender); ok {
		if err := appender.AppendMessages(ctx, convID, newMessages); err != nil {
			return fmt.Errorf("incremental save: failed to append messages: %w", err)
		}
		return nil
	}
	return s.fullSave(ctx, newMessages)
}

// incrementalCollectedData holds data collected during processing.
type incrementalCollectedData struct {
	// messages contains only the new (non-history) messages produced by
	// this Send. History elements coming through the stream are filtered
	// out at collect time so we don't duplicate the conversation in memory.
	messages []types.Message
}

// collectAndForward collects new messages while forwarding all elements.
// Messages whose Source is "statestore", "summary", or "retrieved" are
// considered history and are forwarded but not collected — they're
// already persisted upstream.
func (s *IncrementalSaveStage) collectAndForward(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) (*incrementalCollectedData, error) {
	collected := &incrementalCollectedData{}

	for elem := range input {
		if elem.Message != nil && isNewMessage(elem.Message) {
			collected.messages = append(collected.messages, *elem.Message)
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

// isNewMessage reports whether a message originated from this Send (true)
// or was loaded from history by an upstream stage (false).
func isNewMessage(m *types.Message) bool {
	switch m.Source {
	case sourceStateStore, sourceSummary, sourceRetrieved:
		return false
	default:
		return true
	}
}

// fullSave appends the new messages to the existing state and writes via
// BulkWriter. This path is reached only when the store does not implement
// MessageAppender; persistence requires the store to also implement
// BulkWriter (otherwise the state is fundamentally unwritable and the
// stage errors clearly).
func (s *IncrementalSaveStage) fullSave(ctx context.Context, newMessages []types.Message) error {
	store, ok := s.config.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("incremental save: invalid store type")
	}
	bulkWriter, ok := s.config.StateStoreConfig.Store.(statestore.BulkWriter)
	if !ok {
		return fmt.Errorf(
			"incremental save: store implements neither MessageAppender nor BulkWriter — cannot persist state")
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
			Messages: make([]types.Message, 0, len(newMessages)),
			Metadata: make(map[string]any),
		}
	}

	// Append the new messages to whatever was already in the store —
	// the stage no longer collects history into memory, so this is the
	// only path that knows the full message set.
	state.Messages = append(state.Messages, newMessages...)

	if state.Metadata == nil {
		state.Metadata = make(map[string]any)
	}

	if s.turnState != nil {
		for k, v := range s.turnState.ProviderRequestMetadata {
			state.Metadata[k] = v
		}
	}

	// Re-load summaries before Save: a typed SaveSummary call may have
	// landed between our Load and Save (e.g., from maybeSummarize on a
	// different goroutine, or a future parallel compaction stage). The
	// snapshot we Loaded would otherwise clobber it.
	if accessor, ok := s.config.StateStoreConfig.Store.(statestore.SummaryAccessor); ok {
		summaries, sumErr := accessor.LoadSummaries(ctx, convID)
		if sumErr != nil {
			return fmt.Errorf("incremental save: failed to reload summaries: %w", sumErr)
		}
		state.Summaries = summaries
	}

	return bulkWriter.Save(ctx, state)
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

	// Load only the unsummarized tail via MessageReader. We hit this path
	// only when reader is non-nil (asserted at the top of the function),
	// so avoid the legacy Store.Load that deep-copies the entire history.
	tail, err := reader.LoadRecentMessages(ctx, convID, unsummarized)
	if err != nil {
		logger.Warn("Auto-summarize: failed to load message tail for summarization",
			"conversation", convID, "error", err)
		return
	}
	if len(tail) < unsummarized {
		// Reader returned fewer than asked — adjust unsummarized so the
		// turn-index math below stays consistent.
		unsummarized = len(tail)
	}

	endTurn := lastSummarizedTurn + s.config.SummarizeBatchSize
	if endTurn-lastSummarizedTurn > unsummarized {
		endTurn = lastSummarizedTurn + unsummarized
	}

	// tail covers messages [lastSummarizedTurn, count). Slice off the
	// SummarizeBatchSize prefix to get the oldest unsummarized batch.
	batch := tail[:endTurn-lastSummarizedTurn]
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
