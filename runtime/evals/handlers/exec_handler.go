package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// defaultExecEvalTimeout is the default timeout for exec eval subprocesses.
const defaultExecEvalTimeout = 30 * time.Second

// ExecEvalHandler evaluates content by spawning an external subprocess.
// The subprocess receives an ExecEvalRequest as JSON on stdin and must
// return an ExecEvalResponse as JSON on stdout.
//
// ExecEvalHandler is registered dynamically from RuntimeConfig exec bindings,
// not via init(). Each binding creates a handler whose Type() matches the
// eval type name in the pack — making exec evals transparent to the pack.
type ExecEvalHandler struct {
	typeName  string
	command   string
	args      []string
	env       []string
	timeoutMs int
}

// ExecEvalConfig holds the configuration for creating an ExecEvalHandler.
type ExecEvalConfig struct {
	TypeName  string
	Command   string
	Args      []string
	Env       []string
	TimeoutMs int
}

// NewExecEvalHandler creates a new ExecEvalHandler from a config.
// Exec handlers are automatically classified as long-running and external
// for well-known group filtering.
func NewExecEvalHandler(cfg *ExecEvalConfig) *ExecEvalHandler {
	evals.RegisterTypeGroups(cfg.TypeName, []string{evals.GroupLongRunning, evals.GroupExternal})
	return &ExecEvalHandler{
		typeName:  cfg.TypeName,
		command:   cfg.Command,
		args:      cfg.Args,
		env:       cfg.Env,
		timeoutMs: cfg.TimeoutMs,
	}
}

// Type returns the eval type identifier (matches the pack eval type name).
func (h *ExecEvalHandler) Type() string { return h.typeName }

// execEvalRequest is the JSON payload written to the subprocess stdin.
type execEvalRequest struct {
	Type    string         `json:"type"`
	Params  map[string]any `json:"params,omitempty"`
	Content string         `json:"content"`
	Context *execEvalCtx   `json:"context"`
}

// execEvalCtx provides conversation context to the eval subprocess.
type execEvalCtx struct {
	Messages  []messageView  `json:"messages,omitempty"`
	TurnIndex int            `json:"turn_index"`
	ToolCalls []toolCallView `json:"tool_calls,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	PromptID  string         `json:"prompt_id,omitempty"`
}

// execEvalResponse is the JSON payload read from the subprocess stdout.
type execEvalResponse struct {
	Score  float64        `json:"score"`
	Detail string         `json:"detail,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

// Eval runs the external eval subprocess and returns the result.
func (h *ExecEvalHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	reqBytes, err := json.Marshal(h.buildRequest(evalCtx, params))
	if err != nil {
		return nil, fmt.Errorf("exec eval %q: marshaling request: %w", h.typeName, err)
	}

	timeout := h.timeout()
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout, stderr, err := h.runProcess(execCtx, reqBytes)
	if err != nil {
		detail := ""
		if len(stderr) > 0 {
			detail = fmt.Sprintf(" (stderr: %s)", bytes.TrimSpace(stderr))
		}
		return &evals.EvalResult{
			Type:        h.typeName,
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("exec eval failed: %v%s", err, detail),
		}, nil
	}

	var resp execEvalResponse
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return &evals.EvalResult{
			Type:        h.typeName,
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("exec eval returned invalid JSON: %v", err),
		}, nil
	}

	score := resp.Score
	return &evals.EvalResult{
		Type:        h.typeName,
		Score:       &score,
		Explanation: resp.Detail,
		Value:       resp.Data,
		Details:     resp.Data,
	}, nil
}

func (h *ExecEvalHandler) buildRequest(evalCtx *evals.EvalContext, params map[string]any) *execEvalRequest {
	req := &execEvalRequest{
		Type:    h.typeName,
		Params:  params,
		Content: evalCtx.CurrentOutput,
		Context: &execEvalCtx{
			TurnIndex: evalCtx.TurnIndex,
			Variables: evalCtx.Variables,
			Metadata:  evalCtx.Metadata,
			SessionID: evalCtx.SessionID,
			PromptID:  evalCtx.PromptID,
		},
	}

	req.Context.Messages = buildMessageViews(evalCtx)
	req.Context.ToolCalls = buildToolCallViews(evalCtx)

	return req
}

func (h *ExecEvalHandler) timeout() time.Duration {
	if h.timeoutMs > 0 {
		return time.Duration(h.timeoutMs) * time.Millisecond
	}
	return defaultExecEvalTimeout
}

func (h *ExecEvalHandler) runProcess(ctx context.Context, stdin []byte) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, h.command, h.args...) //nolint:gosec // command comes from trusted config
	cmd.Stdin = bytes.NewReader(stdin)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Pass through requested environment variables from the host
	if len(h.env) > 0 {
		cmd.Env = os.Environ()
		for _, name := range h.env {
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
