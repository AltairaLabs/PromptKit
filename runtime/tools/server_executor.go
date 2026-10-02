package tools

import (
	"bufio"
	"bytes"
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

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
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
// Each tool gets its own subprocess, started lazily on first invocation and
// restarted if it has exited. Requests to one process may be in flight
// concurrently; responses are matched to requests by JSON-RPC id.
//
// # Security: Trust Boundary
//
// The command and arguments that start server processes come from pack
// files (tool definitions) and runtime config files (YAML manifests). These
// config files are the trust boundary: commands are not sandboxed, validated,
// or restricted in any way, which keeps the executor maximally flexible.
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
//
// One reader goroutine per process scans stdout and routes each JSON-RPC
// response to the call waiting on its id, so calls on one process may run
// concurrently and a canceled call's late reply is dropped rather than
// handed to the next caller. A second goroutine waits for the process to
// exit and closes done, which fails every pending call and lets the executor
// restart the process on the next invocation.
type serverProcess struct {
	cmd       *exec.Cmd
	stdinPipe io.WriteCloser
	stdin     *json.Encoder
	stdout    *os.File   // read end of the stdout pipe, owned by the reader goroutine
	writeMu   sync.Mutex // serializes writes of requests to stdin
	nextID    atomic.Int64

	pendingMu sync.Mutex
	pending   map[int64]chan serverReply

	readerDone chan struct{} // closed when the reader goroutine returns
	done       chan struct{} // closed once the process has exited and the reader has returned
	exitErr    error         // error for calls failed by the exit; valid after done is closed
	waitErr    error         // cmd.Wait's result; valid after done is closed
}

// serverReply is a raw JSON-RPC response line, or an error that prevents one.
type serverReply struct {
	data []byte
	err  error
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
	if ok {
		if proc.isRunning() {
			return proc, nil
		}
		// The process exited (or closed stdout): reap it and start a new one.
		_ = proc.close()
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

	// Use our own pipe for stdout rather than cmd.StdoutPipe: cmd.Wait closes
	// a StdoutPipe as soon as the process exits, which could discard a final
	// response the reader has not consumed yet.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stdout = stdoutW

	// Capture stderr for diagnostics. exec's copier goroutine writes to the
	// buffer until cmd.Wait returns, so it is only read after that.
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

	startErr := cmd.Start()
	_ = stdoutW.Close() // the child holds its own copy of the write end
	if startErr != nil {
		_ = stdoutR.Close()
		return nil, fmt.Errorf("starting server process: %w", startErr)
	}

	p := &serverProcess{
		cmd:        cmd,
		stdinPipe:  stdinPipe,
		stdin:      json.NewEncoder(stdinPipe),
		stdout:     stdoutR,
		pending:    make(map[int64]chan serverReply),
		readerDone: make(chan struct{}),
		done:       make(chan struct{}),
	}
	go p.readLoop()
	go p.waitLoop(&stderrBuf)
	return p, nil
}

// serverStdoutDrainTimeout bounds how long waitLoop waits, after the process
// exits, for the reader to drain stdout (a grandchild may hold it open).
const serverStdoutDrainTimeout = 500 * time.Millisecond

// readLoop reads response lines from stdout and routes each to its pending call.
func (p *serverProcess) readLoop() {
	defer close(p.readerDone)

	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), serverMaxLineSize)
	for scanner.Scan() {
		p.dispatch(scanner.Bytes())
	}

	// No further responses can arrive. A scan error (such as an oversized
	// line) leaves the stream unusable, so stop the process; on EOF the
	// process has exited or closed stdout, and callers stop using it.
	if err := scanner.Err(); err != nil {
		logger.Debug("server executor: reading server stdout", "error", err)
		_ = p.cmd.Process.Kill()
	}
}

// dispatch delivers one response line to the call waiting for its id.
func (p *serverProcess) dispatch(line []byte) {
	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		// The line cannot be routed, so fail every waiting call with it.
		p.failPending(fmt.Errorf("invalid JSON-RPC response: %w", err))
		return
	}

	p.pendingMu.Lock()
	ch, ok := p.pending[resp.ID]
	delete(p.pending, resp.ID)
	p.pendingMu.Unlock()

	if !ok {
		logger.Debug("server executor: dropping response with no pending call", "id", resp.ID)
		return
	}
	ch <- serverReply{data: bytes.Clone(line)}
}

