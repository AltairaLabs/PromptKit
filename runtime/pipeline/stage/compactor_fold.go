package stage

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	compactedPreviewMaxBytes = 100
	roleTool                 = "tool"
	minCompactableBytes      = 50
)

// foldToolResultsRule is the default compaction rule. It replaces large
// tool results with a compact summary containing the tool name, a content
// preview, and the original byte count.
type foldToolResultsRule struct{}

// FoldToolResults returns the default compaction rule that folds large
// tool result messages into compact summaries.
func FoldToolResults() CompactionRule { return &foldToolResultsRule{} }

// Name implements CompactionRule.
func (r *foldToolResultsRule) Name() string { return "fold_tool_results" }

// CanFold implements CompactionRule.
func (r *foldToolResultsRule) CanFold(msg *types.Message, _ int, _ *CompactionContext) bool {
	if msg.Role != roleTool {
		return false
	}
	if msg.ToolResult != nil && msg.ToolResult.Error != "" {
		return false
	}
	content := msg.GetContent()
	if len(content) < minCompactableBytes || strings.Contains(content, compactedMarker) {
		return false
	}
	return true
}

// Fold implements CompactionRule.
func (r *foldToolResultsRule) Fold(msg *types.Message, _ int, _ *CompactionContext) (folded string, extra []int) {
	return foldToolResult(msg), nil
}

// foldToolResult creates a compact summary of a tool result.
func foldToolResult(msg *types.Message) string {
	name := ""
	if msg.ToolResult != nil {
		name = msg.ToolResult.Name
	}
	if name == "" {
		name = roleTool
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
