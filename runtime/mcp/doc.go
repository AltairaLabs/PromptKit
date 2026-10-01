// Package mcp is PromptKit's Model Context Protocol client.
//
// It speaks both generations of the protocol: the stateless revision
// (2026-07-28), where every request carries its version and the client's
// capabilities, and the handshake revisions up to 2025-11-25, detecting
// which a server speaks. Transports are stdio, Streamable HTTP and the
// deprecated HTTP+SSE. Authorization and user interaction (elicitation) are
// delegated to the host through Authorizer and ElicitationHandler.
//
// Conformance with the claimed revision (ProtocolVersion) is checked by
// spec_parity_test.go against a mirror of the published schema, and by the
// official conformance suite (make mcp-conformance).
package mcp
