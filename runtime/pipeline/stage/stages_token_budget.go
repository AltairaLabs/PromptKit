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
	config *TokenBudgetConfig
}

// NewTokenBudgetStage creates a new token budget enforcement stage.
func NewTokenBudgetStage(config *TokenBudgetConfig) *TokenBudgetStage {
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
// messages from non-message elements and extracting the system prompt.
func (s *TokenBudgetStage) collectInput(input <-chan StreamElement) *budgetInput {
	bi := &budgetInput{}

	for elem := range input {
		if elem.Message != nil {
			bi.messages = append(bi.messages, *elem.Message)
			continue
		}
		bi.nonMessageElems = append(bi.nonMessageElems, elem)
		if elem.Metadata != nil {
			if sp, ok := elem.Metadata["system_prompt"].(string); ok {
				bi.systemPrompt = sp
			}
		}
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
func (s *TokenBudgetStage) truncateMessages(
	messages []types.Message,
	systemPrompt string,
	budget int,
) []types.Message {
	// Separate system messages (at the start) from conversation messages
	var systemMsgs []types.Message
	var conversationMsgs []types.Message

	for i := range messages {
		if messages[i].Role == roleSystem {
			systemMsgs = append(systemMsgs, messages[i])
		} else {
			conversationMsgs = append(conversationMsgs, messages[i])
		}
	}

	// Calculate tokens used by system messages and prompt
	usedTokens := s.config.TokenCounter.CountMessageTokens(systemMsgs)
	if systemPrompt != "" {
		usedTokens += s.config.TokenCounter.CountTokensContentAware(systemPrompt)
	}

	remainingBudget := budget - usedTokens
	if remainingBudget <= 0 {
		// System messages alone exceed the budget; return them anyway
		logger.Warn("System messages exceed token budget",
			"system_tokens", usedTokens,
			"budget", budget)
		return systemMsgs
	}

	// Fill from most recent messages backward
	kept := s.selectRecentMessages(conversationMsgs, remainingBudget)

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

// selectRecentMessages picks the most recent messages that fit within
// the given token budget, working backward from the end.
func (s *TokenBudgetStage) selectRecentMessages(
	messages []types.Message,
	budget int,
) []types.Message {
	usedTokens := 0
	startIdx := len(messages)

	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := s.config.TokenCounter.CountMessageTokens(
			messages[i : i+1],
		)
		if usedTokens+msgTokens > budget {
			break
		}
		usedTokens += msgTokens
		startIdx = i
	}

	return messages[startIdx:]
}
