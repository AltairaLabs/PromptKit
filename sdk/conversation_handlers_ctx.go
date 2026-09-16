package sdk

import "context"

type conversationHandlersKey struct{}

// conversationHandlers carries the calling conversation's live tool-handler
// accessors, so the executors registered in a tools.Registry can dispatch to
// the right conversation's handlers.
//
// A Registry keys executors by name and keeps exactly one per name. The SDK
// builds localExecutor and clientExecutor per conversation, each holding a
// snapshot of that conversation's handlers and a live accessor pointing back at
// it -- so when two conversations share a registry (WithToolRegistry), the
// second one's registration overwrites the first's, and the first's tool calls
// dispatch through the second's handlers. Reading the accessors from the
// context instead makes the registered instance irrelevant.
// See AltairaLabs/PromptKit#2011.
type conversationHandlers struct {
	local  *localHandlersAccessor
	client *clientHandlersMuAccessor
}

// withLocalHandlers attaches the conversation's handler accessors to ctx.
func withLocalHandlers(ctx context.Context, c *Conversation) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, conversationHandlersKey{}, &conversationHandlers{
		local:  &localHandlersAccessor{conv: c},
		client: &clientHandlersMuAccessor{conv: c},
	})
}

// conversationHandlersFromContext returns the accessors attached by
// [withLocalHandlers], or nil when the context carries none.
func conversationHandlersFromContext(ctx context.Context) *conversationHandlers {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(conversationHandlersKey{}).(*conversationHandlers)
	return h
}
