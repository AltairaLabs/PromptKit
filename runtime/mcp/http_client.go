package mcp

import (
	"context"
	"encoding/json"
	"sync"
)

// httpConn is a transport an httpClient drives: Streamable HTTP or HTTP+SSE.
type httpConn interface {
	conn
	isAlive() bool
}

// httpClient owns the public lifecycle shared by the HTTP clients: one
// session over one transport, created on Initialize and ended on Close. The
// protocol lives in session and the wire details in each transport.
type httpClient struct {
	config  ServerConfig
	options ClientOptions

	// newConn builds the transport for a session.
	newConn func(sess *session) httpConn
	// establish brings a new session up over its transport.
	establish func(ctx context.Context, sess *session, tr httpConn) (*InitializeResponse, error)
	// aliveForCalls refuses calls once the transport reports itself dead
	// (HTTP+SSE: the stream every response arrives on has closed).
	aliveForCalls bool

	tr   httpConn
	sess *session

	mu      sync.Mutex
	started bool
	closed  bool
}

// Initialize establishes the connection and negotiates the protocol.
func (c *httpClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
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
	c.tr = c.newConn(c.sess)
	c.sess.conn = c.tr
	sess, tr := c.sess, c.tr
	c.mu.Unlock()

	resp, err := c.establish(ctx, sess, tr)
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
func (c *httpClient) ListTools(ctx context.Context) ([]Tool, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.listToolsDegrading(ctx)
}

// CallTool executes a tool with the given arguments.
func (c *httpClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	sess, err := c.ready()
	if err != nil {
		return nil, err
	}
	return sess.callTool(ctx, name, arguments)
}

// Close ends the client and its transport (and any server session).
// Idempotent.
func (c *httpClient) Close() error {
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

// IsAlive reports whether the transport is usable.
func (c *httpClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.tr == nil {
		return false
	}
	return c.tr.isAlive()
}

// ready returns the session once the client is initialized and open.
func (c *httpClient) ready() (*session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if !c.started {
		return nil, ErrClientNotInitialized
	}
	if c.aliveForCalls && !c.tr.isAlive() {
		return nil, ErrServerUnresponsive
	}
	return c.sess, nil
}
