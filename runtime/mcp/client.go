package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// ClientOptions configures MCP client behavior
type ClientOptions struct {
	// RequestTimeout is the default timeout for RPC requests, on every
	// transport. A request that outlives it is abandoned and the server told so.
	RequestTimeout time.Duration
	// InitTimeout is the timeout for the initialization handshake
	InitTimeout time.Duration
	// MaxRetries is the number of times an idempotent request (initialize,
	// tools/list) is retried after a transport failure. tools/call is never
	// retried, and neither is a request the server answered with an error.
	MaxRetries int
	// RetryDelay is the initial delay between retries (exponential backoff)
	RetryDelay time.Duration
	// EnableGracefulDegradation allows operations to continue even if MCP is unavailable
	EnableGracefulDegradation bool
	// MaxReconnectAttempts is the maximum number of times to attempt reconnection
	// when a process death is detected. 0 disables auto-reconnection.
	MaxReconnectAttempts int
	// ElicitationHandler, when set, answers servers' requests for user input
	// and makes the client advertise the elicitation capability (form mode).
	// Without it the client does not advertise elicitation, and refuses
	// elicitation requests.
	ElicitationHandler ElicitationHandler
	// DisableModernProtocol skips stateless (2026-07-28) detection and always
	// uses the initialize handshake. For servers that misbehave when probed.
	DisableModernProtocol bool
	// EraProbeTimeout bounds how long the client waits for a stdio server to
	// answer the server/discover probe before treating it as a handshake-era
	// server. Defaults to 3s.
	EraProbeTimeout time.Duration
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
	// maxStdioMessageBytes bounds one newline-delimited message from a stdio
	// server. The spec sets no limit; this one only stops a runaway server
	// from exhausting memory. Exceeding it ends the connection.
	maxStdioMessageBytes = 64 << 20
	// stdioReadBufferBytes is the initial read buffer for stdout.
	stdioReadBufferBytes = 64 << 10
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
	// errMessageTooLarge ends a stdio connection whose server sent a message
	// over maxStdioMessageBytes.
	errMessageTooLarge = errors.New("mcp: stdio message exceeds size limit")
)

// StdioClient implements the MCP Client interface using stdio transport
type StdioClient struct {
	config  ServerConfig
	options ClientOptions
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser

	sess *session

	// pendingReqs maps a request id to the channel its response is delivered on.
	pendingReqs sync.Map // map[int64]chan *JSONRPCMessage
	writeMu     sync.Mutex

	// Lifecycle
	mu      sync.RWMutex
	started bool
	closed  bool
	// exited is set when the read loop ends: the process died or closed
	// stdout. The client is then not alive, and the next call reconnects.
	exited atomic.Bool

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
	c := &StdioClient{
		config:  config,
		options: options,
		ctx:     ctx,
		cancel:  cancel,
		sess:    newSession(config.Name, options),
	}
	c.sess.conn = &stdioConn{c: c}
	return c
}

// Initialize establishes the MCP connection and negotiates capabilities
func (c *StdioClient) Initialize(ctx context.Context) (*InitializeResponse, error) {
	c.mu.Lock()

	if c.started {
		c.mu.Unlock()
		return c.sess.info(), nil
	}

	if c.closed {
		c.mu.Unlock()
		return nil, ErrClientClosed
	}

	// Start the server process with retries (caller must hold c.mu; released on error)
	if err := c.startProcessWithRetry(ctx); err != nil {
		return nil, err
	}

	c.startReadLoop()
	c.started = true
	c.mu.Unlock()

	resp, err := c.sess.connect(ctx)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	c.updateActivity()
	return resp, nil
}

// ListTools retrieves all available tools from the server
func (c *StdioClient) ListTools(ctx context.Context) ([]Tool, error) {
	if err := c.checkHealth(); err != nil {
		return nil, err
	}
	tools, err := c.sess.listToolsDegrading(ctx)
	if err == nil {
		c.updateActivity()
	}
	return tools, err
}

// CallTool executes a tool with the given arguments
func (c *StdioClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*ToolCallResponse, error) {
	if err := c.checkHealth(); err != nil {
		return nil, err
	}
	resp, err := c.sess.callTool(ctx, name, arguments)
	if err != nil {
		return nil, err
	}
	c.updateActivity()
	return resp, nil
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

	// Close pipes and kill process
	c.closePipesAndProcess()

	// Wait for background goroutines
	c.wg.Wait()

	return nil
}

// closePipesAndProcess closes stdio pipes and kills the subprocess.
func (c *StdioClient) closePipesAndProcess() {
	warnClose := func(name string, closer io.Closer) {
		if closer != nil {
			if err := closer.Close(); err != nil {
				logger.Warn("MCP failed to close "+name, "server", c.config.Name, "error", err)
			}
		}
	}
	warnClose("stdin", c.stdin)
	warnClose("stdout", c.stdout)
	warnClose("stderr", c.stderr)

	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil {
			logger.Warn("MCP failed to kill process", "server", c.config.Name, "error", err)
		}
		if err := c.cmd.Wait(); err != nil {
			logger.Warn("MCP process wait failed", "server", c.config.Name, "error", err)
		}
	}
}

// IsAlive checks if the connection is still active
func (c *StdioClient) IsAlive() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.started && !c.closed && c.processRunning()
}

