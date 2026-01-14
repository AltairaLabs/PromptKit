package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// ClientOptions configures MCP client behavior
type ClientOptions struct {
	// RequestTimeout is the default timeout for RPC requests
	RequestTimeout time.Duration
	// InitTimeout is the timeout for the initialization handshake
	InitTimeout time.Duration
	// MaxRetries is the number of times to retry failed requests
	MaxRetries int
	// RetryDelay is the initial delay between retries (exponential backoff)
	RetryDelay time.Duration
	// EnableGracefulDegradation allows operations to continue even if MCP is unavailable
	EnableGracefulDegradation bool
}

// DefaultClientOptions returns sensible defaults
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		RequestTimeout:            30 * time.Second,
		InitTimeout:               10 * time.Second,
		MaxRetries:                3,
		RetryDelay:                100 * time.Millisecond,
		EnableGracefulDegradation: true,
	}
}

var (
	// ErrClientNotInitialized is returned when attempting operations on uninitialized client
	ErrClientNotInitialized = errors.New("mcp: client not initialized")
	// ErrClientClosed is returned when attempting operations on closed client
	ErrClientClosed = errors.New("mcp: client closed")
	// ErrServerUnresponsive is returned when server doesn't respond
	ErrServerUnresponsive = errors.New("mcp: server unresponsive")
	// ErrProcessDied is returned when server process dies unexpectedly
	ErrProcessDied = errors.New("mcp: server process died")
)

// StdioClient implements the MCP Client interface using stdio transport
type StdioClient struct {
	config  ServerConfig
	options ClientOptions
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser

	// JSON-RPC state
	nextID      atomic.Int64
	pendingReqs sync.Map // map[int64]chan *JSONRPCMessage

	// Lifecycle
	mu         sync.RWMutex
	started    bool
	closed     bool
	serverInfo *InitializeResponse

	// Health monitoring
	lastActivity atomic.Int64 // Unix timestamp of last successful RPC

	// Background goroutine management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewStdioClient creates a new MCP client using stdio transport
func NewStdioClient(config ServerConfig) *StdioClient {
	return NewStdioClientWithOptions(config, DefaultClientOptions())
}

// NewStdioClientWithOptions creates a client with custom options
func NewStdioClientWithOptions(config ServerConfig, options ClientOptions) *StdioClient {
	ctx, cancel := context.WithCancel(context.Background())
	return &StdioClient{
		config:  config,
		options: options,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Initialize establishes the MCP connection and negotiates capabilities
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
	c.mu.Lock()

	if c.started {
		c.mu.Unlock()
		return c.serverInfo, nil
	}

	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}

	// Start the server process with retries
	var startErr error
	for attempt := 0; attempt <= c.options.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := c.options.RetryDelay * time.Duration(1<<uint(attempt-1)) // Exponential backoff
			time.Sleep(delay)
		}

		startErr = c.startProcess()
		if startErr == nil {
			break
		}

		if attempt < c.options.MaxRetries {
			logger.Warn("MCP failed to start process, retrying",
				"server", c.config.Name, "attempt", attempt+1, "maxAttempts", c.options.MaxRetries+1, "error", startErr)
		}
	}

	if startErr != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to start server process after %d attempts: %w",
			c.options.MaxRetries+1, startErr)
	}

	// Start background reader
	c.wg.Add(1)
	go c.readLoop()

	// Mark as started before releasing lock
	c.started = true
	c.mu.Unlock()

	// Send initialize request with timeout
	initCtx, cancel := context.WithTimeout(ctx, c.options.InitTimeout)
	defer cancel()

	req := InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ClientCapabilities{
			Elicitation: &ElicitationCapability{},
		},
		ClientInfo: Implementation{
			Name:    "promptkit",
			Version: "0.1.0",
		},
	}

	var resp InitializeResponse
	if err := c.sendRequestWithRetry(initCtx, "initialize", req, &resp); err != nil {
		c.Close()
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("initialization timeout after %v: %w", c.options.InitTimeout, err)
		}
		return nil, fmt.Errorf("initialize request failed: %w", err)
	}

	// Send initialized notification
	if err := c.sendNotification("notifications/initialized", nil); err != nil {
		// Non-fatal: log but continue
		logger.Warn("MCP initialized notification failed, continuing", "server", c.config.Name, "error", err)
	}

	// Store server info and update activity timestamp
	c.mu.Lock()
	c.serverInfo = &resp
	c.mu.Unlock()
	c.updateActivity()

	return &resp, nil
}

// ListTools retrieves all available tools from the server
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.checkHealth(); err != nil {
		return nil, err
	}

	var resp ToolsListResponse
	if err := c.sendRequestWithRetry(ctx, "tools/list", nil, &resp); err != nil {
		if c.options.EnableGracefulDegradation {
			logger.Warn("MCP tools/list failed, using graceful degradation", "server", c.config.Name, "error", err)
			return []Tool{}, nil // Return empty list instead of error
		}
		return nil, fmt.Errorf("tools/list request failed: %w", err)
	}

	c.updateActivity()
	return resp.Tools, nil
}

// CallTool executes a tool with the given arguments
func (c *StdioClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	if err := c.checkHealth(); err != nil {
		return nil, err
	}

	req := ToolCallRequest{
		Name:      name,
		Arguments: arguments,
	}

	var resp ToolCallResponse
	if err := c.sendRequestWithRetry(ctx, "tools/call", req, &resp); err != nil {
		return nil, fmt.Errorf("tools/call request failed: %w", err)
	}

	c.updateActivity()
	return &resp, nil
}

