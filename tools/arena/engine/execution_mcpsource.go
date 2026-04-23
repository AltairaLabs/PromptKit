package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
// Each executeRun invocation is one session (= one trial); scenario scope
// is opened and torn down per invocation because arena runs scenarios
// concurrently, so there's no natural "scenario start / scenario end"
// across multiple combinations.
func (e *Engine) openScenarioSessionMCPSources(
	ctx context.Context,
	scenario *config.Scenario,
	scenarioID, runID string,
) (cleanup func(), err error) {
	cleanup = func() {}
	if e.mcpSourceScope == nil || len(e.mcpConfig) == 0 {
		return cleanup, nil
	}

	scenarioVars := scenarioVariables(scenario)
	if openErr := e.mcpSourceScope.OpenAll(
		ctx, mcpsource.ScopeScenario, scenarioID, scenarioVars,
		e.mcpSkillSources, e.mcpConfig,
	); openErr != nil {
		return cleanup, fmt.Errorf("open scenario-scoped MCP sources: %v", openErr)
	}
	closeScenario := func() {
		for _, cerr := range e.mcpSourceScope.CloseAll(mcpsource.ScopeScenario, scenarioID) {
			logger.Warn("mcp source close failed (scenario scope)", "error", cerr)
		}
	}

	if openErr := e.mcpSourceScope.OpenAll(
		ctx, mcpsource.ScopeSession, runID, scenarioVars,
		e.mcpSkillSources, e.mcpConfig,
	); openErr != nil {
		// Roll back the scenario open before returning.
		closeScenario()
		return cleanup, fmt.Errorf("open session-scoped MCP sources: %v", openErr)
	}
	closeSession := func() {
		for _, cerr := range e.mcpSourceScope.CloseAll(mcpsource.ScopeSession, runID) {
			logger.Warn("mcp source close failed (session scope)", "error", cerr)
		}
	}

	return func() {
		closeSession()
		closeScenario()
	}, nil
}