// processRunning reports whether the server process is up and its stdout is
// still being read. Caller holds c.mu.
func (c *StdioClient) processRunning() bool {
	return c.cmd != nil && c.cmd.Process != nil && !c.exited.Load()
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
	if c.config.WorkingDir != "" {
		c.cmd.Dir = c.config.WorkingDir
	}

	// Inherit the parent process environment and prepend common paths to PATH.
	// Replace the existing PATH entry in-place to avoid duplicate PATH variables.
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			env[i] = "PATH=/usr/local/bin:/usr/bin:/bin:" + os.Getenv("PATH")
			break
		}
	}
	c.cmd.Env = env
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
	c.exited.Store(false)

	// Start stderr logger
	c.wg.Add(1)
	go c.logStderr()

	return nil
}

// startReadLoop starts the stdout reader. Caller holds c.mu.
func (c *StdioClient) startReadLoop() {
	c.wg.Add(1)
	go c.readLoop(c.stdout)
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

	if c.processRunning() {
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
	if c.processRunning() {
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
	c.startReadLoop()
	c.mu.Unlock()

	// A new process is a new connection: establish the protocol again.
	if _, err = c.sess.connect(ctx); err != nil {
		logger.Warn("MCP reconnection attempt failed handshake",
			"server", c.config.Name, "attempt", attemptNum, "error", err)
		return err
	}
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
	alive := c.processRunning()
	c.mu.RUnlock()

	if closed {
		return ErrClientClosed
	}
	if alive {
		return nil
	}
	return ErrProcessDied
}

// failPendingRequests fails every request still waiting for a response, so
// callers don't hang until their timeout once the connection is gone.
func (c *StdioClient) failPendingRequests() {
	c.pendingReqs.Range(func(key, value any) bool {
		ch := value.(chan *JSONRPCMessage)
		errMsg := &JSONRPCMessage{
			JSONRPC: jsonRPCVersion,
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

// writeMessage writes one newline-delimited JSON-RPC message to stdin.
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

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// readLoop reads newline-delimited messages from stdout until it closes.
// When it ends, the connection is gone: pending requests fail at once and
// the client stops reporting itself alive.
func (c *StdioClient) readLoop(stdout io.Reader) {
	defer c.wg.Done()

	reader := bufio.NewReaderSize(stdout, stdioReadBufferBytes)
	for {
		line, err := readLine(reader, maxStdioMessageBytes)
		if err != nil {
			if !errors.Is(err, io.EOF) && !c.isClosed() {
				logger.Error("MCP stdio read failed", "server", c.config.Name, "error", err)
			}
			break
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			logger.Error("MCP failed to unmarshal message", "server", c.config.Name, "error", err)
			continue
		}
		c.handleMessage(&msg)
	}

	c.exited.Store(true)
	c.failPendingRequests()
}

// readLine reads one line of at most limit bytes. A longer line is an error:
// the stream cannot be resynchronized without reading it whole.
func readLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > limit {
			return nil, errMessageTooLarge
		}
		line = append(line, chunk...)
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return line, nil
		default:
			return nil, err
		}
	}
}

func (c *StdioClient) isClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// handleMessage routes one message from the server. A response goes to the
// request waiting for it. A server request or notification goes to the
// session — even when a server request's id matches one of ours, since a
// request carries a method and a response never does.
func (c *StdioClient) handleMessage(msg *JSONRPCMessage) {
	if isResponse(msg) {
		id, ok := coerceID(msg.ID)
		if !ok {
			logger.Warn("MCP invalid response ID type", "type", fmt.Sprintf("%T", msg.ID))
			return
		}
		if ch, ok := c.pendingReqs.LoadAndDelete(id); ok {
			select {
			case ch.(chan *JSONRPCMessage) <- msg:
			default:
			}
		}
		return
	}

	// Answer off the read loop: a handler may take time, and the loop must
	// keep delivering responses meanwhile.
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		dispatchInbound(c.ctx, c.sess, msg, func(reply *JSONRPCMessage) {
			if err := c.writeMessage(reply); err != nil {
				logger.Warn("MCP failed to answer server request", "server", c.config.Name, "error", err)
			}
		})
	}()
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

// stdioConn is the conn view of a StdioClient's pipes.
type stdioConn struct{ c *StdioClient }

func (s *stdioConn) send(ctx context.Context, req *request) (*JSONRPCMessage, error) {
	c := s.c
	respChan := make(chan *JSONRPCMessage, 1)
	c.pendingReqs.Store(req.id, respChan)
	defer c.pendingReqs.Delete(req.id)

	if err := c.writeMessage(req.message()); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respChan:
		return resp, nil
	}
}

func (s *stdioConn) notify(_ context.Context, req *request) error {
	return s.c.writeMessage(req.message())
}

// cancelRequest sends the cancellation notification: stdio has no per-request
// stream to close, so the notification is the only signal.
func (s *stdioConn) cancelRequest(ctx context.Context, id int64, reason string, header http.Header) {
	if err := s.notify(ctx, cancelNotification(id, reason, header)); err != nil {
		logger.Debug("MCP failed to send cancellation", "server", s.c.config.Name, "error", err)
	}
}

func (s *stdioConn) close() error { return s.c.Close() }

func (s *stdioConn) supportsModern() bool { return true }
