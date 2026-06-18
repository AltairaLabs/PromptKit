package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
