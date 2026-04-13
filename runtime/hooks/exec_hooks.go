package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox/direct"
)

const defaultExecHookTimeout = 10 * time.Second

// ExecHookConfig holds the configuration for creating exec-based hooks.
//
// Sandbox, when non-nil, controls how the hook subprocess is launched.
// When nil, the exec hooks fall back to the built-in direct sandbox
// which matches the historical behavior: exec.CommandContext(Command,
// Args...) in-process with the local environment. SDK consumers that
// want their hooks to run elsewhere — in a sidecar, a disposable
// container, a managed cloud sandbox — construct a Sandbox
// implementation and pass it here.
type ExecHookConfig struct {
	Name      string
	Command   string
	Args      []string
	Env       []string
	TimeoutMs int
	Phases    []string
	Mode      string // "filter" | "observe"
	Sandbox   sandbox.Sandbox
}

// execHookRequest is the JSON payload sent to an exec hook subprocess on stdin.
type execHookRequest struct {
	Hook     string `json:"hook"`
	Phase    string `json:"phase"`
	Request  any    `json:"request,omitempty"`
	Response any    `json:"response,omitempty"`
	Event    any    `json:"event,omitempty"`
}

// execHookResponse is the JSON payload returned by an exec hook subprocess on stdout.
type execHookResponse struct {
	Allow    bool           `json:"allow"`
	Reason   string         `json:"reason,omitempty"`
	Enforced bool           `json:"enforced,omitempty"`
	Ack      bool           `json:"ack,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// execHookBase contains shared fields and methods for exec hook implementations.
type execHookBase struct {
	name      string
	command   string
	args      []string
	env       []string
	timeoutMs int
	phases    map[string]bool
	mode      string
	sandbox   sandbox.Sandbox
}

func newExecHookBase(cfg *ExecHookConfig) execHookBase {
	phases := make(map[string]bool, len(cfg.Phases))
	for _, p := range cfg.Phases {
		phases[p] = true
	}
	mode := cfg.Mode
	if mode == "" {
		mode = "filter"
	}
	sb := cfg.Sandbox
	if sb == nil {
		sb = direct.New(direct.ModeName)
	}
	return execHookBase{
		name:      cfg.Name,
		command:   cfg.Command,
		args:      cfg.Args,
		env:       cfg.Env,
		timeoutMs: cfg.TimeoutMs,
		phases:    phases,
		mode:      mode,
		sandbox:   sb,
	}
}

func (b *execHookBase) timeout() time.Duration {
	if b.timeoutMs > 0 {
		return time.Duration(b.timeoutMs) * time.Millisecond
	}
	return defaultExecHookTimeout
}

func (b *execHookBase) hasPhase(phase string) bool {
	return b.phases[phase]
}

func (b *execHookBase) isObserve() bool {
	return b.mode == "observe"
}

// runProcess delegates subprocess execution to the configured Sandbox.
// It preserves the previous signature so the filter/observe helpers
// below don't change. The timeout is applied by the sandbox (it's in
// the Request); the inbound ctx still carries cancellation.
func (b *execHookBase) runProcess(ctx context.Context, stdin []byte) (stdout, stderr []byte, err error) {
	resp, spawnErr := b.sandbox.Spawn(ctx, sandbox.Request{
		Command: b.command,
		Args:    b.args,
		Env:     b.env,
		Stdin:   stdin,
		Timeout: b.timeout(),
	})
	if spawnErr != nil {
		return resp.Stdout, resp.Stderr, spawnErr
	}
	return resp.Stdout, resp.Stderr, resp.Err
}

// execFilter runs the hook subprocess and returns a Decision based on its response.
// On process failure or invalid JSON, it denies (fail-closed).
func (b *execHookBase) execFilter(ctx context.Context, req *execHookRequest) Decision {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return Deny(fmt.Sprintf("exec hook %q: marshal error: %v", b.name, err))
	}

	execCtx, cancel := context.WithTimeout(ctx, b.timeout())
	defer cancel()

	stdout, stderr, err := b.runProcess(execCtx, reqBytes)
	if err != nil {
		detail := ""
		if len(stderr) > 0 {
			detail = fmt.Sprintf(" (stderr: %s)", bytes.TrimSpace(stderr))
		}
		return Deny(fmt.Sprintf("exec hook %q failed: %v%s", b.name, err, detail))
	}

	var resp execHookResponse
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return Deny(fmt.Sprintf("exec hook %q: invalid response JSON: %v", b.name, err))
	}

	if resp.Allow {
		return Allow
	}
	if resp.Enforced {
		return Enforced(resp.Reason, resp.Metadata)
	}
	if len(resp.Metadata) > 0 {
		return DenyWithMetadata(resp.Reason, resp.Metadata)
	}
	return Deny(resp.Reason)
}

// execObserve runs the hook subprocess but discards the result (fire-and-forget).
func (b *execHookBase) execObserve(ctx context.Context, req *execHookRequest) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return
	}

	execCtx, cancel := context.WithTimeout(ctx, b.timeout())
	defer cancel()

	_, _, _ = b.runProcess(execCtx, reqBytes) //nolint:dogsled // observe mode: discard all results
}

// --- ExecProviderHook ---

// ExecProviderHook implements ProviderHook by spawning an external subprocess.
type ExecProviderHook struct {
	execHookBase
}

// NewExecProviderHook creates a new ExecProviderHook from the given config.
func NewExecProviderHook(cfg *ExecHookConfig) *ExecProviderHook {
	return &ExecProviderHook{execHookBase: newExecHookBase(cfg)}
}

// Name returns the hook name.
func (h *ExecProviderHook) Name() string { return h.name }

// BeforeCall intercepts an LLM provider call before it is sent.
func (h *ExecProviderHook) BeforeCall(ctx context.Context, req *ProviderRequest) Decision {
	if !h.hasPhase("before_call") {
		return Allow
	}
	hookReq := &execHookRequest{Hook: "provider", Phase: "before_call", Request: req}
	if h.isObserve() {
		h.execObserve(ctx, hookReq)
		return Allow
	}
	return h.execFilter(ctx, hookReq)
}

// AfterCall intercepts an LLM provider call after it completes.
func (h *ExecProviderHook) AfterCall(
	ctx context.Context, req *ProviderRequest, resp *ProviderResponse,
) Decision {
	if !h.hasPhase("after_call") {
		return Allow
	}
	hookReq := &execHookRequest{Hook: "provider", Phase: "after_call", Request: req, Response: resp}
	if h.isObserve() {
		h.execObserve(ctx, hookReq)
		return Allow
	}
	return h.execFilter(ctx, hookReq)
}

// --- ExecToolHook ---

// ExecToolHook implements ToolHook by spawning an external subprocess.
type ExecToolHook struct {
	execHookBase
}

// NewExecToolHook creates a new ExecToolHook from the given config.
func NewExecToolHook(cfg *ExecHookConfig) *ExecToolHook {
	return &ExecToolHook{execHookBase: newExecHookBase(cfg)}
}

// Name returns the hook name.
func (h *ExecToolHook) Name() string { return h.name }

// BeforeExecution intercepts a tool call before execution.
func (h *ExecToolHook) BeforeExecution(ctx context.Context, req ToolRequest) Decision {
	if !h.hasPhase("before_execution") {
		return Allow
	}
	hookReq := &execHookRequest{Hook: "tool", Phase: "before_execution", Request: req}
	if h.isObserve() {
		h.execObserve(ctx, hookReq)
		return Allow
	}
	return h.execFilter(ctx, hookReq)
}

// AfterExecution intercepts a tool call after execution.
func (h *ExecToolHook) AfterExecution(ctx context.Context, req ToolRequest, resp ToolResponse) Decision {
	if !h.hasPhase("after_execution") {
		return Allow
	}
	hookReq := &execHookRequest{Hook: "tool", Phase: "after_execution", Request: req, Response: resp}
	if h.isObserve() {
		h.execObserve(ctx, hookReq)
		return Allow
	}
	return h.execFilter(ctx, hookReq)
}

// --- ExecSessionHook ---

// ExecSessionHook implements SessionHook by spawning an external subprocess.
type ExecSessionHook struct {
	execHookBase
}

// NewExecSessionHook creates a new ExecSessionHook from the given config.
func NewExecSessionHook(cfg *ExecHookConfig) *ExecSessionHook {
	return &ExecSessionHook{execHookBase: newExecHookBase(cfg)}
}

// Name returns the hook name.
func (h *ExecSessionHook) Name() string { return h.name }

// OnSessionStart handles the session start event.
func (h *ExecSessionHook) OnSessionStart(ctx context.Context, event SessionEvent) error {
	return h.runSessionPhase(ctx, "session_start", event)
}

// OnSessionUpdate handles a session update event.
func (h *ExecSessionHook) OnSessionUpdate(ctx context.Context, event SessionEvent) error {
	return h.runSessionPhase(ctx, "session_update", event)
}

// OnSessionEnd handles the session end event.
func (h *ExecSessionHook) OnSessionEnd(ctx context.Context, event SessionEvent) error {
	return h.runSessionPhase(ctx, "session_end", event)
}

func (h *ExecSessionHook) runSessionPhase(ctx context.Context, phase string, event SessionEvent) error {
	if !h.hasPhase(phase) {
		return nil
	}
	hookReq := &execHookRequest{Hook: "session", Phase: phase, Event: event}
	if h.isObserve() {
		h.execObserve(ctx, hookReq)
		return nil
	}
	// Filter mode: run and check for process errors
	reqBytes, err := json.Marshal(hookReq)
	if err != nil {
		return fmt.Errorf("exec session hook %q: marshal error: %w", h.name, err)
	}

	execCtx, cancel := context.WithTimeout(ctx, h.timeout())
	defer cancel()

	_, _, err = h.runProcess(execCtx, reqBytes)
	if err != nil {
		return fmt.Errorf("exec session hook %q failed: %w", h.name, err)
	}
	return nil
}
