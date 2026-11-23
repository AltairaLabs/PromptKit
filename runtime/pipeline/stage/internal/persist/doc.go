// Package persist holds the stages that keep what a turn produced: the
// recording stage (to an EventStore), the incremental save stage (to the state
// store), the memory retrieval and extraction stages, and the message broadcast
// stage (message.created on the EventBus).
//
// It is internal to stage (#2876): stage re-exports these stages, their
// configs and IsTranscriptMessage under the same names, so callers keep
// importing stage.
package persist
