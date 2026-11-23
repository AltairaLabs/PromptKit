// Package provider holds the provider stage: the request/response and
// streaming call to the LLM, its tool loop (the tool runner, call-rate window,
// structured-output mode, schema resolution and workflow-state handoff), and
// the output guardrails it applies (the guardrail outcome, the blocked-turn
// message and the stream_hold engine that holds a streamed reply for its
// guardrails).
//
// It is internal to stage (#2876): stage re-exports ProviderStage, its config
// and constructors, CallRateWindow, StreamHoldConfig, the structured-output
// and blocked-message types and NewFileSchemaResolver under the same names, so
// callers keep importing stage. The realtime provider stage
// (stage/internal/duplex) builds on the tool runner, the guardrail outcome and
// the stream_hold engine, which is why those are exported here.
package provider
