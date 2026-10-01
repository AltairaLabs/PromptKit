package mcp

import (
	"context"
	"fmt"
)

// SSEClient is the HTTP+SSE transport implementation of the Client interface.
// The protocol lives in session; wire-level details (endpoint discovery,
// request correlation) in sse_transport.go; the lifecycle in httpClient.
type SSEClient struct {
	httpClient
}

// NewSSEClient creates a new MCP client using HTTP+SSE transport.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewSSEClient(config ServerConfig) *SSEClient {
	return NewSSEClientWithOptions(config, DefaultClientOptions())
}

// NewSSEClientWithOptions creates an SSE client with custom options.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewSSEClientWithOptions(config ServerConfig, options ClientOptions) *SSEClient {
	return &SSEClient{httpClient{
		config:  config,
		options: options,
		newConn: func(sess *session) httpConn { return newSSETransport(config, options, sess) },
		establish: func(ctx context.Context, sess *session, tr httpConn) (*InitializeResponse, error) {
			// HTTP+SSE predates the stateless protocol: open the stream, then
			// handshake.
			connectCtx, cancel := context.WithTimeout(ctx, options.InitTimeout)
			defer cancel()
			if err := tr.(*sseTransport).connect(connectCtx); err != nil {
				return nil, fmt.Errorf("mcp/sse: connect: %w", err)
			}
			return sess.initialize(ctx)
		},
		aliveForCalls: true,
	}}
}
