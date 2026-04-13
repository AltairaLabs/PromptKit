package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHookScript(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

// --- execHookBase tests ---

func TestExecHookBase_Timeout(t *testing.T) {
	b := newExecHookBase(&ExecHookConfig{TimeoutMs: 5000})
	assert.Equal(t, 5*time.Second, b.timeout())
}

func TestExecHookBase_DefaultTimeout(t *testing.T) {
	b := newExecHookBase(&ExecHookConfig{})
	assert.Equal(t, defaultExecHookTimeout, b.timeout())
}

func TestExecHookBase_HasPhase(t *testing.T) {
	b := newExecHookBase(&ExecHookConfig{Phases: []string{"before_call", "after_call"}})
	assert.True(t, b.hasPhase("before_call"))
	assert.True(t, b.hasPhase("after_call"))
	assert.False(t, b.hasPhase("unknown"))
}

func TestExecHookBase_DefaultMode(t *testing.T) {
	b := newExecHookBase(&ExecHookConfig{})
	assert.False(t, b.isObserve())
}

func TestExecHookBase_ObserveMode(t *testing.T) {
	b := newExecHookBase(&ExecHookConfig{Mode: "observe"})
	assert.True(t, b.isObserve())
}

// --- ExecProviderHook tests ---

func TestExecProviderHook_Name(t *testing.T) {
	h := NewExecProviderHook(&ExecHookConfig{Name: "pii_redactor"})
	assert.Equal(t, "pii_redactor", h.Name())
}

func TestExecProviderHook_BeforeCall_Allow(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": true}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "test",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{Model: "gpt-4o"})
	assert.True(t, d.Allow)
}

func TestExecProviderHook_BeforeCall_Deny(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "reason": "PII detected"}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "pii",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.False(t, d.Allow)
	assert.Equal(t, "PII detected", d.Reason)
}

func TestExecProviderHook_BeforeCall_Enforced(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "enforced": true, "reason": "PII redacted", "metadata": {"field": "email"}}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "pii",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.False(t, d.Allow)
	assert.True(t, d.Enforced)
	assert.Equal(t, "PII redacted", d.Reason)
	assert.Equal(t, "email", d.Metadata["field"])
}

func TestExecProviderHook_BeforeCall_DenyWithMetadata(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "reason": "blocked", "metadata": {"score": 0.9}}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "test",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.False(t, d.Allow)
	assert.Equal(t, "blocked", d.Reason)
	assert.NotNil(t, d.Metadata)
}

func TestExecProviderHook_BeforeCall_SkipsPhase(t *testing.T) {
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "test",
		Command: "/nonexistent",
		Phases:  []string{"after_call"}, // not before_call
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.True(t, d.Allow)
}

func TestExecProviderHook_AfterCall(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": true}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "test",
		Command: script,
		Phases:  []string{"after_call"},
	})

	d := h.AfterCall(context.Background(), &ProviderRequest{}, &ProviderResponse{})
	assert.True(t, d.Allow)
}

func TestExecProviderHook_AfterCall_SkipsPhase(t *testing.T) {
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "test",
		Command: "/nonexistent",
		Phases:  []string{"before_call"},
	})

	d := h.AfterCall(context.Background(), &ProviderRequest{}, &ProviderResponse{})
	assert.True(t, d.Allow)
}

func TestExecProviderHook_ProcessFailure_Denies(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
exit 1
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "fail",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.False(t, d.Allow)
	assert.Contains(t, d.Reason, "exec hook \"fail\" failed")
}

func TestExecProviderHook_InvalidJSON_Denies(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo 'not json'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "bad",
		Command: script,
		Phases:  []string{"before_call"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.False(t, d.Allow)
	assert.Contains(t, d.Reason, "invalid response JSON")
}

func TestExecProviderHook_ObserveMode(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "reason": "would deny"}'
`)
	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "observer",
		Command: script,
		Phases:  []string{"before_call"},
		Mode:    "observe",
	})

	// Observe mode always returns Allow regardless of subprocess output
	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.True(t, d.Allow)
}

func TestExecProviderHook_WithEnv(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": true}'
`)
	t.Setenv("EXEC_HOOK_TEST_VAR", "set")

	h := NewExecProviderHook(&ExecHookConfig{
		Name:    "env_test",
		Command: script,
		Phases:  []string{"before_call"},
		Env:     []string{"EXEC_HOOK_TEST_VAR"},
	})

	d := h.BeforeCall(context.Background(), &ProviderRequest{})
	assert.True(t, d.Allow)
}

