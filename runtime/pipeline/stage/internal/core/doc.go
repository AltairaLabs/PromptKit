// Package core holds what every part of runtime/pipeline/stage shares: the
// StreamElement that flows between stages and its metadata, the Stage
// interface and its basic implementations, the per-turn TurnState, format
// capabilities, the package's errors, the idle timeout, and the small flow
// helpers stages use to forward, cancel and tag elements.
//
// It is internal to stage (#2876): stage re-exports what was its public API
// before the move under the same names (type aliases, wrapper funcs, sentinel
// vars and consts), so callers keep importing stage. The other exported names
// here are for stage and its internal packages only.
package core
