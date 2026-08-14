package stage

import "context"

// Handoff describes the prompt and tool set the workflow's current state needs.
//
// It is a statement of what the turn *should* be running, not a record that
// something changed — the stage compares it against what the turn is actually
// running and swaps only on a mismatch. That distinction matters: a pipeline
// re-execution (HITL resume, deferred client tool) re-runs PromptAssemblyStage,
// which resets the turn's prompt to the state the pipeline was built for. A
// "did a transition just happen" signal is unobservable by then; a "what should
// be running" signal self-corrects.
type Handoff struct {
	// Valid reports whether SystemPrompt and AllowedTools are meaningful.
	// False when there is no workflow state to apply.
	Valid bool
	// Stop ends the turn without a further provider round. Set for states the
	// runtime must not continue into — externally orchestrated states pause for
	// an injected event (RFC 0009), and composition states are run by
	// CompositionStage rather than the tool loop.
	Stop bool
	// SystemPrompt is the current state's rendered system prompt.
	SystemPrompt string
	// AllowedTools is the current state's allowed-tool list, used to rebuild
	// the provider tool set from the registry.
	AllowedTools []string
}

// WorkflowStateResolver lets a workflow consumer keep a turn aligned with the
// workflow's current state.
//
// A workflow state change is exactly two things: a different system prompt and
// a different tool set. Both are already per-round inputs to the provider tool
// loop, so reconciling them between rounds is sufficient to make the turn run
// as the current state — no new conversation, no new pipeline, and no user
// message required. Without this, a transition advances the state machine and
// the destination state never speaks (see
// docs/local-backlog/WORKFLOW_IN_TURN_STATE_HANDOFF_DESIGN.md).
//
// runtime must not import sdk, so the interface is defined here and implemented
// by consumers (SDK WorkflowConversation, Arena's per-run transition executor).
// Nil is the non-workflow case and is always safe.
type WorkflowStateResolver interface {
	// ResolveCurrentState commits any transition left pending by the workflow
	// transition tool, then reports the prompt and tools for the state the
	// workflow is now in.
	//
	// It is called at the start of each pipeline execution and after each
	// round's tool results, and must be safe to call repeatedly: callers rely
	// on it being idempotent when nothing has changed.
	ResolveCurrentState(ctx context.Context) (Handoff, error)

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
