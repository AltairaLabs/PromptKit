package tools

import "context"

type callIDKeyType struct{}

var callIDKey = callIDKeyType{}

// WithCallID returns a new context that carries the tool call ID.
// This is set by the pipeline before executing a tool so that
// executors can access the provider-assigned call ID.
func WithCallID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callIDKey, id)
}

// CallIDFromContext extracts the tool call ID from the context.
// Returns an empty string if no call ID is set.
func CallIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(callIDKey).(string); ok {
		return id
	}
	return ""
}
