package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

const execExecutorName = "exec"

// ExecExecutor runs tool invocations as one-shot subprocesses.
// The tool arguments are written as JSON to stdin; the subprocess result is read from stdout.
// Stderr output is captured and logged on failure.
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

// execPending represents a pending/async tool result.
type execPending struct {
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// Execute runs the tool as a one-shot subprocess.
func (e *ExecExecutor) Execute(
	ctx context.Context, descriptor *ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if descriptor.ExecConfig == nil {
		return nil, fmt.Errorf("exec executor: tool %q has no exec configuration", descriptor.Name)
	}
	cfg := descriptor.ExecConfig

	reqBytes, err := json.Marshal(execToolRequest{Args: args})
	if err != nil {
		return nil, fmt.Errorf("exec executor: marshaling request: %w", err)
	}

	stdout, stderr, err := runExecProcess(ctx, cfg.Command, cfg.Args, cfg.Env, reqBytes)
	if err != nil {
		detail := ""
		if len(stderr) > 0 {
			detail = fmt.Sprintf(" (stderr: %s)", bytes.TrimSpace(stderr))
		}
		return nil, fmt.Errorf("exec executor: tool %q failed: %w%s", descriptor.Name, err, detail)
	}

	var resp execToolResponse
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return nil, fmt.Errorf("exec executor: tool %q returned invalid JSON: %w", descriptor.Name, err)
	}

	if resp.Error != "" {
		return nil, fmt.Errorf("exec executor: tool %q: %s", descriptor.Name, resp.Error)
	}

	if resp.Result != nil {
		return resp.Result, nil
	}

	// If no result field, return the full response (backward compat)
	return stdout, nil
}

// runExecProcess spawns a subprocess, writes input to stdin, and captures stdout/stderr.
func runExecProcess(
	ctx context.Context, command string, args, envNames []string, stdin []byte,
) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, command, args...) //#nosec G204 -- command comes from trusted config
	cmd.Stdin = bytes.NewReader(stdin)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Pass through requested environment variables from the host
	if len(envNames) > 0 {
		cmd.Env = os.Environ()
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
