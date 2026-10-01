package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// SSEClient is the HTTP+SSE transport implementation of the Client interface.
// The protocol lives in session; wire-level details (endpoint discovery,
// request correlation) in sse_transport.go; this file owns the public
// lifecycle.
type SSEClient struct {
	config  ServerConfig
	options ClientOptions

	tr   *sseTransport
	sess *session

	mu      sync.Mutex
	started bool
	closed  bool
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
	return &SSEClient{config: config, options: options}
}

// Initialize establishes the SSE connection and negotiates capabilities.
func (c *SSEClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
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
	c.tr = newSSETransport(c.config, c.options, c.sess)
	c.sess.conn = c.tr
	sess, tr := c.sess, c.tr
	c.mu.Unlock()

	connectCtx, cancel := context.WithTimeout(ctx, c.options.InitTimeout)
	defer cancel()
	if err := tr.connect(connectCtx); err != nil {
		return nil, fmt.Errorf("mcp/sse: connect: %w", err)
	}

	resp, err := sess.initialize(ctx)
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
func (c *SSEClient) ListTools(ctx context.Context) ([]Tool, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.listToolsDegrading(ctx)
}

// CallTool executes a tool with the given arguments.
func (c *SSEClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.callTool(ctx, name, arguments)
}

// Close terminates the SSE connection. Idempotent.
func (c *SSEClient) Close() error {
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

// IsAlive reports whether the SSE stream is currently open.
func (c *SSEClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.tr == nil {
		return false
	}
	return c.tr.alive.Load()
}

// ready returns the session once the client is initialized and its stream open.
func (c *SSEClient) ready() (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if !c.started {
		return nil, ErrClientNotInitialized
	}
	if !c.tr.alive.Load() {
		return nil, ErrServerUnresponsive
	}
	return c.sess, nil
}
