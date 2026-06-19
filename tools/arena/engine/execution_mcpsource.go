package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
)

// scenarioVariables extracts a string map from the scenario's variables block
// for use in MCP source arg templating ({{scenario.<key>}} expansion).
// Returns nil when the scenario is nil or has no variables.
func scenarioVariables(sc *config.Scenario) map[string]string {
	if sc == nil || len(sc.Variables) == 0 {
		return nil
	}
	out := make(map[string]string, len(sc.Variables))
	for k, v := range sc.Variables {
		out[k] = v
	}
	return out
}

// openScenarioSessionMCPSources opens scenario- and session-scoped MCPSource
// entries for this run. The returned cleanup closure must be called (via
// defer) to tear down the opens; it's always safe to call even on the
// success-return path. When no source-backed entries are configured, the
// cleanup is a no-op.
//
// Each executeRun invocation is one session (= one trial). To allow
// concurrent runs of source-backed MCP entries that present at different
// URLs under the same logical name, this function forks the engine's
// MCP registry and runs the scenario+session opens against the fork. The
// returned per-run registry is attached to runCtx so MCPExecutor routes
// to the run's own server set; on cleanup the fork is unregistered and
// dropped.
func (e *Engine) openScenarioSessionMCPSources(
	ctx context.Context,
	scenario *config.Scenario,
	scenarioID, runID string,
) (runCtx context.Context, cleanup func(), err error) {
	cleanup = func() {}
	if e.mcpSourceScope == nil || len(e.mcpConfig) == 0 {
		return ctx, cleanup, nil
	}

	// Per-run forked registry isolates this run's session-scoped servers
	// from concurrent runs that may use the same logical names.
	runMCPRegistry := e.mcpRegistry.Fork()
	runScope := newMCPSourceScopeWithTools(runMCPRegistry, e.toolRegistry)
	runCtx = tools.WithMCPRegistry(ctx, runMCPRegistry)

	scenarioVars := scenarioVariables(scenario)
	if openErr := runScope.OpenAll(
		runCtx, mcpsource.ScopeScenario, scenarioID, scenarioVars,
		e.mcpSkillSources, e.mcpConfig,
	); openErr != nil {
		return ctx, cleanup, fmt.Errorf("open scenario-scoped MCP sources: %v", openErr)
	}
	closeScenario := func() {
		for _, cerr := range runScope.CloseAll(mcpsource.ScopeScenario, scenarioID) {
			logger.Warn("mcp source close failed (scenario scope)", "error", cerr)
		}
	}

	if openErr := runScope.OpenAll(
		runCtx, mcpsource.ScopeSession, runID, scenarioVars,
		e.mcpSkillSources, e.mcpConfig,
	); openErr != nil {
		closeScenario()
		return ctx, cleanup, fmt.Errorf("open session-scoped MCP sources: %v", openErr)
	}
	closeSession := func() {
		for _, cerr := range runScope.CloseAll(mcpsource.ScopeSession, runID) {
			logger.Warn("mcp source close failed (session scope)", "error", cerr)
		}
	}

	// Carry this run's open sandbox container IDs on the context so the session
	// metadata can expose them to hooks (e.g. a workspace-capture hook). They
	// live on the per-run runScope, not the engine's shared scope.
	runCtx = withSandboxContainerIDs(runCtx, runScope.containerIDs())

	return runCtx, func() {
		closeSession()
		closeScenario()
	}, nil
}

type sandboxContainersKey struct{}

// withSandboxContainerIDs returns a context carrying the open sandbox container
// IDs (keyed by server name). A no-op when ids is empty.
func withSandboxContainerIDs(ctx context.Context, ids map[string]string) context.Context {
	if len(ids) == 0 {
		return ctx
	}
	return context.WithValue(ctx, sandboxContainersKey{}, ids)
}

// sandboxContainerIDsFromContext returns the sandbox container IDs carried by
// ctx, or nil when none were set.
func sandboxContainerIDsFromContext(ctx context.Context) map[string]string {
	ids, _ := ctx.Value(sandboxContainersKey{}).(map[string]string)
	return ids
}
