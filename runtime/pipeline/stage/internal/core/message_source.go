package core

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// Message.Source values that mark a message as history: loaded from the state
// store, a summary of earlier turns, or retrieved for context. A message with
// any other Source is new in this Send.
const (
	// SourceStateStore marks a message loaded from the state store.
	SourceStateStore = "statestore"
	// SourceSummary marks a summary of earlier turns.
	SourceSummary = "summary"
	// SourceRetrieved marks a message retrieved for context.
	SourceRetrieved = "retrieved"
)

// IsNewMessage reports whether a message originated from this Send (true)
// or was loaded from history by an upstream stage (false).
func IsNewMessage(m *types.Message) bool {
	switch m.Source {
	case SourceStateStore, SourceSummary, SourceRetrieved:
		return false
	default:
		return true
	}
}
