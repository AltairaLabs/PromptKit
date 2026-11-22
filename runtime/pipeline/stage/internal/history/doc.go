// Package history holds what stage does to a conversation's history before a
// model sees it: it recognizes the turns an input guardrail blocked (the
// user's message and the canned reply that answered it) and leaves them out of
// every path that builds model context, and it renders a provider's
// message-metadata template into the request's copy of each message.
//
// It is internal to stage (#2876): stage re-exports what was its public API
// before the move under the same names (wrapper funcs), so callers keep
// importing stage. The other exported names here are for stage and its
// internal packages only.
package history
