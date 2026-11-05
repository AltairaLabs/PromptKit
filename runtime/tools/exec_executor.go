package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const execExecutorName = "exec"

// execWaitDelay bounds how long a timed-out process's output is drained after
// it is killed, before its pipes are closed regardless.
const execWaitDelay = 100 * time.Millisecond

// ExecExecutor runs tool invocations as one-shot subprocesses.
// The tool arguments are written as JSON to stdin; the subprocess result is read from stdout.
// Stderr output is captured and logged on failure.
//
// # Security: Trust Boundary
//
// The command and arguments executed by ExecExecutor come from pack files
// (tool definitions) and runtime config files (YAML manifests). These config
// files are the trust boundary: commands are not sandboxed, validated, or
// restricted in any way, which keeps the executor maximally flexible.
//
// Pack files and runtime config files MUST come from trusted sources.
// Untrusted or unreviewed packs should never be loaded, as they can execute
// arbitrary commands with the privileges of the host process.
type ExecExecutor struct{}

// Name returns the executor name used for mode-based routing.
func (e *ExecExecutor) Name() string { return execExecutorName }

// execToolRequest is the JSON payload written to the subprocess stdin.
type execToolRequest struct {
	Args json.RawMessage `json:"args"`
}

// execToolResponse is the JSON payload read from the subprocess stdout.
type execToolResponse struct {
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
	Pending *execPending    `json:"pending,omitempty"`
}

// execPending is a pending answer: the call is held until resolved, the same
// as a client tool awaiting fulfillment. See ExecuteAsync.
type execPending struct {
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// Execute runs the tool as a one-shot subprocess.
//
// Execute is the synchronous path, which has no way to suspend a call, so a
// subprocess that answers {"pending": ...} is an error here. The pipeline calls
// ExecuteAsync, where a pending answer suspends the call.
func (e *ExecExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	resp, stdout, err := e.run(ctx, descriptor, args)
	if err != nil {
		return nil, err
	}
	if resp.Pending != nil {
		return nil, fmt.Errorf(
			"exec executor: tool %q returned pending, which only the async execution path can suspend",
			descriptor.Name)
	}
	return execResult(resp, stdout), nil
}

// ExecuteAsync runs the tool as a one-shot subprocess and reports a
// {"pending": ...} answer as ToolStatusPending, on the same path client and
// other async tools use: the pipeline suspends the call and the caller resolves
// it later.
func (e *ExecExecutor) ExecuteAsync(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (*ToolExecutionResult, error) {
	resp, stdout, err := e.run(ctx, descriptor, args)
	if err != nil {
		return nil, err
	}
	if resp.Pending != nil {
		return &ToolExecutionResult{
			Status: ToolStatusPending,
			PendingInfo: &PendingToolInfo{
				Reason:   resp.Pending.Reason,
				Message:  resp.Pending.Message,
				ToolName: descriptor.Name,
				Args:     args,
			},
		}, nil
	}
	return &ToolExecutionResult{
		Status:  ToolStatusComplete,
		Content: execResult(resp, stdout),
	}, nil
}

// run spawns the subprocess and decodes its answer. An "error" answer is
// returned as an error.
func (e *ExecExecutor) run(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (resp execToolResponse, stdout []byte, err error) {
	if descriptor.ExecConfig == nil {
		return resp, nil, fmt.Errorf("exec executor: tool %q has no exec configuration", descriptor.Name)
	}
	cfg := descriptor.ExecConfig

	reqBytes, err := json.Marshal(execToolRequest{Args: args})
	if err != nil {
		return resp, nil, fmt.Errorf("exec executor: marshaling request: %w", err)
	}

	stdout, stderr, err := runExecProcess(ctx, cfg.Command, cfg.Args, cfg.Env, reqBytes)
	if err != nil {
		detail := ""
		if len(stderr) > 0 {
			detail = fmt.Sprintf(" (stderr: %s)", bytes.TrimSpace(stderr))
		}
		return resp, nil, fmt.Errorf("exec executor: tool %q failed: %w%s", descriptor.Name, err, detail)
	}

	if err := json.Unmarshal(stdout, &resp); err != nil {
		return resp, nil, fmt.Errorf("exec executor: tool %q returned invalid JSON: %w", descriptor.Name, err)
	}

	if resp.Error != "" {
		return resp, nil, fmt.Errorf("exec executor: tool %q: %s", descriptor.Name, resp.Error)
	}
	return resp, stdout, nil
}

// execResult is the tool's result: the "result" field, or the whole answer
// when there is none (backward compat).
func execResult(resp execToolResponse, stdout []byte) json.RawMessage {
	if resp.Result != nil {
		return resp.Result
	}
	return stdout
}

// runExecProcess spawns a subprocess, writes input to stdin, and captures stdout/stderr.
func runExecProcess(
	ctx context.Context, command string, args, envNames []string, stdin []byte,
) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, command, args...) //#nosec G204 -- command comes from trusted config
	cmd.Stdin = bytes.NewReader(stdin)
	// Killing the process on timeout does not close its output pipes while a
	// child it spawned still holds them; without a bound, Run waits for that
	// child and the timeout is not enforced.
	cmd.WaitDelay = execWaitDelay

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Pass through ONLY the explicitly requested environment variables.
	// When envNames is specified, the subprocess gets a minimal environment
	// to prevent accidental leakage of secrets from the parent process.
	if len(envNames) > 0 {
		cmd.Env = make([]string, 0, len(envNames))
		for _, name := range envNames {
			if val, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", name, val))
			}
		}
	}

	if err := cmd.Run(); err != nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), err
	}

	return stdoutBuf.Bytes(), stderrBuf.Bytes(), nil
}