// failPending delivers err to every pending call and clears the pending set.
func (p *serverProcess) failPending(err error) {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()
	for id, ch := range p.pending {
		ch <- serverReply{err: err}
		delete(p.pending, id)
	}
}

// waitLoop reaps the process, then records its exit and closes done.
func (p *serverProcess) waitLoop(stderrBuf *bytes.Buffer) {
	waitErr := p.cmd.Wait()

	select {
	case <-p.readerDone:
	case <-time.After(serverStdoutDrainTimeout):
	}
	_ = p.stdout.Close() // unblocks the reader if stdout is still held open
	<-p.readerDone

	msg := "server process exited"
	if waitErr != nil {
		msg += ": " + waitErr.Error()
	}
	if stderr := bytes.TrimSpace(stderrBuf.Bytes()); len(stderr) > 0 {
		msg += "; stderr: " + string(stderr)
	}
	p.exitErr = errors.New(msg)
	p.waitErr = waitErr
	close(p.done)
}

// call sends a JSON-RPC request and waits for its response.
// Calls may run concurrently; responses are matched to calls by id.
func (p *serverProcess) call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	// Check context before sending
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context canceled before send: %w", err)
	}

	id := p.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "execute",
		// Same params structure as exec tool requests
		Params: map[string]any{"args": args},
		ID:     id,
	}

	ch := make(chan serverReply, 1)
	p.pendingMu.Lock()
	p.pending[id] = ch
	p.pendingMu.Unlock()

	p.writeMu.Lock()
	err := p.stdin.Encode(req)
	p.writeMu.Unlock()
	if err != nil {
		p.unregister(id)
		return nil, fmt.Errorf("writing JSON-RPC request: %w", err)
	}

	select {
	case reply := <-ch:
		return reply.result(id)
	case <-ctx.Done():
		p.unregister(id)
		return nil, fmt.Errorf("context canceled waiting for response: %w", ctx.Err())
	case <-p.readerDone:
		// The reader delivers before it returns, so a reply for this call
		// would already be buffered in ch.
		select {
		case reply := <-ch:
			return reply.result(id)
		default:
		}
		p.unregister(id)
		return nil, p.stoppedErr(ctx)
	}
}

// stoppedErr is the error for a call whose process stopped producing output.
// It waits briefly for the exit so the error can carry the exit status and stderr.
func (p *serverProcess) stoppedErr(ctx context.Context) error {
	select {
	case <-p.done:
		return p.exitErr
	case <-ctx.Done():
		return fmt.Errorf("context canceled waiting for response: %w", ctx.Err())
	case <-time.After(serverStdoutDrainTimeout):
		return errors.New("server process closed stdout")
	}
}

// unregister removes a pending call, so a late response for it is dropped.
func (p *serverProcess) unregister(id int64) {
	p.pendingMu.Lock()
	delete(p.pending, id)
	p.pendingMu.Unlock()
}

// result parses the reply for the call with the given id.
func (r serverReply) result(id int64) (json.RawMessage, error) {
	if r.err != nil {
		return nil, r.err
	}
	return parseJSONRPCResponse(r.data, id)
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

// isRunning reports whether the server process can still serve calls: it has
// not exited and its stdout is still open.
func (p *serverProcess) isRunning() bool {
	select {
	case <-p.readerDone:
		return false
	default:
		return true
	}
}

// close terminates the server process by closing stdin and killing it if it
// does not exit within serverShutdownTimeout. It returns the process's exit error.
func (p *serverProcess) close() error {
	// Close stdin to signal the process to exit
	_ = p.stdinPipe.Close()

	select {
	case <-p.done:
	case <-time.After(serverShutdownTimeout):
		_ = p.cmd.Process.Kill()
		<-p.done
	}
	return p.waitErr
}
