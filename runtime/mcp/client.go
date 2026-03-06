package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	// MaxReconnectAttempts is the maximum number of times to attempt reconnection
	// when a process death is detected. 0 disables auto-reconnection.
	MaxReconnectAttempts int
}

// DefaultClientOptions returns sensible defaults
func DefaultClientOptions() ClientOptions {
	return ClientOptions{
		RequestTimeout:            30 * time.Second,
		InitTimeout:               10 * time.Second,
		MaxRetries:                3,
		RetryDelay:                100 * time.Millisecond,
		EnableGracefulDegradation: true,
		MaxReconnectAttempts:      defaultMaxReconnectAttempts,
	}
}

const (
	// defaultMaxReconnectAttempts is the default number of reconnection attempts.
	defaultMaxReconnectAttempts = 3
	// reconnectPollInterval is the polling interval when waiting for a concurrent reconnection.
	reconnectPollInterval = 100 * time.Millisecond
	// reconnectPollMaxIterations is the max iterations to poll for concurrent reconnection.
	reconnectPollMaxIterations = 50
)

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

	// Reconnection state
	reconnecting  bool
	reconnectDone chan struct{} // closed when reconnection completes

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

	// Start the server process with retries (caller must hold c.mu; released on error)
	if err := c.startProcessWithRetry(ctx); err != nil {
		return nil, err
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

// startProcessWithRetry attempts to start the server process with exponential backoff.
// The caller must hold c.mu.Lock(). On error, the mutex is released before returning.
// On success, the mutex remains held.
func (c *StdioClient) startProcessWithRetry(ctx context.Context) error {
	var startErr error
	for attempt := 0; attempt <= c.options.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := c.options.RetryDelay * time.Duration(1<<uint(attempt-1)) //nolint:gosec // bounded by MaxRetries
			// Release the mutex during sleep so other operations are not blocked
			c.mu.Unlock()
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
			c.mu.Lock()
			// Re-check state after re-acquiring the lock
			if c.closed {
				c.mu.Unlock()
				return ErrClientClosed
			}
		}

		startErr = c.startProcess()
		if startErr == nil {
			return nil
		}

		if attempt < c.options.MaxRetries {
			logger.Warn("MCP failed to start process, retrying",
				"server", c.config.Name, "attempt", attempt+1,
				"maxAttempts", c.options.MaxRetries+1, "error", startErr)
		}
	}

	c.mu.Unlock()
	return fmt.Errorf("failed to start server process after %d attempts: %w",
		c.options.MaxRetries+1, startErr)
}

// startProcess launches the MCP server process
func (c *StdioClient) startProcess() error {
	c.cmd = exec.CommandContext(c.ctx, c.config.Command, c.config.Args...)

	// Inherit the parent process environment and prepend common paths to PATH.
	c.cmd.Env = os.Environ()
	existingPath := os.Getenv("PATH")
	c.cmd.Env = append(c.cmd.Env, "PATH=/usr/local/bin:/usr/bin:/bin:"+existingPath)
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

// checkHealth verifies the client is in a healthy state.
// If the process has died and auto-reconnection is configured, it attempts to reconnect.
func (c *StdioClient) checkHealth() error {
	c.mu.RLock()

	if c.closed {
		c.mu.RUnlock()
		return ErrClientClosed
	}

	if !c.started {
		c.mu.RUnlock()
		return ErrClientNotInitialized
	}

	// Check if process is still alive
	if c.cmd != nil && c.cmd.Process != nil {
		c.mu.RUnlock()
		return nil
	}

	// Process is dead — attempt reconnection if configured
	reconnectAttempts := c.options.MaxReconnectAttempts
	c.mu.RUnlock()

	if reconnectAttempts <= 0 {
		return ErrProcessDied
	}

	return c.reconnect()
}

// reconnect attempts to restart the MCP server process and re-initialize the connection.
// It fails all pending requests on the dead client before attempting to restart.
func (c *StdioClient) reconnect() error {
	c.mu.Lock()

	// Another goroutine may have already reconnected
	if c.cmd != nil && c.cmd.Process != nil {
		c.mu.Unlock()
		return nil
	}

	// If already reconnecting, wait and re-check
	if c.reconnecting {
		doneCh := c.reconnectDone
		c.mu.Unlock()
		return c.waitForReconnect(doneCh)
	}

	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}

	c.reconnecting = true
	c.reconnectDone = make(chan struct{})
	reconnectDone := c.reconnectDone
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.reconnecting = false
		c.mu.Unlock()
		close(reconnectDone)
	}()

	// Fail all pending requests so callers don't hang until context timeout
	c.failPendingRequests()

	// Clean up old process resources
	c.cleanupDeadProcess()

	// Attempt to restart with retries
	logger.Warn("MCP process died, attempting reconnection", "server", c.config.Name)

	// Use a background-derived context so we're not affected by the old process's canceled context
	totalTimeout := c.options.InitTimeout * time.Duration(c.options.MaxReconnectAttempts+1)
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < c.options.MaxReconnectAttempts; attempt++ {
		if attempt > 0 {
			delay := c.options.RetryDelay * time.Duration(1<<uint(attempt-1)) //nolint:gosec // bounded
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("reconnection canceled: %w", ctx.Err())
			}
		}

		err := c.attemptReconnect(ctx, attempt+1)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrClientClosed) {
			return err
		}
		lastErr = err
	}

	return fmt.Errorf("reconnection failed after %d attempts: %w", c.options.MaxReconnectAttempts, lastErr)
}

