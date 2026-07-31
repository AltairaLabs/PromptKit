package stage

import "context"

// Handoff describes a workflow state change that took effect mid-turn.
//
// Changed is false when no transition was pending, in which case the other
// fields are meaningless and the caller must leave the turn's system prompt
// and tool set alone.
type Handoff struct {
	// Changed reports whether a pending transition was committed.
	Changed bool
	// SystemPrompt is the destination state's rendered system prompt.
	SystemPrompt string
	// AllowedTools is the destination state's allowed-tool list, used to
	// rebuild the provider tool set from the registry.
	AllowedTools []string
}

// WorkflowStateResolver lets a workflow consumer advance the conversation's
// state in the middle of a tool loop.
//
// A workflow state change is exactly two things: a different system prompt and
// a different tool set. Both are already per-round inputs to the provider loop,
// so committing a transition between rounds is sufficient to make the next
// round run as the destination state — no new conversation, no new pipeline,
// and no user message required. Without this, a transition advances the state
// machine and the destination state never speaks (see
// docs/local-backlog/WORKFLOW_IN_TURN_STATE_HANDOFF_DESIGN.md).
//
// runtime must not import sdk, so the interface is defined here and
// implemented by consumers (SDK WorkflowConversation, Arena's per-run
// transition executor). Nil is the non-workflow case and is always safe.
type WorkflowStateResolver interface {
	// ResolvePendingHandoff commits any transition left pending by the
	// workflow transition tool and returns the destination state's prompt and
	// allowed tools. It is called after each round's tool results are appended
	// and before the next provider call.
	//
	// Implementations must return Changed=false — not an error — when no
	// transition is pending, when the destination state is externally
	// orchestrated (RFC 0009: the runtime pauses for an injected event),
	// terminal, or composition-orchestrated.
	ResolvePendingHandoff(ctx context.Context) (Handoff, error)

	// CurrentStateMeta returns metadata describing the state that is active
	// right now, for stamping onto the assistant message a round produced.
	// Returns nil when there is nothing to record.
	CurrentStateMeta() map[string]any
}

// workflowStateMetaKey is the message metadata key under which the active
// workflow state is recorded for each assistant message. Consumers read it to
// attribute output to the state that generated it; a turn may span several
// states, so per-turn attribution is not sufficient.
const workflowStateMetaKey = "current_workflow_state"
