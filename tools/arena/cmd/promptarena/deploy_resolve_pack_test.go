package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withDeployPackGlobals saves and restores the package-level flags that
// resolvePackFile reads, so tests can set them in isolation.
func withDeployPackGlobals(t *testing.T, cfgPath, packFile string) {
	t.Helper()
	oldCfg, oldPack := deployConfig, deployPackFile
	t.Cleanup(func() {
		deployConfig = oldCfg
		deployPackFile = oldPack
	})
	deployConfig = cfgPath
	deployPackFile = packFile
}

func TestResolvePackFile_ExplicitPackOverride(t *testing.T) {
	dir := t.TempDir()
	packPath := filepath.Join(dir, "my.pack.json")
	want := []byte(`{"id":"override-pack"}`)
	require.NoError(t, os.WriteFile(packPath, want, 0o600))

	// Point --config at a nonexistent file to prove the override path does not
	// touch the compiler when --pack is provided.
	withDeployPackGlobals(t, filepath.Join(dir, "does-not-exist.yaml"), packPath)

	got, err := resolvePackFile()
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestResolvePackFile_CompilesFromConfig(t *testing.T) {
	// Place the config in a directory whose name yields a schema-valid pack ID
	// (the compiler derives the pack ID from the parent dir name).
	dir := filepath.Join(t.TempDir(), "deploy-pack")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "prompts"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "prompts", "greeting.yaml"), []byte(exportPromptYAML), 0o644))
	configFile := filepath.Join(dir, "config.arena.yaml")
	require.NoError(t, os.WriteFile(
		configFile, []byte(exportArenaConfig("prompts/greeting.yaml")), 0o644))

	withDeployPackGlobals(t, configFile, "")

	got, err := resolvePackFile()
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.True(t, json.Valid(got), "compiled pack should be valid JSON")

	var pack map[string]interface{}
	require.NoError(t, json.Unmarshal(got, &pack))
	assert.Contains(t, pack, "id", "compiled output should be a pack with an id")
}

func TestResolvePackFile_MissingPackFileErrors(t *testing.T) {
	dir := t.TempDir()
	withDeployPackGlobals(t, "arena.yaml", filepath.Join(dir, "missing.pack.json"))

	_, err := resolvePackFile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read pack file")
}

func TestResolvePackFile_CompileFailureIsClear(t *testing.T) {
	withDeployPackGlobals(t, "/nonexistent/path/config.arena.yaml", "")

	_, err := resolvePackFile()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to compile pack")
	assert.NotContains(t, err.Error(), "packc compile",
		"error should not reference the removed packc compile prerequisite")
}
