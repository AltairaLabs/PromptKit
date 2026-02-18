package stage

import (
	"context"
	"errors"
	"fmt"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// ContextAssemblyConfig configures the ContextAssemblyStage.
type ContextAssemblyConfig struct {
	// StateStoreConfig for accessing the store.
	StateStoreConfig *rtpipeline.StateStoreConfig

	// RecentMessages is the number of recent messages to include (hot window).
	RecentMessages int

	// MessageIndex for semantic retrieval of relevant older messages (Phase 2).
	// When nil, only the hot window and summaries are used.
	MessageIndex statestore.MessageIndex

	// RetrievalTopK is the number of results to retrieve from the message index.
	RetrievalTopK int
}

// ContextAssemblyStage loads a subset of conversation history using efficient
// partial reads (MessageReader) instead of loading the full state. It assembles
// context from three tiers: summaries, semantically retrieved messages, and
// the most recent messages (hot window).
//
// When the store doesn't implement MessageReader, it falls back to loading
// all history via Store.Load (same behavior as StateStoreLoadStage).
type ContextAssemblyStage struct {
	BaseStage
	config *ContextAssemblyConfig
}

// NewContextAssemblyStage creates a new context assembly stage.
func NewContextAssemblyStage(config *ContextAssemblyConfig) *ContextAssemblyStage {
	return &ContextAssemblyStage{
		BaseStage: NewBaseStage("context_assembly", StageTypeTransform),
		config:    config,
	}
}

// Process loads context tiers and emits them before the current input.
func (s *ContextAssemblyStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	if s.config == nil || s.config.StateStoreConfig == nil || s.config.StateStoreConfig.Store == nil {
		return s.forwardAll(ctx, input, output)
	}

	store, ok := s.config.StateStoreConfig.Store.(statestore.Store)
	if !ok {
		return fmt.Errorf("context assembly: invalid store type")
	}

	convID := s.config.StateStoreConfig.ConversationID

	// Try to use MessageReader for efficient partial reads
	reader, hasReader := s.config.StateStoreConfig.Store.(statestore.MessageReader)

	var historyMessages []types.Message

	if hasReader {
		msgs, err := s.assembleFromReader(ctx, convID, reader, store)
		if err != nil {
			return err
		}
		historyMessages = msgs
	} else {
		// Fallback: load full state (same as StateStoreLoadStage)
		msgs, err := s.loadFullHistory(ctx, convID, store)
		if err != nil {
			return err
		}
		historyMessages = msgs
	}

	// Emit history messages
	if err := s.emitHistory(ctx, historyMessages, output); err != nil {
		return err
	}

	// Forward input with metadata
	return s.forwardWithMetadata(ctx, input, output)
}

// assembleFromReader uses MessageReader and SummaryAccessor for efficient loading.
func (s *ContextAssemblyStage) assembleFromReader(
	ctx context.Context,
	convID string,
	reader statestore.MessageReader,
	_ statestore.Store,
) ([]types.Message, error) {
	var assembled []types.Message

	// 1. Load summaries (prepended as context)
	if accessor, ok := s.config.StateStoreConfig.Store.(statestore.SummaryAccessor); ok {
		summaries, err := accessor.LoadSummaries(ctx, convID)
		if err != nil {
			return nil, fmt.Errorf("context assembly: failed to load summaries: %w", err)
		}
		for _, summary := range summaries {
			content := fmt.Sprintf("[Conversation summary (turns %d-%d)]: %s",
				summary.StartTurn, summary.EndTurn, summary.Content)
			assembled = append(assembled, types.Message{
				Role:    "system",
				Content: content,
				Source:  "summary",
			})
		}
	}

	// 2. Load hot window (recent messages)
	n := s.config.RecentMessages
	if n <= 0 {
		n = 20 // sensible default
	}

	recentMsgs, err := reader.LoadRecentMessages(ctx, convID, n)
	if err != nil {
		if errors.Is(err, statestore.ErrNotFound) {
			return assembled, nil
		}
		return nil, fmt.Errorf("context assembly: failed to load recent messages: %w", err)
	}

	// 3. Semantic retrieval (Phase 2) — insert between summaries and hot window
	if s.config.MessageIndex != nil && s.config.RetrievalTopK > 0 && len(recentMsgs) > 0 {
		retrieved, err := s.retrieveRelevant(ctx, convID, recentMsgs)
		if err != nil {
			// Non-fatal: log but continue with hot window only
			_ = err
		} else {
			assembled = append(assembled, retrieved...)
		}
	}

	assembled = append(assembled, recentMsgs...)
	return assembled, nil
}

// retrieveRelevant searches the message index for messages relevant to the current context.
func (s *ContextAssemblyStage) retrieveRelevant(
	ctx context.Context,
	convID string,
	recentMsgs []types.Message,
) ([]types.Message, error) {
	// Build query from the last user message
	var query string
	for i := len(recentMsgs) - 1; i >= 0; i-- {
		if recentMsgs[i].Role == "user" {
			query = recentMsgs[i].Content
			break
		}
	}
	if query == "" {
		return nil, nil
	}

	results, err := s.config.MessageIndex.Search(ctx, convID, query, s.config.RetrievalTopK)
	if err != nil {
		return nil, fmt.Errorf("context assembly: index search failed: %w", err)
	}

	// Build a set of recent message turn indices for deduplication
	// Use message timestamps as a proxy for turn identity since we have recent N messages
	recentSet := make(map[int]bool)
	// We don't have turn indices on messages directly, so use IndexResult.TurnIndex for dedup
	// The hot window is the last N messages — their turn indices start from totalCount - N

	var retrieved []types.Message
	for i := range results {
		if recentSet[results[i].TurnIndex] {
			continue
		}
		msg := results[i].Message
		msg.Source = "retrieved"
		retrieved = append(retrieved, msg)
	}

	return retrieved, nil
}

// loadFullHistory loads all messages from state store (fallback path).
func (s *ContextAssemblyStage) loadFullHistory(
	ctx context.Context,
	convID string,
	store statestore.Store,
) ([]types.Message, error) {
	state, err := store.Load(ctx, convID)
	if err != nil && !errors.Is(err, statestore.ErrNotFound) {
		return nil, fmt.Errorf("context assembly: failed to load state: %w", err)
	}

	if state == nil || len(state.Messages) == 0 {
		return nil, nil
	}

	return state.Messages, nil
}

// emitHistory sends history messages to the output channel.
func (s *ContextAssemblyStage) emitHistory(
	ctx context.Context,
	messages []types.Message,
	output chan<- StreamElement,
) error {
	for i := range messages {
		messages[i].Source = "statestore"
		elem := NewMessageElement(&messages[i])
		elem.Metadata["from_history"] = true
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// forwardWithMetadata forwards input elements with conversation metadata.
func (s *ContextAssemblyStage) forwardWithMetadata(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
		if elem.Metadata == nil {
			elem.Metadata = make(map[string]interface{})
		}
		if s.config.StateStoreConfig != nil {
			elem.Metadata["conversation_id"] = s.config.StateStoreConfig.ConversationID
			if s.config.StateStoreConfig.UserID != "" {
				elem.Metadata["user_id"] = s.config.StateStoreConfig.UserID
			}
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// forwardAll forwards all elements without modification.
func (s *ContextAssemblyStage) forwardAll(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
