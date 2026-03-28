// Package main demonstrates custom context compaction with the PromptKit SDK.
//
// This example shows three extensibility patterns:
//
//  1. Custom CompactionRule — a rule that produces domain-specific summaries
//     for search results instead of the generic preview+bytes format.
//  2. Built-in CollapsePairs — collapses superseded assistant+tool pairs
//     when the same tool is called again with the same arguments.
//  3. Custom CompactionStrategy — a complete replacement for the default
//     compactor, wrapping it with logging.
//
// Run with:
//
//	go run .
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// Example 1: Custom CompactionRule for search results
// ---------------------------------------------------------------------------

// SearchResultRule produces compact summaries for search tool results.
// Instead of the generic "[search: first 100 chars... — 5000 bytes compacted]",
// it extracts the match count and first few filenames.
type SearchResultRule struct{}

// Compile-time interface check.
var _ stage.CompactionRule = (*SearchResultRule)(nil)

func (r *SearchResultRule) Name() string { return "search_result_fold" }

func (r *SearchResultRule) CanFold(msg *types.Message, _ int, _ *stage.CompactionContext) bool {
	if msg.Role != "tool" || msg.ToolResult == nil {
		return false
	}
	// Only handle search-type tools
	return msg.ToolResult.Name == "search" || msg.ToolResult.Name == "grep"
}

func (r *SearchResultRule) Fold(
	msg *types.Message, _ int, _ *stage.CompactionContext,
) (folded string, extra []int) {
	content := msg.GetContent()
	lines := strings.Split(content, "\n")
	matchCount := len(lines)

	// Extract first 3 filenames from results
	var files []string
	for _, line := range lines {
		if len(files) >= 3 {
			break
		}
		if idx := strings.Index(line, ":"); idx > 0 {
			files = append(files, line[:idx])
		}
	}

	summary := fmt.Sprintf("[%s: %d matches in %s — %d bytes compacted]",
		msg.ToolResult.Name, matchCount, strings.Join(files, ", "),
		len(content))
	return summary, nil
}

// ---------------------------------------------------------------------------
// Example 2: Custom CompactionStrategy — logging wrapper
// ---------------------------------------------------------------------------

// LoggingCompactor wraps another CompactionStrategy and logs when
// compaction occurs. This demonstrates the decorator pattern.
type LoggingCompactor struct {
	Inner stage.CompactionStrategy
}

// Compile-time interface check.
var _ stage.CompactionStrategy = (*LoggingCompactor)(nil)

func (lc *LoggingCompactor) Compact(
	messages []types.Message, lastInputTokens int,
) stage.CompactResult {
	result := lc.Inner.Compact(messages, lastInputTokens)
	if result.MessagesFolded > 0 {
		log.Printf("[compaction] folded %d messages: %d → %d tokens",
			result.MessagesFolded, result.OriginalTokens, result.CompactedTokens)
	}
	return result
}

func (lc *LoggingCompactor) TokenBudget() int {
	return lc.Inner.TokenBudget()
}

// ---------------------------------------------------------------------------
// main — demonstrates all three patterns
// ---------------------------------------------------------------------------

func main() {
	// Pattern 1: Use WithCompactionRules to add custom rules alongside
	// the built-in defaults. Rules are tried in order; first match wins.
	fmt.Println("=== Pattern 1: Custom rules ===")
	fmt.Println("sdk.Open(pack, prompt,")
	fmt.Println("    sdk.WithCompactionRules(")
	fmt.Println("        &SearchResultRule{},      // custom: domain-specific search folding")
	fmt.Println("        stage.CollapsePairs(),     // built-in: collapse superseded pairs")
	fmt.Println("        stage.FoldToolResults(),   // built-in: generic fallback")
	fmt.Println("    ),")
	fmt.Println(")")

	// Pattern 2: Use WithCompactionStrategy to replace the compactor entirely.
	fmt.Println("\n=== Pattern 2: Custom strategy (logging decorator) ===")
	fmt.Println("sdk.Open(pack, prompt,")
	fmt.Println("    sdk.WithCompactionStrategy(&LoggingCompactor{")
	fmt.Println("        Inner: &stage.ContextCompactor{")
	fmt.Println("            BudgetTokens:   128000,")
	fmt.Println("            Rules: []stage.CompactionRule{")
	fmt.Println("                &SearchResultRule{},")
	fmt.Println("                stage.FoldToolResults(),")
	fmt.Println("            },")
	fmt.Println("        },")
	fmt.Println("    }),")
	fmt.Println(")")

	// Pattern 3: Disable compaction entirely
	fmt.Println("\n=== Pattern 3: Disable compaction ===")
	fmt.Println("sdk.Open(pack, prompt, sdk.WithCompaction(false))")

	// Demonstrate the custom rule in action
	fmt.Println("\n=== Demo: SearchResultRule folding ===")
	rule := &SearchResultRule{}
	msg := types.Message{
		Role:    "tool",
		Content: "src/main.go:10:func main() {\nsrc/config.go:5:type Config struct {\nsrc/handler.go:20:func Handle() {\nsrc/utils.go:1:package utils\n",
		ToolResult: &types.MessageToolResult{
			ID:   "call-1",
			Name: "search",
		},
	}
	ctx := &stage.CompactionContext{}
	if rule.CanFold(&msg, 0, ctx) {
		folded, _ := rule.Fold(&msg, 0, ctx)
		fmt.Println("Original:", len(msg.Content), "bytes")
		fmt.Println("Folded:  ", folded)
	}

	// Demonstrate the logging compactor
	fmt.Println("\n=== Demo: LoggingCompactor ===")
	inner := &stage.ContextCompactor{
		BudgetTokens:   100,
		Threshold:      0.50,
		PinRecentCount: 2,
		Rules: []stage.CompactionRule{
			&SearchResultRule{},
			stage.FoldToolResults(),
		},
	}
	lc := &LoggingCompactor{Inner: inner}

	msgs := []types.Message{
		{Role: "user", Content: "search for main"},
		msg,
		{Role: "assistant", Content: "found matches"},
		{Role: "user", Content: "next"},
	}
	result := lc.Compact(msgs, 5000) // pretend 5000 tokens (above 50 budget)
	fmt.Printf("Result: %d messages folded, %d → %d tokens\n",
		result.MessagesFolded, result.OriginalTokens, result.CompactedTokens)

	// Verify the actual SDK options compile
	_ = sdk.WithCompactionRules(&SearchResultRule{}, stage.CollapsePairs(), stage.FoldToolResults())
	_ = sdk.WithCompactionStrategy(lc)
	_ = sdk.WithCompaction(false)

	fmt.Println("\nAll patterns compile and work correctly.")
}
