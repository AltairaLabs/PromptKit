package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

// errSSENotImplemented is returned by SSEClient methods whose real
// implementations have not yet landed. Each task in the SSE series
// replaces one of these stubs.
var errSSENotImplemented = errors.New("mcp: SSEClient method not implemented yet")

// SSEClient is the HTTP+SSE transport implementation of the Client interface.
// Wire-level details (endpoint discovery, request correlation) live in
// sse_transport.go; this file owns the public lifecycle.
type SSEClient struct {
	config  ServerConfig
	options ClientOptions
}

// NewSSEClient creates a new MCP client using HTTP+SSE transport.
// Config is passed by value to match the existing Client constructors;
// callers usually hand it to the registry which stores a copy.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewSSEClient(config ServerConfig) *SSEClient {
	return NewSSEClientWithOptions(config, DefaultClientOptions())
}

// NewSSEClientWithOptions creates an SSE client with custom options.
// config is taken by value to match NewStdioClientWithOptions.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewSSEClientWithOptions(config ServerConfig, options ClientOptions) *SSEClient {
	return &SSEClient{config: config, options: options}
}

// Initialize is implemented in a follow-up task.
func (c *SSEClient) Initialize(_ context.Context) (*InitializeResponse, error) {
	return nil, errSSENotImplemented
}

// ListTools is implemented in a follow-up task.
func (c *SSEClient) ListTools(_ context.Context) ([]Tool, error) {
	return nil, errSSENotImplemented
}

// CallTool is implemented in a follow-up task.
func (c *SSEClient) CallTool(_ context.Context, _ string, _ json.RawMessage) (*ToolCallResponse, error) {
	return nil, errSSENotImplemented
}

// Close is implemented in a follow-up task.
func (c *SSEClient) Close() error { return nil }

// IsAlive is implemented in a follow-up task.
func (c *SSEClient) IsAlive() bool { return false }
