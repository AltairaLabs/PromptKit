package core

import "github.com/AltairaLabs/PromptKit/runtime/v2/types"

// CountUserTurns counts the user turns in a transcript.
func CountUserTurns(messages []types.Message) int {
	n := 0
	for i := range messages {
		if messages[i].Role == RoleUser {
			n++
		}
	}
	return n
}
