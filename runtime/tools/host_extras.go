package tools

// HostExtras carries top-level fields from a tool invocation's args that
// fell outside the executor's typed schema. Populated by capability
// executors when sdk.WithToolDescriptorOverride extends a built-in
// tool's InputSchema with deployment-specific fields, so the LLM-supplied
// extension data reaches the host callback that receives the executor's
// typed record.
//
// Contract:
//   - The runtime writes this map exactly once per call, at the executor's
//     arg-decode step (via DecodeArgsExtras). Any other write site inside
//     PromptKit is a bug.
//   - The host reads it from whichever typed record carries it
//     (e.g. *workflow.TransitionResult inside an OnCommit callback).
//   - The field is named HostExtras, not Metadata, to keep this channel
//     distinct from a record's first-class metadata (which has its own
//     domain semantics — see Memory.Metadata, Message.Metadata).
//
// Do not use HostExtras as a general-purpose bag for PromptKit-internal
// state. If the runtime needs to carry something on a typed record, add
// a typed field.
type HostExtras map[string]any
