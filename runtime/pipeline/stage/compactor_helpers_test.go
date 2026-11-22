package stage

import (
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The compactor's tests live in internal/compaction; these builders are what
// stage's own tests share with them.

func toolResultMsg(name, content string) types.Message {
	tr := types.NewTextToolResult("call-"+name, name, content)
	return types.NewToolResultMessage(tr)
}

func largeToolResult(name string, wordCount int) types.Message {
	// Use realistic content with spaces so token counting works (~1.3 tokens/word)
	content := strings.Repeat("the quick brown fox jumps over the lazy dog ", wordCount/9+1)
	maxLen := wordCount * 6
	if maxLen > len(content) {
		maxLen = len(content)
	}
	return toolResultMsg(name, content[:maxLen])
}
