package stage

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tokenizer"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// defaultLargeConversationThreshold is the message count above which
	// a warning is logged if no token budget is configured.
	defaultLargeConversationThreshold = 100

	// roleSystem is the role identifier for system messages.
	roleSystem = "system"
)

// TokenBudgetConfig configures the TokenBudgetStage.
type TokenBudgetConfig struct {
	// MaxTokens is the maximum number of tokens allowed before truncation.
	// When 0, no truncation is performed but warnings are still logged
	// for large conversations.
	MaxTokens int

	// TokenCounter is the counter to use for token estimation.
	// When nil, the default heuristic counter is used.
	TokenCounter *tokenizer.HeuristicTokenCounter

	// ReserveTokens is the number of tokens to reserve for the response.
	// This is subtracted from MaxTokens to determine the effective budget.
	// Default: 0 (no reservation).
	ReserveTokens int

	// LargeConversationThreshold is the message count above which a warning
	// is logged if no token budget is configured.
	// Default: 100.
	LargeConversationThreshold int
}

// TokenBudgetStage enforces a token budget on conversation messages before
// they are sent to the provider. When the total token count exceeds the
// configured budget, older messages are truncated while preserving the
// system prompt and the most recent messages that fit within the budget.
//
// This stage should be placed immediately before the ProviderStage in the
// pipeline to prevent context window overflow errors.
type TokenBudgetStage struct {
	BaseStage
	config    *TokenBudgetConfig
	turnState *TurnState
}

// NewTokenBudgetStage creates a new token budget enforcement stage.
func NewTokenBudgetStage(config *TokenBudgetConfig) *TokenBudgetStage {
	return NewTokenBudgetStageWithTurnState(config, nil)
}

// NewTokenBudgetStageWithTurnState creates a token budget stage that
// reads the system prompt from the supplied TurnState.
func NewTokenBudgetStageWithTurnState(config *TokenBudgetConfig, turnState *TurnState) *TokenBudgetStage {
	if config == nil {
		config = &TokenBudgetConfig{}
	}
	if config.TokenCounter == nil {
		config.TokenCounter = tokenizer.DefaultTokenCounter
	}
	if config.LargeConversationThreshold <= 0 {
		config.LargeConversationThreshold = defaultLargeConversationThreshold
	}
	return &TokenBudgetStage{
		BaseStage: NewBaseStage("token_budget", StageTypeTransform),
		config:    config,
		turnState: turnState,
	}
}

// budgetInput holds the collected input for token budget processing.
type budgetInput struct {
	messages        []types.Message
	nonMessageElems []StreamElement
	systemPrompt    string
}

// Process reads all messages, enforces the token budget, and forwards
// the (possibly truncated) messages downstream.
func (s *TokenBudgetStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	collected := s.collectInput(input)
	s.warnLargeConversation(collected.messages)

	messages := collected.messages
	if s.config.MaxTokens > 0 {
		messages = s.enforceTokenBudget(messages, collected.systemPrompt)
	}

	return s.emitResults(ctx, messages, collected.nonMessageElems, output)
}

// collectInput reads all elements from the input channel, separating
// messages from non-message elements. The system prompt is sourced from
// TurnState when wired.
func (s *TokenBudgetStage) collectInput(input <-chan StreamElement) *budgetInput {
	bi := &budgetInput{}

	for elem := range input {
		if elem.Message != nil {
			bi.messages = append(bi.messages, *elem.Message)
			continue
		}
		bi.nonMessageElems = append(bi.nonMessageElems, elem)
	}

	if s.turnState != nil {
		bi.systemPrompt = s.turnState.SystemPrompt
	}

	return bi
}

// warnLargeConversation logs a warning if the conversation exceeds the
// threshold and no token budget is configured.
func (s *TokenBudgetStage) warnLargeConversation(messages []types.Message) {
	if s.config.MaxTokens == 0 && len(messages) > s.config.LargeConversationThreshold {
		logger.Warn("Large conversation without token budget",
			"message_count", len(messages),
			"threshold", s.config.LargeConversationThreshold,
			"suggestion", "Configure WithTokenBudget to prevent context window overflow")
	}
}