// --- ExecToolHook tests ---

func TestExecToolHook_Name(t *testing.T) {
	h := NewExecToolHook(&ExecHookConfig{Name: "query_allowlist"})
	assert.Equal(t, "query_allowlist", h.Name())
}

func TestExecToolHook_BeforeExecution_Allow(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": true}'
`)
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "test",
		Command: script,
		Phases:  []string{"before_execution"},
	})

	d := h.BeforeExecution(context.Background(), ToolRequest{Name: "db_query"})
	assert.True(t, d.Allow)
}

func TestExecToolHook_BeforeExecution_Deny(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "reason": "query not allowed"}'
`)
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "allowlist",
		Command: script,
		Phases:  []string{"before_execution"},
	})

	d := h.BeforeExecution(context.Background(), ToolRequest{Name: "db_query"})
	assert.False(t, d.Allow)
	assert.Equal(t, "query not allowed", d.Reason)
}

func TestExecToolHook_BeforeExecution_SkipsPhase(t *testing.T) {
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "test",
		Command: "/nonexistent",
		Phases:  []string{"after_execution"},
	})

	d := h.BeforeExecution(context.Background(), ToolRequest{})
	assert.True(t, d.Allow)
}

func TestExecToolHook_AfterExecution(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": true}'
`)
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "test",
		Command: script,
		Phases:  []string{"after_execution"},
	})

	d := h.AfterExecution(context.Background(), ToolRequest{}, ToolResponse{})
	assert.True(t, d.Allow)
}

func TestExecToolHook_AfterExecution_SkipsPhase(t *testing.T) {
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "test",
		Command: "/nonexistent",
		Phases:  []string{"before_execution"},
	})

	d := h.AfterExecution(context.Background(), ToolRequest{}, ToolResponse{})
	assert.True(t, d.Allow)
}

func TestExecToolHook_ObserveMode(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"allow": false, "reason": "denied"}'
`)
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "observer",
		Command: script,
		Phases:  []string{"before_execution"},
		Mode:    "observe",
	})

	d := h.BeforeExecution(context.Background(), ToolRequest{})
	assert.True(t, d.Allow)
}

