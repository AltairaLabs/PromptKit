package engine

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// applyRuntimeHooks wires the pass-through runtime config (config.Runtime) into
// the engine. Arena wraps the runtime, so hooks declared under `runtime:` are
// turned into runtime hooks with the SAME builder the SDK uses
// (hooks.BuildExecHooks) — the two never drift.
//
// Currently only session-type hooks are threaded into the engine (they fire the
// SessionHook lifecycle around each run). Provider and tool hooks declared in
// config are rejected with a clear error, because Arena's per-turn pipeline does
// not yet consume them — failing loudly beats silently dropping them.
func (e *Engine) applyRuntimeHooks() error {
	if e.config == nil || e.config.Runtime == nil || len(e.config.Runtime.Hooks) == 0 {
		return nil
	}
	rc := e.config.Runtime

	sandboxes, err := hooks.ResolveSandboxes(rc.Sandboxes)
	if err != nil {
		return fmt.Errorf("resolving sandboxes: %w", err)
	}
	provider, tool, session, err := hooks.BuildExecHooks(rc.Hooks, sandboxes)
	if err != nil {
		return err
	}
	if len(provider) > 0 || len(tool) > 0 {
		return fmt.Errorf("arena config currently supports only session-type runtime hooks; " +
			"provider/tool hooks are not yet wired into the per-turn pipeline")
	}
	if len(session) == 0 {
		return nil
	}

	opts := make([]hooks.Option, 0, len(session))
	for _, h := range session {
		opts = append(opts, hooks.WithSessionHook(h))
	}
	e.WithSessionHooks(hooks.NewRegistry(opts...))
	return nil
}