// emitResults sends messages and non-message elements to the output channel.
func (s *TokenBudgetStage) emitResults(
	ctx context.Context,
	messages []types.Message,
	nonMessageElems []StreamElement,
	output chan<- StreamElement,
) error {
	for i := range messages {
		elem := NewMessageElement(&messages[i])
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	for i := range nonMessageElems {
		select {
		case output <- nonMessageElems[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// enforceTokenBudget truncates messages to fit within the token budget.
// It preserves system messages at the start and keeps the most recent
// messages that fit within the budget.
func (s *TokenBudgetStage) enforceTokenBudget(
	messages []types.Message,
	systemPrompt string,
) []types.Message {
	effectiveBudget := s.config.MaxTokens - s.config.ReserveTokens
	if effectiveBudget <= 0 {
		return messages
	}

	totalTokens := s.config.TokenCounter.CountMessageTokens(messages)

	// Account for system prompt tokens
	if systemPrompt != "" {
		totalTokens += s.config.TokenCounter.CountTokensContentAware(systemPrompt)
	}

	if totalTokens <= effectiveBudget {
		return messages
	}

	logger.Warn("Token budget exceeded, truncating messages",
		"total_tokens", totalTokens,
		"budget", effectiveBudget,
		"message_count", len(messages))

	return s.truncateMessages(messages, systemPrompt, effectiveBudget)
}

// truncateMessages removes old non-system messages to fit within the budget.
// It keeps system messages at the start and fills from the most recent
// messages backward until the budget is exhausted.
//
// Token counts are pre-computed once per message and reused via a running
// sum to avoid re-counting during selection.
func (s *TokenBudgetStage) truncateMessages(
	messages []types.Message,
	systemPrompt string,
	budget int,
) []types.Message {
	// Pre-compute per-message token counts once.
	tokenCounts := s.precomputeTokenCounts(messages)

	// Separate system messages from conversation messages, tracking
	// token totals via the precomputed counts.
	var systemMsgs []types.Message
	var conversationMsgs []types.Message
	var conversationTokens []int

	systemTokens := 0
	for i := range messages {
		if messages[i].Role == roleSystem {
			systemMsgs = append(systemMsgs, messages[i])
			systemTokens += tokenCounts[i]
		} else {
			conversationMsgs = append(conversationMsgs, messages[i])
			conversationTokens = append(conversationTokens, tokenCounts[i])
		}
	}

	// Account for system prompt tokens.
	if systemPrompt != "" {
		systemTokens += s.config.TokenCounter.CountTokensContentAware(systemPrompt)
	}

	remainingBudget := budget - systemTokens
	if remainingBudget <= 0 {
		// System messages alone exceed the budget; return them anyway
		logger.Warn("System messages exceed token budget",
			"system_tokens", systemTokens,
			"budget", budget)
		return systemMsgs
	}

	// Fill from most recent messages backward using precomputed counts.
	kept := s.selectRecentMessagesPrecomputed(conversationMsgs, conversationTokens, remainingBudget)

	originalCount := len(messages)
	truncatedCount := originalCount - len(systemMsgs) - len(kept)

	logger.Warn("Messages truncated to fit token budget",
		"original_count", originalCount,
		"kept_count", len(systemMsgs)+len(kept),
		"truncated_count", truncatedCount,
		"budget", budget)

	result := make([]types.Message, 0, len(systemMsgs)+len(kept))
	result = append(result, systemMsgs...)
	result = append(result, kept...)
	return result
}

// precomputeTokenCounts returns per-message token counts so that subsequent
// logic can use a running sum instead of re-counting.
func (s *TokenBudgetStage) precomputeTokenCounts(messages []types.Message) []int {
	counts := make([]int, len(messages))
	for i := range messages {
		counts[i] = s.config.TokenCounter.CountMessageTokens(messages[i : i+1])
	}
	return counts
}

// selectRecentMessages picks the most recent messages that fit within
// the given token budget, working backward from the end.
func (s *TokenBudgetStage) selectRecentMessages(
	messages []types.Message,
	budget int,
) []types.Message {
	counts := s.precomputeTokenCounts(messages)
	return s.selectRecentMessagesPrecomputed(messages, counts, budget)
}

// selectRecentMessagesPrecomputed picks the most recent messages using
// precomputed per-message token counts, avoiding redundant counting.
func (s *TokenBudgetStage) selectRecentMessagesPrecomputed(
	messages []types.Message,
	tokenCounts []int,
	budget int,
) []types.Message {
	usedTokens := 0
	startIdx := len(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		if usedTokens+tokenCounts[i] > budget {
			break
		}
		usedTokens += tokenCounts[i]
		startIdx = i
	}

	return messages[startIdx:]
}
