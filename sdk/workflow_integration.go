package sdk

import (
	"context"
	"fmt"
	"strings"
)

// maxSummaryContentLen is the max characters per message in a carry-forward summary.
const maxSummaryContentLen = 200

// buildContextSummary creates a text summary of the previous state's conversation
// for injection into the next state. It includes the state name, turn count,
// and the last few message exchanges.
//
// This function requires a live Conversation with an active session (calls conv.Messages),
// so it is tested via integration tests rather than unit tests.
func buildContextSummary(stateName string, conv *Conversation) string {
	ctx := context.Background()
	messages := conv.Messages(ctx)
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[Previous state: %s, %d messages]\n", stateName, len(messages))

	// Include the last N messages (skip system messages)
	relevant := filterRelevantMessages(messages)
	start := 0
	if len(relevant) > defaultMaxSummaryMessages {
		start = len(relevant) - defaultMaxSummaryMessages
	}

	for i := start; i < len(relevant); i++ {
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
