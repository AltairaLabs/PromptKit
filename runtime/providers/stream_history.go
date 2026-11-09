package providers

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// StreamHistorySession is a realtime session that can be given the prior
// conversation before the first input, through its provider's own mechanism
// (for OpenAI Realtime, conversation.item.create; for Gemini Live, a
// client_content turn list). A resumed duplex conversation uses it so the
// model continues with memory.
type StreamHistorySession interface {
	// SeedHistory adds messages to the session's conversation without asking
	// for a response. Messages with no text (audio-only, tool calls and
	// results) are skipped; system messages are carried by the session's
	// instruction, not here.
	SeedHistory(ctx context.Context, messages []types.Message) error
}

// StreamHistorySupport is implemented by a provider whose realtime sessions
// accept prior conversation (StreamHistorySession). A caller resuming a
// conversation checks it before opening a session, so a provider that cannot
// take the history is refused instead of silently starting fresh.
type StreamHistorySupport interface {
	SupportsStreamHistory() bool
}

// HistoryTurn is one message of prior conversation reduced to what a
// realtime session can replay: its role (user or assistant) and text.
type HistoryTurn struct {
	Role string
	Text string
}

// HistoryTurns reduces messages to the user and assistant turns that carry
// text, in order. It is the replay rule every StreamHistorySession follows.
func HistoryTurns(messages []types.Message) []HistoryTurn {
	turns := make([]HistoryTurn, 0, len(messages))
	for i := range messages {
		role := messages[i].Role
		if role != "user" && role != roleAssistant {
			continue
		}
		if text := messages[i].GetContent(); text != "" {
			turns = append(turns, HistoryTurn{Role: role, Text: text})
		}
	}
	return turns
}
