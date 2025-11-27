package middleware

import (
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// TurnIndexMiddleware computes role-specific turn counters from the authoritative
// conversation history loaded into execCtx.Messages (typically via StateStoreLoadMiddleware).
// It sets clear, role-specific metadata keys that other middleware can consume.
// - arena_user_completed_turns: number of completed user messages
// - arena_user_next_turn: completed user messages + 1 (next user turn to generate)
// - arena_assistant_completed_turns: number of completed assistant messages
// - arena_assistant_next_turn: completed assistant messages + 1
func TurnIndexMiddleware() pipeline.Middleware {
	return &turnIndexMiddleware{}
}

type turnIndexMiddleware struct{}

// Process computes role-specific turn counters and stores them in execCtx.Metadata.
func (m *turnIndexMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	if execCtx.Metadata == nil {
		execCtx.Metadata = make(map[string]interface{})
	}

	// If already computed, do nothing (idempotent)
	if _, ok := execCtx.Metadata["arena_user_completed_turns"]; ok {
		return next()
	}

	const roleUser = "user"
	const roleAssistant = "assistant"

	userCount := 0
	assistantCount := 0
	for i := range execCtx.Messages {
		switch execCtx.Messages[i].Role {
		case roleUser:
			userCount++
		case roleAssistant:
			assistantCount++
		}
	}

	execCtx.Metadata["arena_user_completed_turns"] = userCount
	execCtx.Metadata["arena_user_next_turn"] = userCount + 1
	execCtx.Metadata["arena_assistant_completed_turns"] = assistantCount
	execCtx.Metadata["arena_assistant_next_turn"] = assistantCount + 1
	return next()
}

// StreamChunk is a no-op for this middleware.
func (m *turnIndexMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	return nil
}
