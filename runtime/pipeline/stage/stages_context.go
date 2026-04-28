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

const (
	// DefaultMaxMessages is the maximum number of messages retained when no
	// explicit context window is configured. Older non-system messages are
	// discarded to prevent unbounded context growth.
	DefaultMaxMessages = 200

	// DefaultWarningThreshold is the message count above which a warning is
	// logged when no context window or token budget is configured.
	DefaultWarningThreshold = 100
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

	// MaxMessages is the hard cap on total messages retained when no explicit
	// context window is set. System messages at the start are always preserved.
	// Default: DefaultMaxMessages (200).
	MaxMessages int

	// WarningThreshold is the message count above which a warning is logged
	// when no context window or token budget is configured.
	// Default: DefaultWarningThreshold (100).
	WarningThreshold int

	// HasTokenBudget indicates whether a token budget stage is configured
	// downstream. When true, the large-conversation warning is suppressed
	// because the token budget stage handles overflow.
	HasTokenBudget bool

	// HasContextWindow indicates whether an explicit context window (hot
	// window) size was set by the caller. When false and messages exceed
	// MaxMessages, automatic truncation is applied.
	HasContextWindow bool
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
	config    *ContextAssemblyConfig
	turnState *TurnState
}

// NewContextAssemblyStage creates a new context assembly stage.
func NewContextAssemblyStage(config *ContextAssemblyConfig) *ContextAssemblyStage {
	return NewContextAssemblyStageWithTurnState(config, nil)
}

// NewContextAssemblyStageWithTurnState creates a context assembly stage
// that publishes ConversationID/UserID onto the supplied TurnState.
func NewContextAssemblyStageWithTurnState(
	config *ContextAssemblyConfig, turnState *TurnState,
) *ContextAssemblyStage {
	if config != nil {
		if config.MaxMessages <= 0 {
			config.MaxMessages = DefaultMaxMessages
		}
		if config.WarningThreshold <= 0 {
			config.WarningThreshold = DefaultWarningThreshold
		}
	}
	return &ContextAssemblyStage{
		BaseStage: NewBaseStage("context_assembly", StageTypeTransform),
		config:    config,
		turnState: turnState,
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

	if s.turnState != nil {
		s.turnState.ConversationID = convID
		s.turnState.UserID = s.config.StateStoreConfig.UserID
	}

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

	// Warn about large conversations without token budget or context window
	s.warnLargeConversation(historyMessages)

	// Apply default message limit when no explicit context window is set
	historyMessages = s.applyDefaultMessageLimit(historyMessages)

	// Emit history messages
	if err := s.emitHistory(ctx, historyMessages, output); err != nil {
		return err
	}

	return s.forwardInput(ctx, input, output)
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
			logger.Warn("Context assembly: semantic retrieval failed, using hot window only",
				"conversation", convID, "error", err)
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
		elem.Meta.FromHistory = true
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// warnLargeConversation logs a warning when the conversation has many
// messages and neither a token budget nor an explicit context window is set.
func (s *ContextAssemblyStage) warnLargeConversation(messages []types.Message) {
	if s.config == nil {
		return
	}
	if s.config.HasTokenBudget || s.config.HasContextWindow {
		return
	}
	if len(messages) > s.config.WarningThreshold {
		logger.Warn("Large conversation detected without token budget or context window",
			"message_count", len(messages),
			"threshold", s.config.WarningThreshold,
			"suggestion", "Configure WithTokenBudget or WithContextWindow to manage context size")
	}
}

// applyDefaultMessageLimit truncates messages to DefaultMaxMessages when no
// explicit context window is configured. System messages at the beginning
// are always preserved; only non-system messages are subject to truncation.
func (s *ContextAssemblyStage) applyDefaultMessageLimit(messages []types.Message) []types.Message {
	if s.config == nil || s.config.HasContextWindow {
		return messages
	}
	if len(messages) <= s.config.MaxMessages {
		return messages
	}

	// Separate leading system messages from the rest
	systemCount := 0
	for i := range messages {
		if messages[i].Role == roleSystem {
			systemCount++
		} else {
			break
		}
	}

	systemMsgs := messages[:systemCount]
	conversationMsgs := messages[systemCount:]

	// Keep the most recent conversation messages that fit within the limit
	maxConversation := s.config.MaxMessages - systemCount
	if maxConversation <= 0 {
		// System messages alone exceed the limit; return them all
		return systemMsgs
	}

	if len(conversationMsgs) <= maxConversation {
		return messages
	}

	truncated := len(conversationMsgs) - maxConversation

	logger.Warn("Truncating conversation to default message limit",
		"original_count", len(messages),
		"kept_count", systemCount+maxConversation,
		"truncated_count", truncated,
		"max_messages", s.config.MaxMessages)

	result := make([]types.Message, 0, s.config.MaxMessages)
	result = append(result, systemMsgs...)
	result = append(result, conversationMsgs[truncated:]...)
	return result
}

// forwardInput forwards input elements unchanged. Conversation/user IDs
// are published to TurnState in Process() before this loop runs.
func (s *ContextAssemblyStage) forwardInput(
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
