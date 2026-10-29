package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// defaultExecEvalTimeout is the default timeout for exec eval subprocesses.
const defaultExecEvalTimeout = 30 * time.Second

// execWaitDelay bounds how long a timed-out process's output is drained after
// it is killed, before its pipes are closed regardless.
const execWaitDelay = 100 * time.Millisecond

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
//
// Error says the program could not take its measurement (the app under test
// never started, say). It makes the result an Error, which fails an
// assertion whatever its thresholds — a placeholder score of 0 would pass a
// max_score gate. Metrics carries named sub-scores (functional_score,
// security_score, ...) and lands at details.metrics, where a report or CSV
// writer can tell numeric columns from the free-form Data.
type execEvalResponse struct {
	Score   float64            `json:"score"`
	Detail  string             `json:"detail,omitempty"`
	Data    map[string]any     `json:"data,omitempty"`
	Metrics map[string]float64 `json:"metrics,omitempty"`
	Error   string             `json:"error,omitempty"`
}

// detailsKeyMetrics is where an exec eval's named metrics are reported.
const detailsKeyMetrics = "metrics"

// EvalTimeout reports the binding's timeout so the eval runner does not cap
// it at its own, shorter per-eval default.
func (h *ExecEvalHandler) EvalTimeout(map[string]any) time.Duration {
	return h.timeout()
}

// Eval runs the external eval subprocess and returns the result. Anything
// that leaves no measurement — the process failing or timing out, output
// that is not the response JSON, a response carrying error — is an Error.
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
		if errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("timed out after %s", timeout)
		}
		detail := ""
		if len(stderr) > 0 {
			detail = fmt.Sprintf(" (stderr: %s)", bytes.TrimSpace(stderr))
		}
		return errorResult(h.typeName, fmt.Sprintf("exec eval failed: %v%s", err, detail)), nil
	}

	var resp execEvalResponse
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return errorResult(h.typeName, fmt.Sprintf("exec eval returned invalid JSON: %v", err)), nil
	}

	details := execDetails(resp.Data, resp.Metrics)
	if resp.Error != "" {
		result := errorResult(h.typeName, "exec eval reported error: "+resp.Error)
		result.Details = details
		return result, nil
	}

	score := resp.Score
	return &evals.EvalResult{
		Type:        h.typeName,
		Score:       &score,
		MetricValue: scorePtr(score),
		Explanation: resp.Detail,
		Value:       resp.Data,
		Details:     details,
	}, nil
}

// execDetails is the program's data with its metrics under details.metrics.
// The map is a copy, so the response's data is never mutated.
func execDetails(data map[string]any, metrics map[string]float64) map[string]any {
	if len(data) == 0 && len(metrics) == 0 {
		return nil
	}
	details := make(map[string]any, len(data)+1)
	for k, v := range data {
		details[k] = v
	}
	if len(metrics) > 0 {
		details[detailsKeyMetrics] = metrics
	}
	return details
}

func (h *ExecEvalHandler) buildRequest(evalCtx *evals.EvalContext, params map[string]any) *execEvalRequest {
	req := &execEvalRequest{
		Type:    h.typeName,
		Params:  params,
		Content: evalCtx.CurrentOutput,
		Context: &execEvalCtx{
			TurnIndex: evalCtx.TurnIndex,
			Variables: evalCtx.Variables,
			Metadata:  jsonSafeMetadata(evalCtx.Metadata),
			SessionID: evalCtx.SessionID,
			PromptID:  evalCtx.PromptID,
		},
	}

	req.Context.Messages = buildMessageViews(evalCtx)
	req.Context.ToolCalls = buildToolCallViews(evalCtx)

	return req
}

// jsonSafeMetadata returns the metadata entries that serialize to JSON.
// Hosts keep live objects in eval metadata (a tool registry, a judge
// provider) next to plain data such as sandbox details; one value json
// cannot encode must not stop the program from running, so it is dropped.
func jsonSafeMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return metadata
	}
	safe := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if _, err := json.Marshal(v); err != nil {
			logger.Debug("exec eval: dropping metadata entry that is not JSON", "key", k, "error", err)
			continue
		}
		safe[k] = v
	}
	return safe
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
	// Killing the process on timeout does not close its output pipes while a
	// child it spawned still holds them; without a bound, Run waits for that
	// child and the timeout is not enforced.
	cmd.WaitDelay = execWaitDelay

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
