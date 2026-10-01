package mcp

import (
	"context"
	"encoding/json"
	"sync"
)

// StreamableClient is the Streamable HTTP transport implementation of the
// Client interface. The protocol lives in session; wire-level details in
// streamable_transport.go; this file owns the public lifecycle.
type StreamableClient struct {
	config  ServerConfig
	options ClientOptions

	tr   *streamableTransport
	sess *session

	mu      sync.Mutex
	started bool
	closed  bool
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
	return &StreamableClient{config: config, options: options}
}

// Initialize sends the initialize request and negotiates capabilities.
func (c *StreamableClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
	c.mu.Lock()
	if c.started {
		info := c.sess.info()
		c.mu.Unlock()
		return info, nil
	}
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}
	c.sess = newSession(c.config.Name, c.options)
	c.tr = newStreamableTransport(c.config, c.options, c.sess)
	c.sess.conn = c.tr
	sess, tr := c.sess, c.tr
	c.mu.Unlock()

	resp, err := sess.connect(ctx)
	if err != nil {
		_ = tr.close()
		return nil, err
	}

	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return resp, nil
}

// ListTools retrieves all available tools from the server.
func (c *StreamableClient) ListTools(ctx context.Context) ([]Tool, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.listToolsDegrading(ctx)
}

// CallTool executes a tool with the given arguments.
func (c *StreamableClient) CallTool(
	ctx context.Context, name string, arguments json.RawMessage,
) (*ToolCallResponse, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.callTool(ctx, name, arguments)
}

// Close ends the client and, if the server assigned one, its session. Idempotent.
func (c *StreamableClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	tr := c.tr
	c.mu.Unlock()
	if tr != nil {
		return tr.close()
	}
	return nil
}

// IsAlive reports whether the transport has completed at least one
// successful request since the last close.
func (c *StreamableClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.tr == nil {
		return false
	}
	return c.tr.alive.Load()
}

// ready returns the session once the client is initialized and open.
func (c *StreamableClient) ready() (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if !c.started {
		return nil, ErrClientNotInitialized
	}
	return c.sess, nil
}
