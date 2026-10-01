package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// httpAutoClient serves a server configured by URL alone. It speaks
// Streamable HTTP, and falls back to the deprecated HTTP+SSE transport only
// when the server does not host a Streamable HTTP endpoint: the initialize
// POST is refused with 400, 404 or 405 (basic/transports, "Backwards
// Compatibility": a client accepting a URL that may point at either
// transport tries the POST first).
type httpAutoClient struct {
	config  ServerConfig
	options ClientOptions

	mu     sync.Mutex
	active Client
	closed bool
}

func newHTTPAutoClient(config *ServerConfig, options *ClientOptions) *httpAutoClient {
	return &httpAutoClient{config: *config, options: *options}
}

// Initialize connects over Streamable HTTP, or HTTP+SSE if the server only
// hosts that.
func (c *httpAutoClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}
	if c.active != nil {
		active := c.active
		c.mu.Unlock()
		return active.Initialize(ctx)
	}
	c.mu.Unlock()

	streamable := NewStreamableClientWithOptions(c.config, c.options)
	resp, err := streamable.Initialize(ctx)
	if err == nil {
		return resp, c.adopt(streamable)
	}
	if !isStreamableRefusal(err) {
		return nil, err
	}
	logger.Info("MCP server has no Streamable HTTP endpoint, using the deprecated HTTP+SSE transport",
		"server", c.config.Name, "error", err)
	sse := NewSSEClientWithOptions(c.config, c.options)
	resp, sseErr := sse.Initialize(ctx)
	if sseErr != nil {
		return nil, fmt.Errorf("mcp: Streamable HTTP refused (%w); HTTP+SSE failed: %w", err, sseErr)
	}
	return resp, c.adopt(sse)
}

// adopt makes client the active one, unless Close ran meanwhile.
func (c *httpAutoClient) adopt(client Client) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		_ = client.Close()
		return ErrClientClosed
	}
	c.active = client
	return nil
}

// isStreamableRefusal reports whether a Streamable HTTP initialize failed
// because the endpoint does not speak it.
func isStreamableRefusal(err error) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	switch statusErr.status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	}
	return false
}

func (c *httpAutoClient) client() (Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrClientClosed
	}
	if c.active == nil {
		return nil, ErrClientNotInitialized
	}
	return c.active, nil
}

// ListTools retrieves all available tools from the server.
func (c *httpAutoClient) ListTools(ctx context.Context) ([]Tool, error) {
	client, err := c.client()
	if err != nil {
		return nil, err
	}
	return client.ListTools(ctx)
}

// CallTool executes a tool with the given arguments.
func (c *httpAutoClient) CallTool(
	ctx context.Context, name string, arguments json.RawMessage,
) (*ToolCallResponse, error) {
	client, err := c.client()
	if err != nil {
		return nil, err
	}
	return client.CallTool(ctx, name, arguments)
}

// Close closes the active client. Idempotent.
func (c *httpAutoClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	active := c.active
	c.mu.Unlock()
	if active != nil {
		return active.Close()
	}
	return nil
}

// IsAlive reports whether the active client is alive.
func (c *httpAutoClient) IsAlive() bool {
	client, err := c.client()
	return err == nil && client.IsAlive()
}
