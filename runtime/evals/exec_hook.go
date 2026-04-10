package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// DefaultExecEvalHookTimeout bounds how long a single subprocess invocation
// of an ExecEvalHook may run before it is killed and the result abandoned.
const DefaultExecEvalHookTimeout = 5 * time.Second

// ExecEvalHookConfig configures an ExecEvalHook. It is the eval-side
// analog of hooks.ExecHookConfig: a command to spawn, its arguments
// and environment, and a per-call timeout.
type ExecEvalHookConfig struct {
	// Name is a stable identifier used in logs.
	Name string
	// Command is the absolute path (or PATH-resolvable name) of the
	// executable to spawn for each eval result.
	Command string
	// Args are additional arguments passed to the command.
	Args []string
	// Env is a list of environment variable names whose values are
	// forwarded from the host process to the subprocess.
	Env []string
	// TimeoutMs is the per-invocation timeout in milliseconds. Zero
	// means use DefaultExecEvalHookTimeout.
	TimeoutMs int
}

// ExecEvalHook is an EvalHook that spawns an external process per eval
// result and writes the JSON-encoded EvalResult to its stdin. It is
// strictly fire-and-forget: stdout is discarded, non-zero exits and
// invocation errors are logged but never propagated, and the hook never
// modifies the result.
//
// This mirrors the observe-mode behavior of hooks.ExecProviderHook but
// is specialised for evals, which have no allow/deny semantics.
type ExecEvalHook struct {
	name      string
	command   string
	args      []string
	env       []string
	timeoutMs int
}

// Compile-time interface check.
var _ EvalHook = (*ExecEvalHook)(nil)

// NewExecEvalHook constructs an ExecEvalHook from the given config.
func NewExecEvalHook(cfg *ExecEvalHookConfig) *ExecEvalHook {
	return &ExecEvalHook{
		name:      cfg.Name,
		command:   cfg.Command,
		args:      cfg.Args,
		env:       cfg.Env,
		timeoutMs: cfg.TimeoutMs,
	}
}

// Name returns the hook name.
func (h *ExecEvalHook) Name() string { return h.name }

// OnEvalResult marshals the result to JSON and pipes it to the configured
// subprocess on stdin. Errors (marshal failures, subprocess failures,
// timeouts) are logged and discarded — the eval pipeline continues
// regardless.
func (h *ExecEvalHook) OnEvalResult(
	ctx context.Context, _ *EvalDef, _ *EvalContext, result *EvalResult,
) {
	payload, err := json.Marshal(result)
	if err != nil {
		logger.Warn("exec eval hook: marshal error",
			"hook", h.name, "eval_id", result.EvalID, "error", err)
		return
	}

	timeout := time.Duration(h.timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultExecEvalHookTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, h.command, h.args...) //#nosec G204 -- command from trusted runtime config
	cmd.Stdin = bytes.NewReader(payload)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if len(h.env) > 0 {
		cmd.Env = os.Environ()
		for _, name := range h.env {
			if val, ok := os.LookupEnv(name); ok {
				cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", name, val))
			}
		}
	}

	if err := cmd.Run(); err != nil {
		logger.Warn("exec eval hook: subprocess failed",
			"hook", h.name,
			"eval_id", result.EvalID,
			"error", err,
			"stderr", bytes.TrimSpace(stderr.Bytes()),
		)
	}
}
