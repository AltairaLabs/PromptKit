// Package heard holds HeardReplies, which carries what the caller heard of a
// reply a barge-in cut from a cascaded voice pipeline's speech-out stage back
// to its provider stage.
//
// It is internal to stage (#2876): stage re-exports HeardReplies and
// NewHeardReplies under the same names, so callers keep importing stage. The
// package functions here (Record, Expect, Settle, Await, Apply) are for stage
// only; they were HeardReplies' unexported methods.
package heard
