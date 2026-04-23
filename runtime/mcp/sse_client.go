package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// SSEClient is the HTTP+SSE transport implementation of the Client interface.
// Wire-level details (endpoint discovery, request correlation) live in
// sse_transport.go; this file owns the public lifecycle.
type SSEClient struct {
	config  ServerConfig
	options ClientOptions

	tr         *sseTransport
	serverInfo *InitializeResponse

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
		resp := c.serverInfo
		c.mu.Unlock()
		return resp, nil
	}
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}
	c.tr = newSSETransport(c.config, c.options)
	c.mu.Unlock()

	connectCtx, cancel := context.WithTimeout(ctx, c.options.InitTimeout)
	defer cancel()
	if err := c.tr.connect(connectCtx); err != nil {
		return nil, fmt.Errorf("mcp/sse: connect: %w", err)
	}
	c.tr.startReadLoop()

	req := InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ClientCapabilities{
			Elicitation: &ElicitationCapability{},
		},
		ClientInfo: Implementation{Name: "promptkit", Version: "0.1.0"},
	}
	var resp InitializeResponse
	if err := c.tr.sendRequest(connectCtx, "initialize", req, &resp); err != nil {
		c.tr.close()
		return nil, fmt.Errorf("mcp/sse: initialize: %w", err)
	}

	c.mu.Lock()
	c.serverInfo = &resp
	c.started = true
	c.mu.Unlock()
	return &resp, nil
}

// ListTools retrieves all available tools from the server.
func (c *SSEClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.checkAlive(); err != nil {
		return nil, err
	}
	var resp ToolsListResponse
	if err := c.tr.sendRequest(ctx, "tools/list", nil, &resp); err != nil {
		if c.options.EnableGracefulDegradation {
			logger.Warn("MCP/SSE tools/list failed, using graceful degradation",
				"server", c.config.Name, "error", err)
			return []Tool{}, nil
		}
		return nil, fmt.Errorf("mcp/sse: tools/list: %w", err)
	}
	return resp.Tools, nil
}

// CallTool executes a tool with the given arguments.
func (c *SSEClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	if err := c.checkAlive(); err != nil {
		return nil, err
	}
	req := ToolCallRequest{Name: name, Arguments: arguments}
	var resp ToolCallResponse
	if err := c.tr.sendRequest(ctx, "tools/call", req, &resp); err != nil {
		return nil, fmt.Errorf("mcp/sse: tools/call: %w", err)
	}
	return &resp, nil
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
		tr.close()
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

func (c *SSEClient) checkAlive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClientClosed
	}
	if !c.started {
		return ErrClientNotInitialized
	}
	if !c.tr.alive.Load() {
		return ErrServerUnresponsive
	}
	return nil
}
