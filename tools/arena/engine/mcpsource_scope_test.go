package engine

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/tools/arena/mcpsource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingSource struct {
	opens    atomic.Int32
	closes   atomic.Int32
	lastArgs map[string]any
	fail     error
}

func (s *recordingSource) Open(_ context.Context, args map[string]any) (mcpsource.MCPConn, io.Closer, error) {
	if s.fail != nil {
		return mcpsource.MCPConn{}, nil, s.fail
	}
	s.opens.Add(1)
	s.lastArgs = args
	return mcpsource.MCPConn{
			URL:     "http://fake",
			Headers: map[string]string{"X-Args": "ok"},
		},
		closerFunc(func() error { s.closes.Add(1); return nil }),
		nil
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestMCPSourceScope_OpenAndClose(t *testing.T) {
	mcpsource.RegisterMCPSource("rec-scope-basic", &recordingSource{})
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	cfg := config.MCPServerConfig{
		Name:       "x",
		Source:     "rec-scope-basic",
		Scope:      "scenario",
		SourceArgs: map[string]any{"image": "foo"},
	}
	require.NoError(t, mgr.OpenAll(
		context.Background(), mcpsource.ScopeScenario, "scn1", nil, nil,
		[]config.MCPServerConfig{cfg}))

	assert.Contains(t, reg.ListServers(), "x")

	errs := mgr.CloseAll(mcpsource.ScopeScenario, "scn1")
	require.Empty(t, errs)
	assert.NotContains(t, reg.ListServers(), "x")
}

func TestMCPSourceScope_UnknownSource(t *testing.T) {
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	cfg := config.MCPServerConfig{Name: "x", Source: "no-such-source-registered", Scope: "session"}
	err := mgr.OpenAll(
		context.Background(), mcpsource.ScopeSession, "s1", nil, nil,
		[]config.MCPServerConfig{cfg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-source-registered")
}

func TestMCPSourceScope_PartialOpenRollsBack(t *testing.T) {
	good := &recordingSource{}
	bad := &recordingSource{fail: errors.New("boom")}
	mcpsource.RegisterMCPSource("rec-scope-good", good)
	mcpsource.RegisterMCPSource("rec-scope-bad", bad)

	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	entries := []config.MCPServerConfig{
		{Name: "a", Source: "rec-scope-good", Scope: "session"},
		{Name: "b", Source: "rec-scope-bad", Scope: "session"},
	}
	err := mgr.OpenAll(
		context.Background(), mcpsource.ScopeSession, "rb-s1", nil, nil, entries)
	require.Error(t, err)
	assert.Equal(t, int32(1), good.opens.Load())
	assert.Equal(t, int32(1), good.closes.Load(), "good source closer should have run during rollback")
	assert.NotContains(t, reg.ListServers(), "a")
	assert.NotContains(t, reg.ListServers(), "b")
}

func TestMCPSourceScope_CloseNotOpenedNoop(t *testing.T) {
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)
	errs := mgr.CloseAll(mcpsource.ScopeRun, "never-opened")
	assert.Empty(t, errs)
}

func TestMCPSourceScope_OnlyOpensMatchingScope(t *testing.T) {
	s := &recordingSource{}
	mcpsource.RegisterMCPSource("rec-scope-filter", s)
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	entries := []config.MCPServerConfig{
		{Name: "ses", Source: "rec-scope-filter", Scope: "session"},
		{Name: "scn", Source: "rec-scope-filter", Scope: "scenario"},
	}
	require.NoError(t, mgr.OpenAll(
		context.Background(), mcpsource.ScopeSession, "s1", nil, nil, entries))
	assert.Equal(t, int32(1), s.opens.Load())
	assert.Contains(t, reg.ListServers(), "ses")
	assert.NotContains(t, reg.ListServers(), "scn")
}

// --- Args templating (Task 5) ---

func TestMCPSourceScope_ExpandsScenarioTemplates(t *testing.T) {
	s := &recordingSource{}
	mcpsource.RegisterMCPSource("rec-scope-tmpl", s)
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	cfg := config.MCPServerConfig{
		Name:   "x",
		Source: "rec-scope-tmpl",
		Scope:  "session",
		SourceArgs: map[string]any{
			"repo":   "{{scenario.repo}}",
			"branch": "{{scenario.branch}}",
			"nested": map[string]any{"key": "{{scenario.repo}}"},
		},
	}
	vars := map[string]string{"repo": "github.com/x/y", "branch": "main"}

	require.NoError(t, mgr.OpenAll(
		context.Background(), mcpsource.ScopeSession, "tmpl-s1", vars, nil,
		[]config.MCPServerConfig{cfg}))

	require.Equal(t, "github.com/x/y", s.lastArgs["repo"])
	require.Equal(t, "main", s.lastArgs["branch"])
	nested, ok := s.lastArgs["nested"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "github.com/x/y", nested["key"])
}

func TestExpandArgs_NonStringValuesPassThrough(t *testing.T) {
	in := map[string]any{
		"a": 42,
		"b": true,
		"c": []any{"{{scenario.x}}", "lit"},
	}
	out := expandArgs(in, map[string]string{"x": "sub"})
	assert.Equal(t, 42, out["a"])
	assert.Equal(t, true, out["b"])
	assert.Equal(t, []any{"sub", "lit"}, out["c"])
}

// --- Skill staging (Task 9) ---

func TestBuildSkillMounts_FromSkillSources(t *testing.T) {
	sources := []prompt.SkillSourceConfig{
		{Name: "codegen", Path: "/abs/host/codegen"},
		{Name: "review", Dir: "/abs/host/review"},
	}
	mounts := buildSkillMounts(sources)
	require.Len(t, mounts, 2)

	assert.Equal(t, "/abs/host/codegen", mounts[0]["source"])
	assert.Equal(t, "/skills/codegen", mounts[0]["target"])
	assert.Equal(t, true, mounts[0]["readonly"])

	// EffectiveDir() prefers Dir over Path, so for the second entry
	// source should be the Dir value.
	assert.Equal(t, "/abs/host/review", mounts[1]["source"])
	assert.Equal(t, "/skills/review", mounts[1]["target"])
	assert.Equal(t, true, mounts[1]["readonly"])
}

func TestBuildSkillMounts_EmptyReturnsNil(t *testing.T) {
	assert.Nil(t, buildSkillMounts(nil))
	assert.Nil(t, buildSkillMounts([]prompt.SkillSourceConfig{}))
}

func TestInjectMountsIntoArgs_PreservesExistingArgs(t *testing.T) {
	entry := &config.MCPServerConfig{
		Name:       "sandbox",
		Source:     "docker",
		Scope:      "session",
		SourceArgs: map[string]any{"image": "foo"},
	}
	mounts := []map[string]any{{"source": "/a", "target": "/b", "readonly": true}}

	injectMountsIntoArgs(entry, mounts)

	assert.Equal(t, "foo", entry.SourceArgs["image"])
	assert.Equal(t, mounts, entry.SourceArgs["mounts"])
}

func TestInjectMountsIntoArgs_NoMountsIsNoop(t *testing.T) {
	entry := &config.MCPServerConfig{
		SourceArgs: map[string]any{"image": "foo"},
	}
	injectMountsIntoArgs(entry, nil)
	_, hasMounts := entry.SourceArgs["mounts"]
	assert.False(t, hasMounts, "empty mounts should not add the key")
}

func TestMCPSourceScope_InjectsMountsFromSkills(t *testing.T) {
	s := &recordingSource{}
	mcpsource.RegisterMCPSource("rec-scope-mounts", s)
	reg := mcp.NewRegistry()
	mgr := newMCPSourceScope(reg)

	skills := []prompt.SkillSourceConfig{
		{Name: "codegen", Path: "/host/path/codegen"},
	}

	cfg := config.MCPServerConfig{
		Name:       "sbox",
		Source:     "rec-scope-mounts",
		Scope:      "session",
		SourceArgs: map[string]any{"image": "x"},
	}
	require.NoError(t, mgr.OpenAll(
		context.Background(), mcpsource.ScopeSession, "mnt-s1", nil, skills,
		[]config.MCPServerConfig{cfg}))

	require.Equal(t, "x", s.lastArgs["image"])
	mounts, ok := s.lastArgs["mounts"].([]map[string]any)
	require.True(t, ok, "mounts should be []map[string]any")
	require.Len(t, mounts, 1)
	assert.Equal(t, "/host/path/codegen", mounts[0]["source"])
	assert.Equal(t, "/skills/codegen", mounts[0]["target"])
	assert.Equal(t, true, mounts[0]["readonly"])
}
