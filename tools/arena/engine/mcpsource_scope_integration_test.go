package engine

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// counterSource is a minimal MCPSource that counts opens/closes for
// integration tests of scope hooks.
type counterSource struct {
	opens  atomic.Int32
	closes atomic.Int32
}

func (c *counterSource) Open(_ context.Context, _ map[string]any) (mcpsource.MCPConn, io.Closer, error) {
	c.opens.Add(1)
	return mcpsource.MCPConn{URL: "http://fake"}, closerFunc(func() error {
		c.closes.Add(1)
		return nil
	}), nil
}

// TestScopeHooks_SessionOpenedPerRepetition simulates N session cycles
// (each representing one arena repetition/trial) and verifies the
// source is opened and closed exactly once per cycle.
func TestScopeHooks_SessionOpenedPerRepetition(t *testing.T) {
	s := &counterSource{}
	mcpsource.RegisterMCPSource("rec-integration-sess", s)
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	entries := []config.MCPServerConfig{{
		Name:   "sbox",
		Source: "rec-integration-sess",
		Scope:  "session",
	}}

	for i := 0; i < 3; i++ {
		sessionID := fmt.Sprintf("s%d", i)
		require.NoError(t, mgr.OpenAll(
			context.Background(), mcpsource.ScopeSession, sessionID, nil, nil, entries))
		errs := mgr.CloseAll(mcpsource.ScopeSession, sessionID)
		require.Empty(t, errs)
	}

	assert.Equal(t, int32(3), s.opens.Load())
	assert.Equal(t, int32(3), s.closes.Load())
}

// TestScenarioVariables_NilAndEmpty verifies the helper handles the
// nil-scenario and empty-variables cases without allocation.
func TestScenarioVariables_NilAndEmpty(t *testing.T) {
	assert.Nil(t, scenarioVariables(nil))
	assert.Nil(t, scenarioVariables(&config.Scenario{}))
	assert.Nil(t, scenarioVariables(&config.Scenario{Variables: map[string]string{}}))
}

// TestScenarioVariables_CopiesVariables verifies the helper returns a
// copy of the scenario's Variables map (mutations don't leak back).
func TestScenarioVariables_CopiesVariables(t *testing.T) {
	sc := &config.Scenario{Variables: map[string]string{
		"repo":   "github.com/x/y",
		"branch": "main",
	}}
	got := scenarioVariables(sc)
	assert.Equal(t, "github.com/x/y", got["repo"])
	assert.Equal(t, "main", got["branch"])

	got["repo"] = "mutated"
	assert.Equal(t, "github.com/x/y", sc.Variables["repo"],
		"scenarioVariables should return a defensive copy")
}

// TestOpenScenarioSessionMCPSources_NoOpWhenUnconfigured verifies the
// helper is a no-op when either the scope manager or MCP config is empty.
func TestOpenScenarioSessionMCPSources_NoOpWhenUnconfigured(t *testing.T) {
	e := &Engine{}
	cleanup, err := e.openScenarioSessionMCPSources(context.Background(), nil, "scn", "run1")
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	cleanup() // must not panic

	// With a scope manager but no config entries, still a no-op.
	reg := mcp.NewRegistry()
	e = &Engine{mcpSourceScope: newMCPSourceScope(reg)}
	cleanup, err = e.openScenarioSessionMCPSources(context.Background(), nil, "scn", "run1")
	require.NoError(t, err)
	cleanup()
}

// TestOpenScenarioSessionMCPSources_OpensAndCloses verifies the helper
// opens scenario- and session-scoped entries for a configured source
// and that the cleanup closer tears both scopes down.
func TestOpenScenarioSessionMCPSources_OpensAndCloses(t *testing.T) {
	scn := &counterSource{}
	ses := &counterSource{}
	mcpsource.RegisterMCPSource("rec-exec-open-scn", scn)
	mcpsource.RegisterMCPSource("rec-exec-open-ses", ses)

	reg := mcp.NewRegistry()
	e := &Engine{
		mcpSourceScope: newMCPSourceScope(reg),
		mcpConfig: []config.MCPServerConfig{
			{Name: "a", Source: "rec-exec-open-scn", Scope: "scenario"},
			{Name: "b", Source: "rec-exec-open-ses", Scope: "session"},
		},
	}

	scenario := &config.Scenario{Variables: map[string]string{"k": "v"}}
	cleanup, err := e.openScenarioSessionMCPSources(
		context.Background(), scenario, "scenario-1", "run-xyz")
	require.NoError(t, err)
	assert.Contains(t, reg.ListServers(), "a")
	assert.Contains(t, reg.ListServers(), "b")

	cleanup()
	assert.Equal(t, int32(1), scn.closes.Load())
	assert.Equal(t, int32(1), ses.closes.Load())
	assert.NotContains(t, reg.ListServers(), "a")
	assert.NotContains(t, reg.ListServers(), "b")
}

// TestOpenScenarioSessionMCPSources_RollsBackScenarioOnSessionFailure
// verifies that if session-scope open fails after scenario-scope open
// succeeds, the scenario-scope entries are torn down before the helper
// returns (preventing leaks on the error path).
func TestOpenScenarioSessionMCPSources_RollsBackScenarioOnSessionFailure(t *testing.T) {
	scn := &counterSource{}
	mcpsource.RegisterMCPSource("rec-exec-rollback-scn", scn)

	reg := mcp.NewRegistry()
	e := &Engine{
		mcpSourceScope: newMCPSourceScope(reg),
		mcpConfig: []config.MCPServerConfig{
			{Name: "a", Source: "rec-exec-rollback-scn", Scope: "scenario"},
			// Session entry points at a non-existent source, forcing failure.
			{Name: "b", Source: "no-such-source-exec-rollback", Scope: "session"},
		},
	}

	cleanup, err := e.openScenarioSessionMCPSources(
		context.Background(), nil, "scenario-rb", "run-rb")
	require.Error(t, err)
	require.NotNil(t, cleanup) // always safe to call
	cleanup()                  // no-op on the error path

	// Scenario was opened once and closed during rollback; registry must be clean.
	assert.Equal(t, int32(1), scn.opens.Load())
	assert.Equal(t, int32(1), scn.closes.Load())
	assert.NotContains(t, reg.ListServers(), "a")
	assert.NotContains(t, reg.ListServers(), "b")
}

// TestScopeHooks_ScenarioScopeSharedAcrossRepetitions verifies that a
// source with scope=scenario is opened exactly once even when multiple
// session cycles run inside. Session opens for an unrelated scope must
// not trigger the scenario-scoped source.
func TestScopeHooks_ScenarioScopeSharedAcrossRepetitions(t *testing.T) {
	s := &counterSource{}
	mcpsource.RegisterMCPSource("rec-integration-scn", s)
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	entries := []config.MCPServerConfig{{
		Name:   "sbox",
		Source: "rec-integration-scn",
		Scope:  "scenario",
	}}

	// Scenario opens once; multiple session cycles inside.
	require.NoError(t, mgr.OpenAll(
		context.Background(), mcpsource.ScopeScenario, "scn", nil, nil, entries))
	for i := 0; i < 3; i++ {
		require.NoError(t, mgr.OpenAll(
			context.Background(), mcpsource.ScopeSession, "s", nil, nil, entries))
		mgr.CloseAll(mcpsource.ScopeSession, "s")
	}
	errs := mgr.CloseAll(mcpsource.ScopeScenario, "scn")
	require.Empty(t, errs)

	// Session opens are 0 because the entry's scope is "scenario".
	assert.Equal(t, int32(1), s.opens.Load())
	assert.Equal(t, int32(1), s.closes.Load())
}
