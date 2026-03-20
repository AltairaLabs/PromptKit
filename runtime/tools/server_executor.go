package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const (
	serverExecutorName = "server"

	// serverMaxLineSize is the maximum line size for JSON-RPC responses (10 MB).
	serverMaxLineSize = 10 << 20

	// serverShutdownTimeout is the time to wait for a process to exit before killing it.
	serverShutdownTimeout = 2 * time.Second
)

// ServerExecutor runs tool invocations against a long-running subprocess.
// The subprocess stays alive across multiple calls and communicates via
// JSON-RPC 2.0 over stdin/stdout (one JSON object per line).
//
// Each tool gets its own subprocess, started lazily on first invocation.
// Requests are serialized per-process to maintain request-response ordering.
//
// # Security: Trust Boundary
//
// The command and arguments used to start server processes come from pack
// files (tool definitions) and runtime config files (YAML manifests). These
// config files are the trust boundary: commands are not sandboxed, validated,
// or restricted in any way. This is by design for maximum flexibility.
//
// Pack files and runtime config files MUST come from trusted sources.
// Untrusted or unreviewed packs should never be loaded, as they can execute
// arbitrary commands with the privileges of the host process.
type ServerExecutor struct {
	mu        sync.Mutex
	processes map[string]*serverProcess
}

// Name returns the executor name used for mode-based routing.
func (e *ServerExecutor) Name() string { return serverExecutorName }

// serverProcess manages a single long-running subprocess.
type serverProcess struct {
	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	stdin     *json.Encoder
	scanner   *bufio.Scanner
	mu        sync.Mutex // serializes request/response pairs
	nextID    atomic.Int64
}

// JSON-RPC 2.0 types (local definitions to avoid coupling with adaptersdk).

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      int64          `json:"id"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int64           `json:"id"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Execute sends a JSON-RPC request to the tool's server process and returns the result.
func (e *ServerExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.ExecConfig == nil {
		return nil, fmt.Errorf("server executor: tool %q has no exec configuration", descriptor.Name)
	}

	proc, err := e.getOrStart(descriptor)
	if err != nil {
		return nil, fmt.Errorf("server executor: starting process for tool %q: %w", descriptor.Name, err)
	}

	return proc.call(ctx, args)
}

// getOrStart returns the running server process for a tool, starting it if needed.
func (e *ServerExecutor) getOrStart(descriptor *ToolDescriptor) (*serverProcess, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.processes == nil {
		e.processes = make(map[string]*serverProcess)
	}

	proc, ok := e.processes[descriptor.Name]
	if ok && proc.isRunning() {
		return proc, nil
	}

	proc, err := startServerProcess(descriptor.ExecConfig)
	if err != nil {
		return nil, err
	}
	e.processes[descriptor.Name] = proc
	return proc, nil
}

// Close terminates all managed server processes.
func (e *ServerExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var firstErr error
	for name, proc := range e.processes {
		if err := proc.close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("closing server process %q: %w", name, err)
		}
	}
	e.processes = nil
	return firstErr
}

// startServerProcess spawns a long-running subprocess and sets up JSON-RPC communication.
func startServerProcess(cfg *ExecConfig) (*serverProcess, error) {
	cmd := exec.CommandContext(context.Background(), cfg.Command, cfg.Args...) //#nosec G204 -- trusted config

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for diagnostics
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if len(cfg.Env) > 0 {
		cmd.Env = os.Environ()
		for _, name := range cfg.Env {
			if val, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", name, val))
			}
		}
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting server process: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), serverMaxLineSize)

	return &serverProcess{
		cmd:       cmd,
		stdinPipe: stdinPipe,
		stdin:     json.NewEncoder(stdinPipe),
		scanner:   scanner,
	}, nil
}

// call sends a JSON-RPC request and waits for the response.
// Requests are serialized per-process to maintain ordering.
func (p *serverProcess) call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := p.nextID.Add(1)

	// Build params with the same structure as exec tool requests
	params := map[string]any{"args": args}

	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "execute",
		Params:  params,
		ID:      id,
	}

	// Check context before sending
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context canceled before send: %w", err)
	}

	if err := p.stdin.Encode(req); err != nil {
		return nil, fmt.Errorf("writing JSON-RPC request: %w", err)
	}

	// Read response with context cancellation
	responseCh := make(chan scanResult, 1)
	go func() {
		if p.scanner.Scan() {
			responseCh <- scanResult{data: p.scanner.Bytes()}
		} else {
			responseCh <- scanResult{err: p.scanner.Err()}
		}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context canceled waiting for response: %w", ctx.Err())
	case result := <-responseCh:
		if result.err != nil {
			return nil, fmt.Errorf("reading JSON-RPC response: %w", result.err)
		}
		if result.data == nil {
			return nil, fmt.Errorf("server process closed stdout unexpectedly")
		}
		return parseJSONRPCResponse(result.data, id)
	}
}

type scanResult struct {
	data []byte
	err  error
}

func parseJSONRPCResponse(data []byte, expectedID int64) (json.RawMessage, error) {
	var resp jsonRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC response: %w", err)
	}

	if resp.ID != expectedID {
		return nil, fmt.Errorf("JSON-RPC response ID mismatch: got %d, want %d", resp.ID, expectedID)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if resp.Result != nil {
		return resp.Result, nil
	}

	return json.RawMessage("null"), nil
}

// isRunning checks if the server process is still alive.
func (p *serverProcess) isRunning() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	// ProcessState is only set after Wait completes; nil means still running
	return p.cmd.ProcessState == nil
}

// close terminates the server process by closing stdin and killing if needed.
func (p *serverProcess) close() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	// Close stdin to signal the process to exit
	_ = p.stdinPipe.Close()

	// Give the process a brief chance to exit, then kill it
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(serverShutdownTimeout):
		_ = p.cmd.Process.Kill()
		return <-done
	}
}