// Close terminates the connection to the MCP server
func (c *StdioClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	// Cancel context to stop background goroutines
	c.cancel()

	// Close pipes
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.stderr != nil {
		c.stderr.Close()
	}

	// Kill the process
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait() // Clean up zombie process
	}

	// Wait for background goroutines
	c.wg.Wait()

	return nil
}

// IsAlive checks if the connection is still active
func (c *StdioClient) IsAlive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started && !c.closed && c.cmd != nil && c.cmd.Process != nil
}

// startProcess launches the MCP server process
func (c *StdioClient) startProcess() error {
	c.cmd = exec.CommandContext(c.ctx, c.config.Command, c.config.Args...)

	// Set environment variables
	c.cmd.Env = append(c.cmd.Env, "PATH="+"/usr/local/bin:/usr/bin:/bin")
	for k, v := range c.config.Env {
		c.cmd.Env = append(c.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var err error

	// Setup stdin
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Setup stdout
	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Setup stderr (for logging)
	c.stderr, err = c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Start stderr logger
	c.wg.Add(1)
	go c.logStderr()

	return nil
}

// checkHealth verifies the client is in a healthy state
func (c *StdioClient) checkHealth() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return ErrClientClosed
	}

	if !c.started {
		return ErrClientNotInitialized
	}

	// Check if process is still alive
	if c.cmd == nil || c.cmd.Process == nil {
		return ErrProcessDied
	}

	return nil
}

// updateActivity records the timestamp of the last successful operation
func (c *StdioClient) updateActivity() {
	c.lastActivity.Store(time.Now().Unix())
}

// sendRequestWithRetry sends a request with automatic retry on failure
func (c *StdioClient) sendRequestWithRetry(ctx context.Context, method string, params, result interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.options.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := c.options.RetryDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := c.sendRequest(ctx, method, params, result)
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		// Log retry attempt
		if attempt < c.options.MaxRetries {
			logger.Warn("MCP request failed, retrying",
				"server", c.config.Name, "method", method, "attempt", attempt+1, "maxAttempts", c.options.MaxRetries+1, "error", err)
		}
	}

	return fmt.Errorf("request failed after %d attempts: %w", c.options.MaxRetries+1, lastErr)
}

// sendRequest sends a JSON-RPC request and waits for the response
func (c *StdioClient) sendRequest(ctx context.Context, method string, params, result interface{}) error {
	id := c.nextID.Add(1)

	// Marshal params
	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}

	// Create response channel
	respChan := make(chan *JSONRPCMessage, 1)
	c.pendingReqs.Store(id, respChan)
	defer c.pendingReqs.Delete(id)

	// Send the request
	if err := c.writeMessage(&msg); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}

	// Wait for response with configurable timeout
	timeout := c.options.RequestTimeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return fmt.Errorf("%w: request timeout after %v", ErrServerUnresponsive, timeout)
	case resp := <-respChan:
		if resp.Error != nil {
			return fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("failed to unmarshal result: %w", err)
			}
		}
		return nil
	}
}

// sendNotification sends a JSON-RPC notification (no response expected)
func (c *StdioClient) sendNotification(method string, params interface{}) error {
	var paramsJSON json.RawMessage
	if params != nil {
		var err error
		paramsJSON, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params: %w", err)
		}
	}

	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsJSON,
	}

	return c.writeMessage(&msg)
}

// writeMessage writes a JSON-RPC message to stdin
func (c *StdioClient) writeMessage(msg *JSONRPCMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// MCP uses newline-delimited JSON over stdio
	data = append(data, '\n')

	c.mu.RLock()
	stdin := c.stdin
	c.mu.RUnlock()

	if stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	if _, err := stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// readLoop continuously reads messages from stdout
func (c *StdioClient) readLoop() {
	defer c.wg.Done()

	scanner := bufio.NewScanner(c.stdout)
	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max

	for scanner.Scan() {
		line := scanner.Bytes()

		var msg JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Log error but continue reading
			logger.Error("MCP failed to unmarshal message", "error", err)
			continue
		}

		c.handleMessage(&msg)
	}

	if err := scanner.Err(); err != nil && !c.closed {
		logger.Error("MCP scanner error", "error", err)
	}
}

// handleMessage processes incoming JSON-RPC messages
func (c *StdioClient) handleMessage(msg *JSONRPCMessage) {
	// If it's a response (has ID and no method), route to pending request
	if msg.ID != nil && msg.Method == "" {
		id, ok := msg.ID.(float64) // JSON numbers are float64
		if !ok {
			logger.Warn("MCP invalid response ID type", "type", fmt.Sprintf("%T", msg.ID))
			return
		}

		if ch, ok := c.pendingReqs.Load(int64(id)); ok {
			respChan := ch.(chan *JSONRPCMessage)
			select {
			case respChan <- msg:
			default:
				// Channel full or closed
			}
		}
		return
	}

	// If it's a notification (no ID), handle it
	if msg.ID == nil && msg.Method != "" {
		c.handleNotification(msg)
		return
	}

	// If it's a request from server (has ID and method), we'd handle it here
	// For now, we don't expect servers to call client methods
}

// handleNotification processes server notifications
func (c *StdioClient) handleNotification(msg *JSONRPCMessage) {
	// Handle notifications like "notifications/tools/list_changed"
	switch msg.Method {
	case "notifications/tools/list_changed":
		// Tool list changed - could trigger a refresh
		logger.Info("MCP tools list changed", "server", c.config.Name)
	case "notifications/resources/list_changed":
		logger.Info("MCP resources list changed", "server", c.config.Name)
	default:
		// Unknown notification
		logger.Debug("MCP received unknown notification", "method", msg.Method)
	}
}

// logStderr logs stderr output from the MCP server
func (c *StdioClient) logStderr() {
	defer c.wg.Done()

	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		logger.Debug("MCP server stderr", "server", c.config.Name, "output", line)
	}
}
