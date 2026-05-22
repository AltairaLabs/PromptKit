package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// streamableClientImplName / streamableClientImplVersion identify this
// implementation in the MCP initialize handshake's ClientInfo.
const (
	streamableClientImplName    = "promptkit"
	streamableClientImplVersion = "0.1.0"
)

// StreamableClient is the MCP 2025-03-26 Streamable HTTP transport
// implementation of the Client interface. Wire-level details live in
// streamable_transport.go; this file owns the public lifecycle.
type StreamableClient struct {
	config  ServerConfig
	options ClientOptions

	tr         *streamableTransport
	serverInfo *InitializeResponse

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
		resp := c.serverInfo
		c.mu.Unlock()
		return resp, nil
	}
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}
	c.tr = newStreamableTransport(c.config, c.options)
	c.mu.Unlock()

	initCtx, cancel := context.WithTimeout(ctx, c.options.InitTimeout)
	defer cancel()
	req := InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ClientCapabilities{
			Elicitation: &ElicitationCapability{},
		},
		ClientInfo: Implementation{Name: streamableClientImplName, Version: streamableClientImplVersion},
	}
	var resp InitializeResponse
	if err := c.tr.sendRequest(initCtx, "initialize", req, &resp); err != nil {
		c.tr.close()
		return nil, fmt.Errorf("mcp/streamable: initialize: %w", err)
	}

	c.mu.Lock()
	c.serverInfo = &resp
	c.started = true
	c.mu.Unlock()
	return &resp, nil
}

// ListTools retrieves all available tools from the server.
func (c *StreamableClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.checkAlive(); err != nil {
		return nil, err
	}
	var resp ToolsListResponse
	if err := c.tr.sendRequest(ctx, "tools/list", nil, &resp); err != nil {
		if c.options.EnableGracefulDegradation {
			logger.Warn("MCP/Streamable tools/list failed, using graceful degradation",
				"server", c.config.Name, "error", err)
			return []Tool{}, nil
		}
		return nil, fmt.Errorf("mcp/streamable: tools/list: %w", err)
	}
	return resp.Tools, nil
}

// CallTool executes a tool with the given arguments.
func (c *StreamableClient) CallTool(
	ctx context.Context, name string, arguments json.RawMessage,
) (*ToolCallResponse, error) {
	if err := c.checkAlive(); err != nil {
		return nil, err
	}
	req := ToolCallRequest{Name: name, Arguments: arguments}
	var resp ToolCallResponse
	if err := c.tr.sendRequest(ctx, "tools/call", req, &resp); err != nil {
		return nil, fmt.Errorf("mcp/streamable: tools/call: %w", err)
	}
	return &resp, nil
}

// Close marks the client closed. Idempotent.
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
		tr.close()
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

func (c *StreamableClient) checkAlive() error {
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
