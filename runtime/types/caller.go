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
	// Claims carries host-defined attributes of the caller.
	Claims map[string]any
}

// IsZero reports whether no caller was set.
func (c Caller) IsZero() bool {
	return c.ID == "" && len(c.Claims) == 0
}
