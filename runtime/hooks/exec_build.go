package hooks

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
)

// Hook type names as used in config.ExecHook.Hook.
const (
	HookTypeProvider = "provider"
	HookTypeTool     = "tool"
	HookTypeSession  = "session"
	HookTypeEval     = "eval"
)

// ResolveSandboxes builds a map from declared sandbox names to ready Sandbox
// instances using the process-wide factory registry. Factories must have been
// registered (via sandbox.RegisterFactory or a backend's init) beforehand.
// Shared by the SDK and Arena so both resolve sandboxes identically.
func ResolveSandboxes(specs map[string]*config.SandboxConfig) (map[string]sandbox.Sandbox, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[string]sandbox.Sandbox, len(specs))
	for name, sb := range specs {
		if sb == nil {
			continue
		}
		factory, err := sandbox.LookupFactory(sb.Mode)
		if err != nil {
			return nil, fmt.Errorf("sandbox %q: %w", name, err)
		}
		inst, err := factory(name, sb.Config)
		if err != nil {
			return nil, fmt.Errorf("building sandbox %q: %w", name, err)
		}
		out[name] = inst
	}
	return out, nil
}

// BuildExecHooks converts runtime-config exec-hook bindings into provider, tool,
// and session hook instances, resolving each binding's named sandbox from the
// provided map. Bindings with Hook=="eval" are skipped — eval hooks live in the
// evals package and are wired by the caller. This is the single source of truth
// for turning config.ExecHook bindings into runtime hooks, used by both the SDK
// and Arena so the two never drift.
func BuildExecHooks(bindings map[string]*config.ExecHook, sandboxes map[string]sandbox.Sandbox) (
	provider []ProviderHook, tool []ToolHook, session []SessionHook, err error) {
	for name, b := range bindings {
		if b == nil {
			continue
		}
		var sb sandbox.Sandbox
		if b.Sandbox != "" {
			var ok bool
			sb, ok = sandboxes[b.Sandbox]
			if !ok {
				return nil, nil, nil, fmt.Errorf("hook %q references undeclared sandbox %q", name, b.Sandbox)
			}
		}
		cfg := &ExecHookConfig{
			Name:      name,
			Command:   b.Command,
			Args:      b.Args,
			Env:       b.Env,
			TimeoutMs: b.TimeoutMs,
			Phases:    b.Phases,
			Mode:      b.Mode,
			Sandbox:   sb,
		}
		switch b.Hook {
		case HookTypeProvider:
			provider = append(provider, NewExecProviderHook(cfg))
		case HookTypeTool:
			tool = append(tool, NewExecToolHook(cfg))
		case HookTypeSession:
			session = append(session, NewExecSessionHook(cfg))
		case HookTypeEval:
			// Eval hooks are wired by the caller via the evals package.
		default:
			return nil, nil, nil, fmt.Errorf("hook %q: unknown hook type %q", name, b.Hook)
		}
	}
	return provider, tool, session, nil
}
