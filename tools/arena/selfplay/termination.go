package selfplay

import (
	"errors"
	"strings"
)

// CompletionMarker is the token the self-play LLM emits to signal the conversation is complete.
const CompletionMarker = "[CONVERSATION_COMPLETE]"

// ErrConversationComplete is returned when the self-play LLM signals the conversation is done.
var ErrConversationComplete = errors.New("conversation complete")

// CompletionInstruction is appended to the persona system prompt when natural termination is enabled.
const CompletionInstruction = `

When you feel the conversation has reached a natural conclusion —
the user's question has been fully answered, the topic is resolved,
or continuing would be repetitive — end your message with the exact
marker: [CONVERSATION_COMPLETE]
Only use this marker after the minimum number of turns has been
reached. Do not use it prematurely.`

// DetectAndStripCompletion checks whether content contains the completion marker.
// Returns the cleaned content (marker removed) and whether the marker was detected.
func DetectAndStripCompletion(content string) (string, bool) {
	if !strings.Contains(content, CompletionMarker) {
		return content, false
	}
	cleaned := strings.ReplaceAll(content, CompletionMarker, "")
	cleaned = strings.TrimSpace(cleaned)
	return cleaned, true
}
