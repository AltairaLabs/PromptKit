// Package duplex holds the realtime (duplex) provider stage: the stage that
// streams a conversation through a provider's bidirectional session, with its
// tool rounds, guardrails, cost accounting and reply holds, and the stages
// that follow it in the realtime chain and depend on its elements: the cue
// stage it wires (NewDuplexCueStage), transcript reordering and the playout
// hold.
//
// It is internal to stage (#2876): stage re-exports DuplexProviderStage, its
// constructors and tool config, EndInputter, TextInputTurnSource,
// NewDuplexCueStage, TranscriptReorderStage and PlayoutHoldStage under the
// same names, so callers keep importing stage. stage's NewRealtimeChain, which
// orders these stages, stays in stage.
package duplex
