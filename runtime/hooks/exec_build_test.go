package hooks

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestResolveSandboxes_NilAndEmpty(t *testing.T) {
	out, err := ResolveSandboxes(nil)
	if err != nil || out != nil {
		t.Fatalf("nil specs: got out=%v err=%v", out, err)
	}
	out, err = ResolveSandboxes(map[string]*config.SandboxConfig{"skip": nil})
	if err != nil {
		t.Fatalf("nil entry: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %d", len(out))
	}
}

func TestResolveSandboxes_UnknownMode(t *testing.T) {
	_, err := ResolveSandboxes(map[string]*config.SandboxConfig{
		"x": {Mode: "not_a_registered_mode"},
	})
	if err == nil {
		t.Fatal("expected error for unregistered sandbox mode")
	}
}

func TestBuildExecHooks_ByType(t *testing.T) {
	bindings := map[string]*config.ExecHook{
		"p": {Hook: "provider", ExecBinding: config.ExecBinding{Command: "echo"}},
		"t": {Hook: "tool", ExecBinding: config.ExecBinding{Command: "echo"}},
		"s": {Hook: "session", ExecBinding: config.ExecBinding{Command: "echo"}},
		"e": {Hook: "eval", ExecBinding: config.ExecBinding{Command: "echo"}}, // skipped
	}
	provider, tool, session, err := BuildExecHooks(bindings, nil)
	if err != nil {
		t.Fatalf("BuildExecHooks: %v", err)
	}
	if len(provider) != 1 || len(tool) != 1 || len(session) != 1 {
		t.Fatalf("expected 1 provider/tool/session (eval skipped), got p=%d t=%d s=%d",
			len(provider), len(tool), len(session))
	}
}

func TestBuildExecHooks_NilBindingSkipped(t *testing.T) {
	p, to, s, err := BuildExecHooks(map[string]*config.ExecHook{"nil": nil}, nil)
	if err != nil || len(p)+len(to)+len(s) != 0 {
		t.Fatalf("nil binding should be skipped: p=%d t=%d s=%d err=%v", len(p), len(to), len(s), err)
	}
}

func TestBuildExecHooks_UnknownType(t *testing.T) {
	_, _, _, err := BuildExecHooks(map[string]*config.ExecHook{
		"x": {Hook: "bogus", ExecBinding: config.ExecBinding{Command: "echo"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown hook type")
	}
}

func TestBuildExecHooks_UndeclaredSandbox(t *testing.T) {
	_, _, _, err := BuildExecHooks(map[string]*config.ExecHook{
		"x": {Hook: "session", ExecBinding: config.ExecBinding{Command: "echo", Sandbox: "ghost"}},
	}, nil)
	if err == nil {
		t.Fatal("expected error for undeclared sandbox reference")
	}
}
