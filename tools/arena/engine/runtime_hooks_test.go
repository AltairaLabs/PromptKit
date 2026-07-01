package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/tools/arena/arenaconfig"
)

func sessionHookConfig() *arenaconfig.Config {
	return &arenaconfig.Config{Runtime: &config.RuntimeConfigSpec{
		Hooks: map[string]*config.ExecHook{
			"capture": {Hook: "session", ExecBinding: config.ExecBinding{Command: "echo"}},
		},
	}}
}

func TestApplyRuntimeHooks_NilRuntimeIsNoop(t *testing.T) {
	e := &Engine{config: &arenaconfig.Config{}}
	if err := e.applyRuntimeHooks(); err != nil {
		t.Fatalf("nil runtime should be a no-op, got %v", err)
	}
	if e.sessionHooks != nil {
		t.Fatal("no hooks should be registered for an empty runtime")
	}
}

func TestApplyRuntimeHooks_RegistersSessionHook(t *testing.T) {
	e := &Engine{config: sessionHookConfig()}
	if err := e.applyRuntimeHooks(); err != nil {
		t.Fatalf("applyRuntimeHooks: %v", err)
	}
	if e.sessionHooks == nil {
		t.Fatal("expected a session hook registry to be wired")
	}
}

func TestApplyRuntimeHooks_RejectsProviderAndToolHooks(t *testing.T) {
	for _, hookType := range []string{"provider", "tool"} {
		e := &Engine{config: &arenaconfig.Config{Runtime: &config.RuntimeConfigSpec{
			Hooks: map[string]*config.ExecHook{
				"x": {Hook: hookType, ExecBinding: config.ExecBinding{Command: "echo"}},
			},
		}}}
		if err := e.applyRuntimeHooks(); err == nil {
			t.Fatalf("%s hook should be rejected (not yet wired into the pipeline)", hookType)
		}
	}
}

func TestApplyRuntimeHooks_UndeclaredSandboxErrors(t *testing.T) {
	e := &Engine{config: &arenaconfig.Config{Runtime: &config.RuntimeConfigSpec{
		Hooks: map[string]*config.ExecHook{
			"x": {Hook: "session", ExecBinding: config.ExecBinding{Command: "echo", Sandbox: "ghost"}},
		},
	}}}
	if err := e.applyRuntimeHooks(); err == nil {
		t.Fatal("expected error for hook referencing an undeclared sandbox")
	}
}
