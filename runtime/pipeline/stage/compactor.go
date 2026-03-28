package stage

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tokenizer"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	defaultCompactThreshold = 0.70
	defaultPinRecentCount   = 4
	// DefaultBudgetTokens is the default context window budget for compaction.
	DefaultBudgetTokens      = 128000 // sensible default for modern models
	compactedPreviewMaxBytes = 100
	compactedMarker          = "compacted"
	roleTool                 = "tool"
)

// CompactResult contains the output of a compaction pass.
type CompactResult struct {
	Messages        []types.Message
	OriginalTokens  int
	CompactedTokens int
	MessagesFolded  int
}

// ContextCompactor folds stale tool results between rounds to keep context
// bounded. Deterministic, zero LLM calls. Only modifies the in-memory message
// slice sent to the provider — the persisted log (via MessageLog) retains the
// full uncompacted history.
type ContextCompactor struct {
	// BudgetTokens is the context window size. When 0, compaction is disabled.
	BudgetTokens int

	// Threshold is the fraction of BudgetTokens above which compaction triggers.
	// Default: 0.70 (compact when usage exceeds 70% of budget).
	Threshold float64

	// PinRecentCount is the number of recent messages to preserve verbatim.
	// Default: 4 (2 round-trips).
	PinRecentCount int
}

// Compact folds stale tool results to reduce token usage.
// lastInputTokens is the actual token count from the provider's CostInfo.InputTokens.
// When > 0, used instead of heuristic counting (more accurate). Pass 0 for heuristic only.
// Safe to call on a nil receiver (returns messages unchanged).
func (c *ContextCompactor) Compact(messages []types.Message, lastInputTokens int) CompactResult {
	noOp := CompactResult{Messages: messages}
	if c == nil || c.BudgetTokens <= 0 || len(messages) == 0 {
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

	budget := int(float64(c.BudgetTokens) * threshold)

	// Use actual token count from provider if available (more accurate)
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

	totalTokens := originalTokens
	messagesFolded := 0
	for i := 0; i < pinBoundary && totalTokens > budget; i++ {
		msg := &compacted[i]

		if !isCompactable(msg) {
			continue
		}

		beforeTokens := tokenizer.CountMessageTokensDefault([]types.Message{*msg})
		folded := foldToolResult(msg)
		msg.Content = folded
		msg.Parts = nil // clear multimodal parts
		// Update ToolResult.Parts so GetContent() (which reads from ToolResult
		// for tool messages) returns the folded content for token counting.
		if msg.ToolResult != nil {
			msg.ToolResult.Parts = []types.ContentPart{
				{Type: types.ContentTypeText, Text: &folded},
			}
		}
		afterTokens := tokenizer.CountMessageTokensDefault([]types.Message{*msg})

		totalTokens -= beforeTokens - afterTokens
		messagesFolded++
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

// isCompactable returns true if a message can be folded.
func isCompactable(msg *types.Message) bool {
	if msg.Role != roleTool {
		return false
	}
	if msg.ToolResult != nil && msg.ToolResult.Error != "" {
		return false
	}
	// Already compacted or too small to bother — skip.
	// Use GetContent() which reads from ToolResult.Parts for tool messages.
	content := msg.GetContent()
	if len(content) < 50 || strings.Contains(content, compactedMarker) {
		return false
	}
	return true
}

// foldToolResult creates a compact summary of a tool result.
func foldToolResult(msg *types.Message) string {
	name := ""
	if msg.ToolResult != nil {
		name = msg.ToolResult.Name
	}
	if name == "" {
		name = "tool"
	}

	// Handle multimodal parts
	if len(msg.Parts) > 0 {
		totalBytes := 0
		for _, part := range msg.Parts {
			if part.Text != nil {
				totalBytes += len(*part.Text)
			}
			if part.Media != nil && part.Media.Data != nil {
				totalBytes += len(*part.Media.Data)
			}
		}
		return fmt.Sprintf("[%s: %d parts, %d bytes %s]",
			name, len(msg.Parts), totalBytes, compactedMarker)
	}

	// Text-only: preview + size
	content := msg.GetContent()
	originalBytes := len(content)

	preview := content
	if len(preview) > compactedPreviewMaxBytes {
		preview = preview[:compactedPreviewMaxBytes] + "..."
	}

	return fmt.Sprintf("[%s: %s — %d bytes %s]",
		name, preview, originalBytes, compactedMarker)
}
