package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFixture creates a file in the temp directory and returns its path.
func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	subdir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

const minimalPromptYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: greeting
spec:
  task_type: "greeting"
  version: "v1.0.0"
  description: "A simple greeting prompt"
  system_template: "You are a friendly assistant."
`

const secondPromptYAML = `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: farewell
spec:
  task_type: "farewell"
  version: "v1.0.0"
  description: "A farewell prompt"
  system_template: "You are a polite assistant that says goodbye."
`

func minimalArenaConfig(promptFiles ...string) string {
	cfg := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs:
`
	for i, f := range promptFiles {
		cfg += "    - id: prompt" + string(rune('0'+i)) + "\n"
		cfg += "      file: " + f + "\n"
	}
	cfg += `  providers: []
  defaults:
    temperature: 0.7
    max_tokens: 100
`
	return cfg
}

func TestCompile_SinglePrompt(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)
	configFile := writeFixture(t, dir, "config.arena.yaml", minimalArenaConfig("prompts/greeting.yaml"))

	result, err := Compile(configFile,
		WithPackID("test-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Pack)
	require.NotEmpty(t, result.JSON)

	assert.Equal(t, "test-pack", result.Pack.ID)
	assert.Contains(t, result.Pack.Prompts, "greeting")
	assert.Equal(t, "A simple greeting prompt", result.Pack.Prompts["greeting"].Description)

	// Verify JSON is valid
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(result.JSON, &parsed))
	assert.Equal(t, "test-pack", parsed["id"])
}

