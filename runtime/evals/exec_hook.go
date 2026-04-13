package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// DefaultExecEvalHookTimeout bounds how long a single subprocess invocation
// of an ExecEvalHook may run before it is killed and the result abandoned.
const DefaultExecEvalHookTimeout = 5 * time.Second

// ExecEvalHookConfig configures an ExecEvalHook. It is the eval-side
// analog of hooks.ExecHookConfig: a command to spawn, its arguments
// and environment, a per-call timeout, and an optional Sandbox that
// controls how the subprocess is actually launched.
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
	// Sandbox, when non-nil, overrides the default direct-exec sandbox.
	// Use this to run eval hooks in a container, a sidecar, or a custom
	// backend; see runtime/hooks/sandbox for the interface.
	Sandbox sandbox.Sandbox
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
	sandbox   sandbox.Sandbox
}

// Compile-time interface check.
var _ EvalHook = (*ExecEvalHook)(nil)

// NewExecEvalHook constructs an ExecEvalHook from the given config.
// When cfg.Sandbox is nil the hook falls back to the built-in direct
// sandbox, which matches the historical local-exec behavior.
func NewExecEvalHook(cfg *ExecEvalHookConfig) *ExecEvalHook {
	sb := cfg.Sandbox
	if sb == nil {
		sb = direct.New(direct.ModeName)
	}
	return &ExecEvalHook{
		name:      cfg.Name,
		command:   cfg.Command,
		args:      cfg.Args,
		env:       cfg.Env,
		timeoutMs: cfg.TimeoutMs,
		sandbox:   sb,
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

	resp, spawnErr := h.sandbox.Spawn(ctx, sandbox.Request{
		Command: h.command,
		Args:    h.args,
		Env:     h.env,
		Stdin:   payload,
		Timeout: timeout,
	})
	// Spawn returns err only for transport-level failure; non-zero exit
	// and timeout come back as a populated Response.Err. Treat both the
	// same way — log and drop, per fire-and-forget contract.
	runErr := spawnErr
	if runErr == nil {
		runErr = resp.Err
	}
	if runErr != nil {
		logger.Warn("exec eval hook: subprocess failed",
			"hook", h.name,
			"eval_id", result.EvalID,
			"error", runErr,
			"stderr", bytes.TrimSpace(resp.Stderr),
		)
	}
}
