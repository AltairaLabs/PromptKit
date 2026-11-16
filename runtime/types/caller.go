package types

// Caller is who a conversation acts for. A host sets it once (sdk.WithCaller)
// and PromptKit puts it on the requests it builds; it is never carried on
// context.Context. PromptKit hands it only to the host's own code at its
// action points: tool executors, tool hooks and session hooks, including exec
// hooks, which receive it in the JSON on their stdin (as "Caller" with "ID"
// and "Claims"). Otherwise PromptKit never persists, logs or emits it: it is
// not in events, spans, the state store or recordings. PromptKit never reads
// ID or Claims itself.
type Caller struct {
	// ID identifies the caller in the host's own terms.
	ID string
	// Claims carries host-defined attributes of the caller. When exec hooks
	// are configured, Claims must be JSON-encodable data: a value
	// encoding/json cannot encode (a func, a channel, a cycle) makes an exec
	// filter hook deny the call with a marshal error and an exec observe hook
	// skip it. Receivers must treat Claims as read-only: one map is shared by
	// parallel tool calls, forks and hooks.
	Claims map[string]any
}

// IsZero reports whether no caller was set.
func (c Caller) IsZero() bool {
	return c.ID == "" && len(c.Claims) == 0
}