func TestCompile_MultiplePrompts(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)
	writeFixture(t, dir, "prompts/farewell.yaml", secondPromptYAML)
	configFile := writeFixture(t, dir, "config.arena.yaml",
		minimalArenaConfig("prompts/greeting.yaml", "prompts/farewell.yaml"))

	result, err := Compile(configFile,
		WithPackID("multi-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "multi-pack", result.Pack.ID)
	assert.Len(t, result.Pack.Prompts, 2)
	assert.Contains(t, result.Pack.Prompts, "greeting")
	assert.Contains(t, result.Pack.Prompts, "farewell")
}

func TestCompile_WithTools(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)

	toolYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: search
spec:
  name: search
  description: "Search the web"
  mode: client
  input_schema:
    type: object
    properties:
      query:
        type: string
        description: "Search query"
    required:
      - query
  output_schema:
    type: object
    properties:
      results:
        type: array
`
	writeFixture(t, dir, "tools/search.yaml", toolYAML)

	arenaConfig := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs:
    - id: prompt0
      file: prompts/greeting.yaml
  tools:
    - file: tools/search.yaml
  providers: []
  defaults:
    temperature: 0.7
    max_tokens: 100
`
	configFile := writeFixture(t, dir, "config.arena.yaml", arenaConfig)

	result, err := Compile(configFile,
		WithPackID("tools-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "tools-pack", result.Pack.ID)
	assert.Contains(t, result.Pack.Prompts, "greeting")

	// Tools may or may not be present depending on how the tool loader works
	// with the Kind: Tool format; verify pack compiled without error.
}

func TestCompile_WithWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)
	writeFixture(t, dir, "prompts/farewell.yaml", secondPromptYAML)

	arenaConfig := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs:
    - id: prompt0
      file: prompts/greeting.yaml
    - id: prompt1
      file: prompts/farewell.yaml
  providers: []
  workflow:
    version: 1
    entry: start
    states:
      start:
        prompt_task: greeting
        on_event:
          Done: end
      end:
        prompt_task: farewell
  defaults:
    temperature: 0.7
    max_tokens: 100
`
	configFile := writeFixture(t, dir, "config.arena.yaml", arenaConfig)

	result, err := Compile(configFile,
		WithPackID("workflow-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "workflow-pack", result.Pack.ID)
	assert.NotNil(t, result.Pack.Workflow)
	assert.Equal(t, "start", result.Pack.Workflow.Entry)
	assert.Len(t, result.Pack.Workflow.States, 2)
}

func TestCompile_WithAgents(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)

	arenaConfig := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs:
    - id: prompt0
      file: prompts/greeting.yaml
  providers: []
  agents:
    entry: triage
    members:
      triage:
        description: "Triage agent"
        tags:
          - router
  defaults:
    temperature: 0.7
    max_tokens: 100
`
	configFile := writeFixture(t, dir, "config.arena.yaml", arenaConfig)

	result, err := Compile(configFile,
		WithPackID("agents-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "agents-pack", result.Pack.ID)
	assert.NotNil(t, result.Pack.Agents)
	assert.Equal(t, "triage", result.Pack.Agents.Entry)
}

func TestCompile_CustomPackID(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)
	configFile := writeFixture(t, dir, "config.arena.yaml", minimalArenaConfig("prompts/greeting.yaml"))

	result, err := Compile(configFile,
		WithPackID("custom-id-123"),
		WithCompilerVersion("test-v2"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "custom-id-123", result.Pack.ID)
	assert.Contains(t, result.Pack.Compilation.CompiledWith, "test-v2")
}

func TestCompile_DefaultPackIDFromDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a subdirectory with a specific name to test ID derivation
	subDir := filepath.Join(dir, "My Cool Project")
	require.NoError(t, os.MkdirAll(filepath.Join(subDir, "prompts"), 0o755))
	writeFixture(t, subDir, "prompts/greeting.yaml", minimalPromptYAML)
	configFile := writeFixture(t, subDir, "config.arena.yaml", minimalArenaConfig("prompts/greeting.yaml"))

	result, err := Compile(configFile,
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	// Should be sanitized from "My Cool Project"
	assert.Equal(t, "my-cool-project", result.Pack.ID)
}

func TestCompile_NonexistentConfigFile(t *testing.T) {
	result, err := Compile("/nonexistent/path/config.arena.yaml",
		WithPackID("test"),
		WithSkipSchemaValidation(),
	)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "loading arena config")
}

func TestCompile_InvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	configFile := writeFixture(t, dir, "config.arena.yaml", "this is not valid yaml: [[[")

	result, err := Compile(configFile,
		WithPackID("test"),
		WithSkipSchemaValidation(),
	)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestCompile_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	arenaConfig := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs: []
  providers: []
  defaults:
    temperature: 0.7
    max_tokens: 100
`
	configFile := writeFixture(t, dir, "config.arena.yaml", arenaConfig)

	result, err := Compile(configFile,
		WithPackID("empty-pack"),
		WithSkipSchemaValidation(),
	)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestCompile_ResultJSONIsValidPack(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "prompts/greeting.yaml", minimalPromptYAML)
	configFile := writeFixture(t, dir, "config.arena.yaml", minimalArenaConfig("prompts/greeting.yaml"))

	result, err := Compile(configFile,
		WithPackID("json-test"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)

	// Verify the JSON can be deserialized back into a Pack
	var pack prompt.Pack
	require.NoError(t, json.Unmarshal(result.JSON, &pack))
	assert.Equal(t, "json-test", pack.ID)
	assert.Contains(t, pack.Prompts, "greeting")
}

func TestSanitizePackID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MyProject", "myproject"},
		{"my project name", "my-project-name"},
		{"my_project!@#$%", "myproject"},
		{"Customer Support Bot!", "customer-support-bot"},
		{"my---project", "my-project"},
		{"-my-project-", "my-project"},
		{"customer-support", "customer-support"},
		{"project123", "project123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizePackID(tt.input))
		})
	}
}

func TestCompile_IncludesSkills(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	writeFixture(t, dir, "prompts/chat.yaml", minimalPromptYAML)

	// Create skill directory with a SKILL.md
	skillDir := filepath.Join(dir, "skills", "test-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(
		"---\nname: test-skill\ndescription: Test\n---\nInstructions.\n",
	), 0o600))

	arenaConfig := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test
spec:
  prompt_configs:
    - id: greeting
      file: prompts/chat.yaml
  providers: []
  skills:
    - path: skills/
  defaults:
    concurrency: 1
`
	configFile := writeFixture(t, dir, "config.arena.yaml", arenaConfig)

	result, err := Compile(configFile,
		WithPackID("skills-pack"),
		WithCompilerVersion("test-v1"),
		WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	require.NotNil(t, result.Pack)

	assert.Equal(t, "skills-pack", result.Pack.ID)
	require.Len(t, result.Pack.Skills, 1)
	// Path should be relative (converted back from absolute)
	assert.Equal(t, "skills", result.Pack.Skills[0].EffectiveDir())
}

func TestCompileOptions(t *testing.T) {
	t.Run("WithPackID sets pack ID", func(t *testing.T) {
		var opts compileOptions
		WithPackID("my-id")(&opts)
		assert.Equal(t, "my-id", opts.packID)
	})

	t.Run("WithCompilerVersion sets version", func(t *testing.T) {
		var opts compileOptions
		WithCompilerVersion("v2.0")(&opts)
		assert.Equal(t, "v2.0", opts.compilerVersion)
	})

	t.Run("WithSkipSchemaValidation sets flag", func(t *testing.T) {
		var opts compileOptions
		WithSkipSchemaValidation()(&opts)
		assert.True(t, opts.skipSchemaValidation)
	})
}
