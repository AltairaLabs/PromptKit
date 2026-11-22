// Package compaction holds stage's context compaction and relevance helpers:
// the CompactionStrategy the provider stage calls between tool-loop rounds,
// the default ContextCompactor and its rules, the cosine-similarity scoring
// relevance truncation uses, and the embedding cache that keeps one call's
// vectors for the next.
//
// It is internal to stage (#2876): stage re-exports what was its public API
// before the move under the same names (type aliases, wrapper funcs and
// consts), so callers keep importing stage. The other exported names here are
// for stage and its internal packages only.
package compaction
