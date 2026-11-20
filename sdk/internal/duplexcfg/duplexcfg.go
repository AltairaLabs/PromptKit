// Package duplexcfg carries the duplex session settings package sdk sets on a
// session.DuplexSessionConfig that are not part of the public SDK API.
//
// Package session registers the setter in its init, so package sdk can set
// the unexported field without it being exported on the config.
package duplexcfg

// runToolsInPipeline is the setter package session registers.
var runToolsInPipeline func(cfg any)

// Register sets the function RunToolsInPipeline calls. Package session calls
// it in its init.
func Register(f func(cfg any)) { runToolsInPipeline = f }

// RunToolsInPipeline marks cfg, a *session.DuplexSessionConfig, as one whose
// pipeline runs the session's tool calls, so the session never runs them
// itself.
func RunToolsInPipeline(cfg any) { runToolsInPipeline(cfg) }
