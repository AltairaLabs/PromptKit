package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exportPromptYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: greeting
spec:
  task_type: "greeting"
  version: "v1.0.0"
  description: "A simple greeting prompt"
  system_template: "You are a friendly assistant."
`

func exportArenaConfig(promptFile string) string {
	return `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: export-test
spec:
  prompt_configs:
    - id: prompt0
      file: ` + promptFile + `
  providers: []
  defaults:
    temperature: 0.7
    max_tokens: 100
`
}

func setupExportFixtures(t *testing.T) (dir string, configFile string) {
	t.Helper()
	dir = t.TempDir()

	promptDir := filepath.Join(dir, "prompts")
	require.NoError(t, os.MkdirAll(promptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, "greeting.yaml"), []byte(exportPromptYAML), 0o644))

	configFile = filepath.Join(dir, "config.arena.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(exportArenaConfig("prompts/greeting.yaml")), 0o644))
	return dir, configFile
}

func TestRunExport_ValidConfig(t *testing.T) {
	_, configFile := setupExportFixtures(t)

	exportConfig = configFile
	exportOutput = ""
	exportID = "test-export"

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runExport(exportCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, runErr)

	var buf [64 * 1024]byte
	n, _ := r.Read(buf[:])
	output := buf[:n]

	// Verify output is valid JSON
	assert.True(t, json.Valid(output), "output should be valid JSON")

	// Verify it contains the pack ID
	var pack map[string]interface{}
	require.NoError(t, json.Unmarshal(output, &pack))
	assert.Equal(t, "test-export", pack["id"])
}

func TestRunExport_NonexistentConfig(t *testing.T) {
	exportConfig = "/nonexistent/path/config.arena.yaml"
	exportOutput = ""
	exportID = ""

	err := runExport(exportCmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "export failed")
}

func TestRunExport_CustomID(t *testing.T) {
	_, configFile := setupExportFixtures(t)

	exportConfig = configFile
	exportOutput = ""
	exportID = "custom-pack-id"

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	runErr := runExport(exportCmd, nil)

	w.Close()
	os.Stdout = oldStdout

	require.NoError(t, runErr)

	var buf [64 * 1024]byte
	n, _ := r.Read(buf[:])

	var pack map[string]interface{}
	require.NoError(t, json.Unmarshal(buf[:n], &pack))
	assert.Equal(t, "custom-pack-id", pack["id"])
}

func TestRunExport_OutputFile(t *testing.T) {
	dir, configFile := setupExportFixtures(t)

	outputFile := filepath.Join(dir, "output.json")
	exportConfig = configFile
	exportOutput = outputFile
	exportID = "file-export"

	err := runExport(exportCmd, nil)
	require.NoError(t, err)

	// Verify the output file was created
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	assert.True(t, json.Valid(data), "output file should contain valid JSON")

	var pack map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &pack))
	assert.Equal(t, "file-export", pack["id"])
}
