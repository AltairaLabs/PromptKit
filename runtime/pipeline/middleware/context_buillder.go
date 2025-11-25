package middleware

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TruncationStrategy defines how to handle messages when over token budget
type TruncationStrategy string

const (
	// TruncateOldest drops oldest messages first (simple, preserves recent context)
	TruncateOldest TruncationStrategy = "oldest"

	// TruncateLeastRelevant drops least relevant messages (requires embeddings)
	TruncateLeastRelevant TruncationStrategy = "relevance"

	// TruncateSummarize compresses old messages into summaries
	TruncateSummarize TruncationStrategy = "summarize"

	// TruncateFail returns error if over budget (strict mode)
	TruncateFail TruncationStrategy = "fail"
)

// ContextBuilderPolicy defines token budget and truncation behavior
type ContextBuilderPolicy struct {
	TokenBudget      int                // Max tokens for context (0 = unlimited)
	ReserveForOutput int                // Reserve tokens for response
	Strategy         TruncationStrategy // How to handle overflow
	CacheBreakpoints bool               // Insert cache markers (Anthropic)
}

// contextBuilderMiddleware manages conversation context with token budget enforcement
type contextBuilderMiddleware struct {
	policy *ContextBuilderPolicy
}

// ContextBuilderMiddleware manages conversation context with token budget enforcement
// This middleware should be placed BEFORE ProviderMiddleware in the pipeline
func ContextBuilderMiddleware(policy *ContextBuilderPolicy) pipeline.Middleware {
	return &contextBuilderMiddleware{policy: policy}
}

// Process manages conversation context by enforcing token budget limits and applying truncation strategies.
// This middleware should be placed before ProviderMiddleware in the pipeline.
// If messages exceed the token budget, it applies the configured truncation strategy.
func (m *contextBuilderMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// No policy specified, pass through without modification
	if m.policy == nil {
		return next()
	}

	// Skip if no budget limit (0 = unlimited)
	if m.policy.TokenBudget <= 0 {
		return next()
	}

	// Calculate available budget
	available := m.policy.TokenBudget - m.policy.ReserveForOutput
	systemTokens := countTokens(execCtx.SystemPrompt)
	available -= systemTokens

	if available <= 0 {
		return fmt.Errorf("token budget too small: need at least %d for system prompt", systemTokens)
	}

	// Calculate current token usage
	currentTokens := countMessagesTokens(execCtx.Messages)

	// If under budget, no truncation needed
	if currentTokens <= available {
		return next()
	}

	// Apply truncation strategy
	truncated, err := truncateMessages(execCtx.Messages, available, m.policy.Strategy)
	if err != nil {
		return fmt.Errorf("context middleware: %w", err)
	}

	// Store metadata about truncation
	originalCount := len(execCtx.Messages)
	if len(truncated) < originalCount {
		execCtx.Metadata["context_truncated"] = true
		execCtx.Metadata["context_original_count"] = originalCount
		execCtx.Metadata["context_truncated_count"] = len(truncated)
		execCtx.Metadata["context_dropped_count"] = originalCount - len(truncated)
	}

	// Replace messages with truncated version
	execCtx.Messages = truncated

	// Insert cache breakpoint on system prompt if enabled (Anthropic)
	// This is handled by the provider middleware, but we set a flag
	if m.policy.CacheBreakpoints {
		execCtx.Metadata["enable_cache_breakpoints"] = true
	}

	// Continue to next middleware
	return next()
}

// StreamChunk is a no-op for context builder middleware as it doesn't process stream chunks.
func (m *contextBuilderMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Context builder middleware doesn't process chunks
	return nil
}

// countTokens estimates token count for text using a simple heuristic
// This is a rough approximation: words * 1.3 to account for subword tokenization
// For production, consider using tiktoken or similar
func countTokens(text string) int {
	if text == "" {
		return 0
	}
	words := strings.Fields(text)
	return int(float64(len(words)) * 1.3)
}

// countMessagesTokens estimates total tokens for messages
func countMessagesTokens(messages []types.Message) int {
	total := 0
	for i := range messages {
		total += countTokens(messages[i].Content)
		// Add tokens for tool calls (rough estimate)
		for _, tc := range messages[i].ToolCalls {
			total += countTokens(string(tc.Args))
		}
	}
	return total
}

// truncateMessages applies truncation strategy
func truncateMessages(messages []types.Message, budget int, strategy TruncationStrategy) ([]types.Message, error) {
	switch strategy {
	case TruncateOldest:
		return truncateOldest(messages, budget), nil
	case TruncateLeastRelevant:
		return truncateLeastRelevant(messages, budget), nil
	case TruncateSummarize:
		return truncateSummarize(messages, budget), nil
	case TruncateFail:
		return nil, fmt.Errorf("token budget exceeded: have %d, budget %d", countMessagesTokens(messages), budget)
	default:
		return truncateOldest(messages, budget), nil
	}
}

// truncateOldest keeps most recent messages that fit budget
func truncateOldest(messages []types.Message, budget int) []types.Message {
	// Start from most recent, work backwards
	var result []types.Message
	used := 0

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		msgTokens := countTokens(msg.Content)

		// Add tool call tokens
		for _, tc := range msg.ToolCalls {
			msgTokens += countTokens(string(tc.Args))
		}

		if used+msgTokens > budget {
			break
		}

		result = append([]types.Message{msg}, result...) // Prepend
		used += msgTokens
	}

	return result
}

// truncateLeastRelevant keeps most relevant messages (placeholder - needs embeddings)
func truncateLeastRelevant(messages []types.Message, budget int) []types.Message {
	// TODO: Implement with embedding-based relevance scoring
	// For now, fall back to oldest strategy
	return truncateOldest(messages, budget)
}

// truncateSummarize compresses old messages (placeholder - needs LLM)
func truncateSummarize(messages []types.Message, budget int) []types.Message {
	// TODO: Implement with LLM-based summarization
	// For now, fall back to oldest strategy
	return truncateOldest(messages, budget)
}

// GetContextMetadata extracts context truncation metadata from ExecutionContext
func GetContextMetadata(execCtx *pipeline.ExecutionContext) (truncated bool, originalCount, truncatedCount int) {
	if truncated, ok := execCtx.Metadata["context_truncated"].(bool); ok && truncated {
		origCount, _ := execCtx.Metadata["context_original_count"].(int)
		truncCount, _ := execCtx.Metadata["context_truncated_count"].(int)
		return true, origCount, truncCount
	}
	return false, 0, 0
}
