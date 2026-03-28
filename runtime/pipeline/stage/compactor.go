package stage

import (
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tokenizer"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultCompactThreshold = 0.70
	defaultPinRecentCount   = 4
	// DefaultBudgetTokens is the default context window budget for compaction.
	DefaultBudgetTokens = 128000 // sensible default for modern models
	compactedMarker     = "compacted"
)

// CompactResult contains the output of a compaction pass.
type CompactResult struct {
	Messages        []types.Message
	OriginalTokens  int
	CompactedTokens int
	MessagesFolded  int
}

// CompactionStrategy is the top-level interface for context compaction.
// Called by ProviderStage between tool loop rounds. Implementations must
// be safe for concurrent use if the provider stage is used concurrently
// across conversations (each conversation has its own message slice).
type CompactionStrategy interface {
	Compact(messages []types.Message, lastInputTokens int) CompactResult
	// BudgetTokens returns the configured token budget for event emission.
	BudgetTokens() int
}

// CompactionRule transforms individual messages during compaction.
// Rules are applied in order to each compactable message outside the
// pinned window. The first rule whose CanFold returns true wins.
type CompactionRule interface {
	Name() string
	// CanFold reports whether this rule can compact the message at idx.
	CanFold(msg *types.Message, idx int, ctx *CompactionContext) bool
	// Fold returns the compacted content for the message, plus indices of
	// additional messages to remove (e.g., the preceding assistant message
	// in a pair collapse). Return nil for no additional removals.
	Fold(msg *types.Message, idx int, ctx *CompactionContext) (string, []int)
}

// CompactionContext provides read-only context to rules.
type CompactionContext struct {
	Messages    []types.Message // full message slice (read-only view of the copy)
	PinBoundary int             // messages at/after this index are pinned
	Budget      int             // target token count
	Current     int             // current estimated token count
}

// ContextCompactor is the default CompactionStrategy. It applies rules in
// order to fold stale messages until context is under the token budget.
// Deterministic, zero LLM calls.
type ContextCompactor struct {
	// BudgetTokensValue is the context window size. When 0, compaction is disabled.
	BudgetTokensValue int

	// Threshold is the fraction of BudgetTokensValue above which compaction triggers.
	// Default: 0.70 (compact when usage exceeds 70% of budget).
	Threshold float64

	// PinRecentCount is the number of recent messages to preserve verbatim.
	// Default: 4 (2 round-trips).
	PinRecentCount int

	// Rules are applied in order; first match wins. When nil/empty,
	// defaults to [FoldToolResults()].
	Rules []CompactionRule
}

// BudgetTokens returns the configured token budget.
func (c *ContextCompactor) BudgetTokens() int {
	if c == nil {
		return 0
	}
	return c.BudgetTokensValue
}

// Compact applies rules to fold stale messages until under budget.
// Safe to call on a nil receiver (returns messages unchanged).
func (c *ContextCompactor) Compact(messages []types.Message, lastInputTokens int) CompactResult {
	noOp := CompactResult{Messages: messages}
	if c == nil || c.BudgetTokensValue <= 0 || len(messages) == 0 {
		return noOp
	}

	threshold := c.Threshold
	if threshold <= 0 {
		threshold = defaultCompactThreshold
	}
	pinCount := c.PinRecentCount
	if pinCount <= 0 {
		pinCount = defaultPinRecentCount
	}

	budget := int(float64(c.BudgetTokensValue) * threshold)

	originalTokens := lastInputTokens
	if originalTokens <= 0 {
		originalTokens = tokenizer.CountMessageTokensDefault(messages)
	}
	if originalTokens <= budget {
		return noOp
	}

	pinBoundary := len(messages) - pinCount
	if pinBoundary < 0 {
		pinBoundary = 0
	}

	compacted := make([]types.Message, len(messages))
	copy(compacted, messages)

	rules := c.Rules
	if len(rules) == 0 {
		rules = []CompactionRule{FoldToolResults()}
	}

	ctx := &CompactionContext{
		Messages:    compacted,
		PinBoundary: pinBoundary,
		Budget:      budget,
		Current:     originalTokens,
	}

	totalTokens := originalTokens
	messagesFolded := 0
	removed := map[int]bool{}

	for i := 0; i < pinBoundary && totalTokens > budget; i++ {
		if removed[i] {
			continue
		}
		msg := &compacted[i]

		ctx.Current = totalTokens

		var matchedRule CompactionRule
		for _, rule := range rules {
			if rule.CanFold(msg, i, ctx) {
				matchedRule = rule
				break
			}
		}
		if matchedRule == nil {
			continue
		}

		beforeTokens := tokenizer.CountMessageTokensDefault([]types.Message{*msg})
		folded, extraRemovals := matchedRule.Fold(msg, i, ctx)
		applyFold(msg, folded)
		afterTokens := tokenizer.CountMessageTokensDefault([]types.Message{*msg})
		totalTokens -= beforeTokens - afterTokens
		messagesFolded++

		// Handle additional removals (e.g., pair collapse removes the assistant msg)
		for _, ri := range extraRemovals {
			if ri >= 0 && ri < pinBoundary && !removed[ri] {
				removed[ri] = true
				totalTokens -= tokenizer.CountMessageTokensDefault(
					[]types.Message{compacted[ri]})
				messagesFolded++
			}
		}
	}

	// Build final message slice, excluding removed indices
	if len(removed) > 0 {
		final := make([]types.Message, 0, len(compacted)-len(removed))
		for i := range compacted {
			if !removed[i] {
				final = append(final, compacted[i])
			}
		}
		compacted = final
	}

	if messagesFolded > 0 {
		logger.Debug("Context compacted",
			"original_tokens", originalTokens,
			"compacted_tokens", totalTokens,
			"messages_folded", messagesFolded,
			"budget", budget)
	}

	return CompactResult{
		Messages:        compacted,
		OriginalTokens:  originalTokens,
		CompactedTokens: totalTokens,
		MessagesFolded:  messagesFolded,
	}
}

// applyFold sets the folded content on a message, updating both Content
// and ToolResult.Parts so that GetContent() returns the folded text.
func applyFold(msg *types.Message, folded string) {
	msg.Content = folded
	msg.Parts = nil
	if msg.ToolResult != nil {
		msg.ToolResult.Parts = []types.ContentPart{
			{Type: types.ContentTypeText, Text: &folded},
		}
	}
}
