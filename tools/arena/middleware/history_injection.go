package middleware

import (
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HistoryInjectionMiddleware prepends conversation history to the message list.
// This is useful for self-play scenarios where history needs to be explicitly provided
// before the LLM generates the next user message.
//
// The middleware preserves any existing messages in the ExecutionContext and prepends
// the provided history, maintaining chronological order.
func HistoryInjectionMiddleware(history []types.Message) pipeline.Middleware {
	return &historyInjectionMiddleware{history: history}
}

type historyInjectionMiddleware struct {
	history []types.Message
}

func (m *historyInjectionMiddleware) Process(ctx *pipeline.ExecutionContext, next func() error) error {
	// Prepend history before any new messages
	if len(m.history) > 0 {
		existingMessages := ctx.Messages
		ctx.Messages = make([]types.Message, 0, len(m.history)+len(existingMessages))
		ctx.Messages = append(ctx.Messages, m.history...)
		ctx.Messages = append(ctx.Messages, existingMessages...)
	}
	return next()
}

func (m *historyInjectionMiddleware) StreamChunk(ctx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// History injection middleware doesn't process chunks
	return nil
}
