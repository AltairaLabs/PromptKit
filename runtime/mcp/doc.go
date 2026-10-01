// Package mcp is PromptKit's Model Context Protocol client.
//
// It is built on the official Go SDK (github.com/modelcontextprotocol/go-sdk),
// which speaks both generations of the protocol: the stateless revision
// (2026-07-28), where every request carries its version and the client's
// capabilities, and the handshake revisions up to 2025-11-25, detecting
// which a server speaks. Transports are stdio, Streamable HTTP and the
// deprecated HTTP+SSE. This package adapts the SDK to PromptKit's types and
// adds what the SDK leaves to its callers: retries, reconnection, timeouts
// that pause while a user answers, and the delegation of authorization and
// user interaction (elicitation) to the host through Authorizer and
// ElicitationHandler.
//
// The types results arrive in are checked against the claimed revisions'
// published schemas by spec_parity_test.go, and behavior by the official
// conformance suite (make mcp-conformance).
package mcp
