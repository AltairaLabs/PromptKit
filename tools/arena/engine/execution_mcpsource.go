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
// The returned apiURLs map (serverName → http base URL) is populated from
// session-scoped servers that expose a sandbox HTTP API (e.g. /api/download).
// It is empty when no such servers are configured.
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
) (runCtx context.Context, cleanup func(), apiURLs map[string]string, err error) {
	cleanup = func() {}
	if e.mcpSourceScope == nil || len(e.mcpConfig) == 0 {
		return ctx, cleanup, nil, nil
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
		return ctx, cleanup, nil, fmt.Errorf("open scenario-scoped MCP sources: %v", openErr)
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
		return ctx, cleanup, nil, fmt.Errorf("open session-scoped MCP sources: %v", openErr)
	}
	closeSession := func() {
		for _, cerr := range runScope.CloseAll(mcpsource.ScopeSession, runID) {
			logger.Warn("mcp source close failed (session scope)", "error", cerr)
		}
	}

	// Collect API URLs from session-scoped servers (container still alive).
	urls := runScope.APIURLs(mcpsource.ScopeSession, runID)

	return runCtx, func() {
		closeSession()
		closeScenario()
	}, urls, nil
}