func TestExecToolHook_ProcessFailure(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
exit 1
`)
	h := NewExecToolHook(&ExecHookConfig{
		Name:    "fail",
		Command: script,
		Phases:  []string{"before_execution"},
	})

	d := h.BeforeExecution(context.Background(), ToolRequest{})
	assert.False(t, d.Allow)
	assert.Contains(t, d.Reason, "exec hook \"fail\" failed")
}

// --- ExecSessionHook tests ---

func TestExecSessionHook_Name(t *testing.T) {
	h := NewExecSessionHook(&ExecHookConfig{Name: "audit_logger"})
	assert.Equal(t, "audit_logger", h.Name())
}

func TestExecSessionHook_OnSessionStart(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"ack": true}'
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "audit",
		Command: script,
		Phases:  []string{"session_start"},
	})

	err := h.OnSessionStart(context.Background(), SessionEvent{SessionID: "s1"})
	require.NoError(t, err)
}

func TestExecSessionHook_OnSessionUpdate(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"ack": true}'
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "audit",
		Command: script,
		Phases:  []string{"session_update"},
	})

	err := h.OnSessionUpdate(context.Background(), SessionEvent{SessionID: "s1"})
	require.NoError(t, err)
}

func TestExecSessionHook_OnSessionEnd(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"ack": true}'
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "audit",
		Command: script,
		Phases:  []string{"session_end"},
	})

	err := h.OnSessionEnd(context.Background(), SessionEvent{SessionID: "s1"})
	require.NoError(t, err)
}

func TestExecSessionHook_SkipsPhase(t *testing.T) {
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "test",
		Command: "/nonexistent",
		Phases:  []string{"session_end"},
	})

	// session_start not in phases, should be no-op
	err := h.OnSessionStart(context.Background(), SessionEvent{})
	require.NoError(t, err)
}

func TestExecSessionHook_ObserveMode_IgnoresErrors(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
exit 1
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "observer",
		Command: script,
		Phases:  []string{"session_start"},
		Mode:    "observe",
	})

	err := h.OnSessionStart(context.Background(), SessionEvent{})
	require.NoError(t, err)
}

func TestExecSessionHook_FilterMode_ReturnsError(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
exit 1
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "strict",
		Command: script,
		Phases:  []string{"session_start"},
		Mode:    "filter",
	})

	err := h.OnSessionStart(context.Background(), SessionEvent{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exec session hook \"strict\" failed")
}

func TestExecSessionHook_AllPhases(t *testing.T) {
	script := writeHookScript(t, "hook.sh", `#!/bin/sh
echo '{"ack": true}'
`)
	h := NewExecSessionHook(&ExecHookConfig{
		Name:    "full",
		Command: script,
		Phases:  []string{"session_start", "session_update", "session_end"},
	})

	require.NoError(t, h.OnSessionStart(context.Background(), SessionEvent{}))
	require.NoError(t, h.OnSessionUpdate(context.Background(), SessionEvent{}))
	require.NoError(t, h.OnSessionEnd(context.Background(), SessionEvent{}))
}

// --- Interface compliance ---

var (
	_ ProviderHook = (*ExecProviderHook)(nil)
	_ ToolHook     = (*ExecToolHook)(nil)
	_ SessionHook  = (*ExecSessionHook)(nil)
)

// --- Sandbox integration ---

// recordingSandbox captures the Request it is given and returns a
// pre-canned Response. Used below to verify Exec*Hook wires through a
// caller-provided Sandbox instead of spawning locally.
type recordingSandbox struct {
	lastReq sandbox.Request
	resp    sandbox.Response
	err     error
}

func (s *recordingSandbox) Name() string { return "recording" }
func (s *recordingSandbox) Spawn(_ context.Context, req sandbox.Request) (sandbox.Response, error) {
	s.lastReq = req
	return s.resp, s.err
}

// TestExecHookConfig_CustomSandboxIsUsed verifies that when
// ExecHookConfig.Sandbox is set, the hook routes all subprocess
// invocations through it — no local exec happens.
func TestExecHookConfig_CustomSandboxIsUsed(t *testing.T) {
	stub := &recordingSandbox{
		resp: sandbox.Response{Stdout: []byte(`{"allow": true}`)},
	}
	hook := NewExecProviderHook(&ExecHookConfig{
		Name:      "pii",
		Command:   "/would/not/exist/on/disk",
		Args:      []string{"arg1"},
		Env:       []string{"PK_TEST_VAR=value"},
		TimeoutMs: 1234,
		Phases:    []string{"before_call"},
		Mode:      "filter",
		Sandbox:   stub,
	})

	decision := hook.BeforeCall(context.Background(), &ProviderRequest{})
	if !decision.Allow {
		t.Errorf("Allow = %v, want true", decision.Allow)
	}

	// The stub captured the request — verify everything routed through it.
	if stub.lastReq.Command != "/would/not/exist/on/disk" {
		t.Errorf("Command = %q", stub.lastReq.Command)
	}
	if len(stub.lastReq.Args) != 1 || stub.lastReq.Args[0] != "arg1" {
		t.Errorf("Args = %v", stub.lastReq.Args)
	}
	if len(stub.lastReq.Env) != 1 || stub.lastReq.Env[0] != "PK_TEST_VAR=value" {
		t.Errorf("Env = %v", stub.lastReq.Env)
	}
	if stub.lastReq.Timeout != 1234*time.Millisecond {
		t.Errorf("Timeout = %v, want 1234ms", stub.lastReq.Timeout)
	}
	if len(stub.lastReq.Stdin) == 0 {
		t.Error("Stdin should carry the exec-hook JSON payload")
	}
}

// TestExecHookConfig_NilSandboxDefaultsToDirect verifies that an
// ExecHookConfig with Sandbox left unset still works — falling back to
// the direct backend (historical behavior).
func TestExecHookConfig_NilSandboxDefaultsToDirect(t *testing.T) {
	// Use /bin/cat or similar only if present; we just need to verify
	// that the hook constructs and invokes cleanly without a panic.
	hook := NewExecProviderHook(&ExecHookConfig{
		Name:      "nil-sandbox",
		Command:   "/nonexistent/command/for/this/test",
		Phases:    []string{"before_call"},
		Mode:      "filter",
		TimeoutMs: 100,
	})
	// The hook uses the default direct sandbox; invoking it against a
	// nonexistent command should produce a deny (filter mode, process
	// failed), not a panic or crash.
	decision := hook.BeforeCall(context.Background(), &ProviderRequest{})
	if decision.Allow {
		t.Error("missing command with filter mode should deny, not allow")
	}
}
