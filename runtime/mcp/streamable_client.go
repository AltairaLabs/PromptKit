package mcp

import "context"

// StreamableClient is the Streamable HTTP transport implementation of the
// Client interface. The protocol lives in session; wire-level details in
// streamable_transport.go; the lifecycle in httpClient.
type StreamableClient struct {
	httpClient
}

// NewStreamableClient creates an MCP client using the Streamable HTTP transport.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewStreamableClient(config ServerConfig) *StreamableClient {
	return NewStreamableClientWithOptions(config, DefaultClientOptions())
}

// NewStreamableClientWithOptions creates a Streamable HTTP client with custom options.
//
//nolint:gocritic // matches existing Client constructor signatures
func NewStreamableClientWithOptions(config ServerConfig, options ClientOptions) *StreamableClient {
	return &StreamableClient{httpClient{
		config:  config,
		options: options,
		newConn: func(sess *session) httpConn { return newStreamableTransport(config, options, sess) },
		establish: func(ctx context.Context, sess *session, _ httpConn) (*InitializeResponse, error) {
			return sess.connect(ctx)
		},
	}}
}
