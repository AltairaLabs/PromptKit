package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/agentkb"
	"github.com/AltairaLabs/PromptKit/tools/arena/templates"
)

func TestWriteAgentBrief_CreatesFiles(t *testing.T) {
	dir := t.TempDir()
	res := &templates.GenerationResult{ProjectPath: dir, Success: true}

	require.NoError(t, writeAgentBrief(res))

	skill := filepath.Join(dir, ".claude", "skills", "promptarena-authoring", "SKILL.md")
	assert.FileExists(t, skill)
	agents := filepath.Join(dir, "AGENTS.md")
	assert.FileExists(t, agents)

	assert.Contains(t, res.FilesCreated, filepath.Join(".claude", "skills", "promptarena-authoring", "SKILL.md"))
	assert.Contains(t, res.FilesCreated, "AGENTS.md")
}

func TestWriteAgentBrief_AppendsWhenAgentsExists(t *testing.T) {
	dir := t.TempDir()
	existing := "# My project\n\nhand-written notes\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o644))

	res := &templates.GenerationResult{ProjectPath: dir, Success: true}
	require.NoError(t, writeAgentBrief(res))

	got, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "hand-written notes", "must not clobber existing content")
	assert.Contains(t, string(got), "<!-- promptarena-authoring -->", "must append the brief")
}

func TestWriteAgentBrief_ErrorsWhenAgentsUnreadable(t *testing.T) {
	dir := t.TempDir()
	// A directory at the AGENTS.md path makes os.ReadFile fail with a non-NotExist
	// error, exercising the read-error branch.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755))

	res := &templates.GenerationResult{ProjectPath: dir, Success: true}
	require.Error(t, writeAgentBrief(res))
}

func TestWriteAgentBrief_IdempotentOnRerun(t *testing.T) {
	dir := t.TempDir()
	res := &templates.GenerationResult{ProjectPath: dir, Success: true}
	require.NoError(t, writeAgentBrief(res))
	first, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)

	res2 := &templates.GenerationResult{ProjectPath: dir, Success: true}
	require.NoError(t, writeAgentBrief(res2))
	second, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "re-run must not duplicate the brief")
	assert.NotContains(t, res2.FilesCreated, "AGENTS.md", "unchanged AGENTS.md not reported as created")
}

// TestWriteAgentBriefTo_BriefsWithoutScaffold covers the standalone `agent-brief`
// path: it installs AGENTS.md + the full skill into a target dir and scaffolds no
// sample kit (unlike `init`). The skill is richer than the AGENTS.md shim.
func TestWriteAgentBriefTo_BriefsWithoutScaffold(t *testing.T) {
	dir := t.TempDir()

	written, err := writeAgentBriefTo(dir)
	require.NoError(t, err)
	require.Contains(t, written, skillRelPath)
	require.Contains(t, written, "AGENTS.md")

	skillPath := filepath.Join(dir, ".claude", "skills", "promptarena-authoring", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	assert.Greater(t, len(skill), len(agentkb.AgentsBrief()),
		"installed skill must be richer than the AGENTS.md shim")

	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(agents), "<!-- promptarena-authoring -->")

	// No sample kit scaffolded (init would write config.arena.yaml).
	_, statErr := os.Stat(filepath.Join(dir, "config.arena.yaml"))
	assert.True(t, os.IsNotExist(statErr), "agent-brief must not scaffold a sample kit")

	// Command is wired with the expected signature.
	assert.Equal(t, "agent-brief [dir]", agentBriefCmd.Use)
}

// TestAgentBriefCmd_RunE exercises the command's RunE handler directly (cobra's
// Execute() would walk to root and re-parse), covering arg handling and output.
func TestAgentBriefCmd_RunE(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	agentBriefCmd.SetOut(&out)

	require.NoError(t, agentBriefCmd.RunE(agentBriefCmd, []string{dir}))

	assert.FileExists(t, filepath.Join(dir, ".claude", "skills", "promptarena-authoring", "SKILL.md"))
	assert.FileExists(t, filepath.Join(dir, "AGENTS.md"))
	assert.Contains(t, out.String(), "Agent briefed")
}
