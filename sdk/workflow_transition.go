package sdk

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// maxSummaryContentLen is the max characters per message in a carry-forward summary.
const maxSummaryContentLen = 200

// summarizeMessages formats a slice of messages into a carry-forward summary.
func summarizeMessages(stateName string, messages []types.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Previous state: %s, %d messages]\n", stateName, len(messages))

	// Collect the last N non-system messages by iterating from the end,
	// avoiding a full filter + slice allocation for long histories.
	relevant := make([]types.Message, 0, defaultMaxSummaryMessages)
	for i := len(messages) - 1; i >= 0 && len(relevant) < defaultMaxSummaryMessages; i-- {
		if messages[i].Role != roleSystem {
			relevant = append(relevant, messages[i])
		}
	}
	// Reverse to restore chronological order
	for i, j := 0, len(relevant)-1; i < j; i, j = i+1, j-1 {
		relevant[i], relevant[j] = relevant[j], relevant[i]
	}

	for i := 0; i < len(relevant); i++ {
		content := extractMessageText(&relevant[i])
		if content == "" {
			continue
		}
		if len(content) > maxSummaryContentLen {
			content = content[:maxSummaryContentLen] + "..."
		}
		fmt.Fprintf(&sb, "%s: %s\n", relevant[i].Role, content)
	}

	return sb.String()
}
