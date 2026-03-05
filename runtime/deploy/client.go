package deploy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// AdapterClient launches an adapter binary and communicates with it via
// JSON-RPC 2.0 over stdio. It implements the Provider interface so callers
// can use it interchangeably with an in-process provider.
type AdapterClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu     sync.Mutex
	nextID int
	closed bool
}

// jsonRPC constants matching the adapter SDK.
const (
	jsonRPCVersion     = "2.0"
	methodProviderInfo = "get_provider_info"
	methodValidate     = "validate_config"
	methodPlan         = "plan"
	methodApply        = "apply"
	methodDestroy      = "destroy"
	methodStatus       = "status"
	methodImport       = "import"
)

// rpcRequest is a JSON-RPC 2.0 request envelope.
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int    `json:"id"`
}

// rpcResponse is a JSON-RPC 2.0 response envelope.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("adapter error %d: %s", e.Code, e.Message)
}

const (
	// maxScanSize is the maximum size of a single JSON-RPC response line (10 MB).
	maxScanSize = 10 * 1024 * 1024
	// initialBufSize is the initial buffer size for the scanner (64 KB).
	initialBufSize = 64 * 1024
)

// NewAdapterClient starts the adapter binary at the given path and returns
// a client ready for JSON-RPC calls. The process is kept alive for the
// lifetime of the client; call Close when done.
//
// Deprecated: Use [NewAdapterClientWithContext] to pass a context that
// controls the subprocess lifetime.
func NewAdapterClient(binaryPath string) (*AdapterClient, error) {
	return NewAdapterClientWithContext(context.Background(), binaryPath)
}

// NewAdapterClientWithContext starts the adapter binary at the given path,
// using ctx to control the subprocess lifetime. If ctx is canceled, the
// subprocess is killed. Call Close when done.
func NewAdapterClientWithContext(ctx context.Context, binaryPath string) (*AdapterClient, error) {
	cmd := exec.CommandContext(ctx, binaryPath)
	return newAdapterClient(cmd)
}

// newAdapterClient starts the given exec.Cmd and wires up stdio pipes.
// Exported only for testing convenience (NewAdapterClient wraps this).
func newAdapterClient(cmd *exec.Cmd) (*AdapterClient, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start adapter: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, initialBufSize), maxScanSize)

	return &AdapterClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: scanner,
		nextID: 1,
	}, nil
}

// NewAdapterClientIO creates an AdapterClient backed by the given reader and
// writer instead of a subprocess. Useful for testing.
func NewAdapterClientIO(r io.Reader, w io.WriteCloser) *AdapterClient {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, initialBufSize), maxScanSize)
	return &AdapterClient{
		stdin:  w,
		stdout: scanner,
		nextID: 1,
	}
}

// Close shuts down the adapter process. It closes stdin (signaling EOF)
// and waits for the process to exit.
func (c *AdapterClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	_ = c.stdin.Close()

	if c.cmd != nil {
		return c.cmd.Wait()
	}
	return nil
}

// callCtx sends a JSON-RPC request and reads the response with context support.
// The context controls the read timeout — if no response arrives before the
// context deadline, the call returns the context error.
func (c *AdapterClient) callCtx(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("adapter client is closed")
	}

	id := c.nextID
	c.nextID++

	req := rpcRequest{
		JSONRPC: jsonRPCVersion,
		Method:  method,
		Params:  params,
		ID:      id,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	// Read response with context-based timeout.
	type scanResult struct {
		line []byte
		ok   bool
		err  error
	}
	ch := make(chan scanResult, 1)
	go func() {
		ok := c.stdout.Scan()
		ch <- scanResult{
			line: append([]byte(nil), c.stdout.Bytes()...),
			ok:   ok,
			err:  c.stdout.Err(),
		}
	}()

	var sr scanResult
	select {
	case <-ctx.Done():
		return fmt.Errorf("read response: %w", ctx.Err())
	case sr = <-ch:
	}

	if !sr.ok {
		if sr.err != nil {
			return fmt.Errorf("read response: %w", sr.err)
		}
		return fmt.Errorf("adapter closed connection unexpectedly")
	}

	var resp rpcResponse
	if err := json.Unmarshal(sr.line, &resp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("unmarshal result: %w", err)
		}
	}

	return nil
}

// GetProviderInfo returns metadata about the adapter.
func (c *AdapterClient) GetProviderInfo(ctx context.Context) (*ProviderInfo, error) {
	var info ProviderInfo
	if err := c.callCtx(ctx, methodProviderInfo, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ValidateConfig validates provider-specific configuration.
func (c *AdapterClient) ValidateConfig(ctx context.Context, req *ValidateRequest) (*ValidateResponse, error) {
	var resp ValidateResponse
	if err := c.callCtx(ctx, methodValidate, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Plan generates a deployment plan showing what would change.
func (c *AdapterClient) Plan(ctx context.Context, req *PlanRequest) (*PlanResponse, error) {
	var resp PlanResponse
	if err := c.callCtx(ctx, methodPlan, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// applyResult matches the adapter SDK's response shape for Apply.
type applyResult struct {
	AdapterState string `json:"adapter_state"`
}

// Apply executes the deployment. The callback is not invoked because the
// current adapter protocol returns all events in the final response rather
// than streaming them. The returned string is the opaque adapter state.
func (c *AdapterClient) Apply(ctx context.Context, req *PlanRequest, callback ApplyCallback) (string, error) {
	var result applyResult
	if err := c.callCtx(ctx, methodApply, req, &result); err != nil {
		return "", err
	}
	return result.AdapterState, nil
}

// Destroy tears down the deployment.
func (c *AdapterClient) Destroy(ctx context.Context, req *DestroyRequest, callback DestroyCallback) error {
	return c.callCtx(ctx, methodDestroy, req, nil)
}

// Status returns the current deployment status.
func (c *AdapterClient) Status(ctx context.Context, req *StatusRequest) (*StatusResponse, error) {
	var resp StatusResponse
	if err := c.callCtx(ctx, methodStatus, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Import imports a pre-existing resource into the deployment state.
func (c *AdapterClient) Import(ctx context.Context, req *ImportRequest) (*ImportResponse, error) {
	var resp ImportResponse
	if err := c.callCtx(ctx, methodImport, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Verify AdapterClient implements Provider at compile time.
var _ Provider = (*AdapterClient)(nil)