// attemptReconnect performs a single reconnection attempt: restarts the process and re-initializes.
func (c *StdioClient) attemptReconnect(ctx context.Context, attemptNum int) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClientClosed
	}

	// Reset the context/cancel for the new process
	c.cancel()
	c.ctx, c.cancel = context.WithCancel(context.Background())

	err := c.startProcess()
	if err != nil {
		c.mu.Unlock()
		logger.Warn("MCP reconnection attempt failed to start process",
			"server", c.config.Name, "attempt", attemptNum, "error", err)
		return err
	}

	// Start background reader
	c.wg.Add(1)
	go c.readLoop()
	c.mu.Unlock()

	// Re-initialize the MCP handshake
	initCtx, initCancel := context.WithTimeout(ctx, c.options.InitTimeout)
	defer initCancel()

	req := InitializeRequest{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    ClientCapabilities{Elicitation: &ElicitationCapability{}},
		ClientInfo:      Implementation{Name: "promptkit", Version: "0.1.0"},
	}

	var resp InitializeResponse
	if err = c.sendRequestWithRetry(initCtx, "initialize", req, &resp); err != nil {
		logger.Warn("MCP reconnection attempt failed handshake",
			"server", c.config.Name, "attempt", attemptNum, "error", err)
		return err
	}

	// Send initialized notification (non-fatal)
	if notifyErr := c.sendNotification("notifications/initialized", nil); notifyErr != nil {
		logger.Warn("MCP initialized notification failed after reconnect",
			"server", c.config.Name, "error", notifyErr)
	}

	c.mu.Lock()
	c.serverInfo = &resp
	c.mu.Unlock()
	c.updateActivity()

	logger.Info("MCP reconnection successful", "server", c.config.Name, "attempt", attemptNum)
	return nil
}

// waitForReconnect waits for an in-progress reconnection to complete, then re-checks health.
// It uses a channel signal instead of polling to avoid wasted CPU and latency.
func (c *StdioClient) waitForReconnect(doneCh <-chan struct{}) error {
	// Wait for the reconnection to complete or timeout.
	timeout := time.Duration(reconnectPollMaxIterations) * reconnectPollInterval
	select {
	case <-doneCh:
		// Reconnection completed; check final state.
	case <-time.After(timeout):
		return fmt.Errorf("timed out waiting for reconnection")
	}

	c.mu.RLock()
	closed := c.closed
	alive := c.cmd != nil && c.cmd.Process != nil
	c.mu.RUnlock()

	if closed {
		return ErrClientClosed
	}
	if alive {
		return nil
	}
	return ErrProcessDied
}

// failPendingRequests sends an error response to all pending requests so they don't
// hang until their context timeout.
func (c *StdioClient) failPendingRequests() {
	c.pendingReqs.Range(func(key, value interface{}) bool {
		ch := value.(chan *JSONRPCMessage)
		errMsg := &JSONRPCMessage{
			JSONRPC: "2.0",
			ID:      key,
			Error: &JSONRPCError{
				Code:    -32000,
				Message: "server process died",
			},
		}
		select {
		case ch <- errMsg:
		default:
		}
		c.pendingReqs.Delete(key)
		return true
	})
}

// cleanupDeadProcess releases resources from a dead process without marking the client as closed.
func (c *StdioClient) cleanupDeadProcess() {
	// Close pipes (safe to call on already-closed pipes; errors are non-actionable)
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
	}
	if c.stderr != nil {
		_ = c.stderr.Close()
	}

	// Clean up zombie process
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}

	// Wait for background goroutines from the old process to finish
	c.wg.Wait()
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
