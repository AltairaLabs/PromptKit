package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// helper to write a minimal arena config with given spec YAML fragment
func writeArenaConfig(t *testing.T, dir, specFragment string) string {
	t.Helper()
	configPath := filepath.Join(dir, "arena.yaml")
	content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
` + specFragment
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0600))
	return configPath
}

// helper to write a provider file and return its filename
func writeProviderFile(t *testing.T, dir, name string) string {
	t.Helper()
	filename := name + ".yaml"
	content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: ` + name + `
spec:
  id: ` + name + `
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0600))
	return filename
}

func TestInlineProviderSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    inline-openai:
      type: openai
      model: gpt-4o
      capabilities:
        - text
        - streaming
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	p, ok := cfg.LoadedProviders["inline-openai"]
	require.True(t, ok, "inline provider should be in LoadedProviders")
	assert.Equal(t, "inline-openai", p.ID)
	assert.Equal(t, "openai", p.Type)
	assert.Equal(t, "gpt-4o", p.Model)
	assert.Equal(t, "default", cfg.ProviderGroups["inline-openai"])
	assert.Equal(t, []string{"text", "streaming"}, cfg.ProviderCapabilities["inline-openai"])
}

func TestInlineScenarioSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	// Need a provider (inline) since providers are required
	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    p1:
      type: openai
      model: gpt-4
  scenario_specs:
    greeting:
      task_type: chat
      description: "Greeting scenario"
      turns:
        - role: user
          content: "Hello"
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	s, ok := cfg.LoadedScenarios["greeting"]
	require.True(t, ok)
	assert.Equal(t, "greeting", s.ID)
	assert.Equal(t, "chat", s.TaskType)
	assert.Equal(t, "Greeting scenario", s.Description)
	require.Len(t, s.Turns, 1)
}

func TestInlineEvalSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	// Create a dummy recording file
	recPath := filepath.Join(dir, "rec.json")
	require.NoError(t, os.WriteFile(recPath, []byte(`{}`), 0600))

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    p1:
      type: openai
      model: gpt-4
  eval_specs:
    eval1:
      description: "Test eval"
      recording:
        path: rec.json
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	e, ok := cfg.LoadedEvals["eval1"]
	require.True(t, ok)
	assert.Equal(t, "eval1", e.ID)
	assert.Equal(t, "Test eval", e.Description)
}

func TestInlineToolSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    p1:
      type: openai
      model: gpt-4
  tool_specs:
    lookup_order:
      description: "Look up an order"
      mode: mock
      input_schema:
        type: object
        properties:
          order_id:
            type: string
      output_schema:
        type: object
      mock_result:
        status: delivered
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	require.NotEmpty(t, cfg.LoadedTools)
	found := false
	for _, td := range cfg.LoadedTools {
		if td.FilePath == "<inline:lookup_order>" {
			found = true
			assert.NotEmpty(t, td.Data)
		}
	}
	assert.True(t, found, "expected inline tool data entry")
}

func TestInlineJudgeSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    judge-provider:
      type: openai
      model: gpt-4o
  judge_specs:
    quality:
      provider: judge-provider
      model: gpt-4o-mini
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	j, ok := cfg.LoadedJudges["quality"]
	require.True(t, ok)
	assert.Equal(t, "quality", j.Name)
	assert.Equal(t, "gpt-4o-mini", j.Model)
	assert.NotNil(t, j.Provider)
	assert.Equal(t, "judge-provider", j.Provider.ID)
}

func TestInlineJudgeSpecs_FallbackToProviderModel(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    jp:
      type: openai
      model: gpt-4o
  judge_specs:
    j1:
      provider: jp
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	j, ok := cfg.LoadedJudges["j1"]
	require.True(t, ok)
	assert.Equal(t, "gpt-4o", j.Model, "should fall back to provider model")
}

func TestInlinePromptSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	configPath := writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    p1:
      type: openai
      model: gpt-4
  prompt_specs:
    chat:
      task_type: chat
      version: "1.0"
      description: "Chat prompt"
      system_template: "You are a helpful assistant."
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	pc, ok := cfg.LoadedPromptConfigs["chat"]
	require.True(t, ok)
	assert.Equal(t, "<inline:chat>", pc.FilePath)
	assert.Equal(t, "chat", pc.TaskType)
	pCfg, ok := pc.Config.(*prompt.Config)
	require.True(t, ok)
	assert.Equal(t, "You are a helpful assistant.", pCfg.Spec.SystemTemplate)
}

func TestInlinePersonaSpecs(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	// Need a provider file for self-play role reference
	provFile := writeProviderFile(t, dir, "sp-provider")

	configPath := writeArenaConfig(t, dir, `  providers:
    - file: `+provFile+`
  self_play:
    enabled: true
    personas: []
    persona_specs:
      frustrated-user:
        description: "A frustrated customer"
        system_prompt: "You are very frustrated."
        goals:
          - "Get help"
        constraints:
          - "Be rude"
    roles:
      - id: user-role
        provider: sp-provider
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	p, ok := cfg.LoadedPersonas["frustrated-user"]
	require.True(t, ok)
	assert.Equal(t, "frustrated-user", p.ID)
	assert.Equal(t, "A frustrated customer", p.Description)
}

func TestInlineSpecs_ConflictDetection(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")

	tests := []struct {
		name     string
		setup    func(t *testing.T, dir string) string
		errMatch string
	}{
		{
			name: "provider conflict",
			setup: func(t *testing.T, dir string) string {
				t.Helper()
				writeProviderFile(t, dir, "dup-provider")
				return writeArenaConfig(t, dir, `  providers:
    - file: dup-provider.yaml
  provider_specs:
    dup-provider:
      type: openai
      model: gpt-4
  defaults:
    concurrency: 1
`)
			},
			errMatch: `provider "dup-provider" defined in both provider_specs and providers file refs`,
		},
		{
			name: "judge conflict",
			setup: func(t *testing.T, dir string) string {
				t.Helper()
				provFile := writeProviderFile(t, dir, "jprov")
				return writeArenaConfig(t, dir, `  providers:
    - file: `+provFile+`
  judges:
    - name: j1
      provider: jprov
  judge_specs:
    j1:
      provider: jprov
  defaults:
    concurrency: 1
`)
			},
			errMatch: `judge "j1" defined in both judge_specs and judges refs`,
		},
		{
			name: "judge references unknown provider",
			setup: func(t *testing.T, dir string) string {
				t.Helper()
				return writeArenaConfig(t, dir, `  providers: []
  provider_specs:
    p1:
      type: openai
      model: gpt-4
  judge_specs:
    j1:
      provider: nonexistent
  defaults:
    concurrency: 1
`)
			},
			errMatch: `judge_specs "j1" references unknown provider "nonexistent"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := tc.setup(t, dir)
			_, err := LoadConfig(configPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errMatch)
		})
	}
}

func TestInlineSpecs_MixedMode(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	dir := t.TempDir()

	// Create a file-ref provider
	provFile := writeProviderFile(t, dir, "file-provider")

	configPath := writeArenaConfig(t, dir, `  providers:
    - file: `+provFile+`
  provider_specs:
    inline-provider:
      type: anthropic
      model: claude-3
  defaults:
    concurrency: 1
`)

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	// Both providers should be present
	_, ok := cfg.LoadedProviders["file-provider"]
	assert.True(t, ok, "file-ref provider should be loaded")
	_, ok = cfg.LoadedProviders["inline-provider"]
	assert.True(t, ok, "inline provider should be loaded")
	assert.Len(t, cfg.LoadedProviders, 2)
}

func TestMergeProviderSpecs_Unit(t *testing.T) {
	cfg := &Config{
		LoadedProviders:      map[string]*Provider{},
		ProviderGroups:       map[string]string{},
		ProviderCapabilities: map[string][]string{},
		ProviderSpecs: map[string]*Provider{
			"p1": {Type: "openai", Model: "gpt-4", Capabilities: []string{"text"}},
			"p2": {Type: "anthropic", Model: "claude-3"},
		},
	}

	require.NoError(t, cfg.mergeProviderSpecs())
	assert.Len(t, cfg.LoadedProviders, 2)
	assert.Equal(t, "p1", cfg.LoadedProviders["p1"].ID)
	assert.Equal(t, "default", cfg.ProviderGroups["p1"])
	assert.Equal(t, []string{"text"}, cfg.ProviderCapabilities["p1"])
	// p2 has no capabilities, so no entry
	_, hasCaps := cfg.ProviderCapabilities["p2"]
	assert.False(t, hasCaps)
}

func TestMergeScenarioSpecs_Unit(t *testing.T) {
	cfg := &Config{
		LoadedScenarios: map[string]*Scenario{},
		ScenarioSpecs: map[string]*Scenario{
			"s1": {TaskType: "chat", Description: "desc"},
		},
	}

	require.NoError(t, cfg.mergeScenarioSpecs())
	assert.Equal(t, "s1", cfg.LoadedScenarios["s1"].ID)
}

func TestMergeEvalSpecs_Unit(t *testing.T) {
	cfg := &Config{
		LoadedEvals: map[string]*Eval{},
		EvalSpecs: map[string]*Eval{
			"e1": {Description: "test eval"},
		},
	}

	require.NoError(t, cfg.mergeEvalSpecs())
	assert.Equal(t, "e1", cfg.LoadedEvals["e1"].ID)
}

func TestMergeToolSpecs_Unit(t *testing.T) {
	cfg := &Config{
		LoadedTools: []ToolData{},
		ToolSpecs: map[string]*ToolSpec{
			"my-tool": {Description: "A tool", Mode: "mock"},
		},
	}

	require.NoError(t, cfg.mergeToolSpecs())
	require.Len(t, cfg.LoadedTools, 1)
	assert.Equal(t, "<inline:my-tool>", cfg.LoadedTools[0].FilePath)
	assert.Contains(t, string(cfg.LoadedTools[0].Data), "my-tool")
}

func TestMergeJudgeSpecs_Unit(t *testing.T) {
	provider := &Provider{ID: "jp", Type: "openai", Model: "gpt-4"}
	cfg := &Config{
		LoadedProviders: map[string]*Provider{"jp": provider},
		LoadedJudges:    map[string]*JudgeTarget{},
		JudgeSpecs: map[string]*JudgeSpec{
			"j1": {Provider: "jp", Model: "gpt-4o-mini"},
		},
	}

	require.NoError(t, cfg.mergeJudgeSpecs())
	j := cfg.LoadedJudges["j1"]
	require.NotNil(t, j)
	assert.Equal(t, "gpt-4o-mini", j.Model)
	assert.Equal(t, provider, j.Provider)
}

func TestMergePromptSpecs_Unit(t *testing.T) {
	cfg := &Config{
		LoadedPromptConfigs: map[string]*PromptConfigData{},
		PromptSpecs: map[string]*prompt.Spec{
			"chat": {TaskType: "chat", SystemTemplate: "Hello"},
		},
	}

	require.NoError(t, cfg.mergePromptSpecs())
	pc := cfg.LoadedPromptConfigs["chat"]
	require.NotNil(t, pc)
	assert.Equal(t, "chat", pc.TaskType)
	assert.Equal(t, "<inline:chat>", pc.FilePath)
}

func TestMergeScenarioSpecs_Conflict(t *testing.T) {
	cfg := &Config{
		LoadedScenarios: map[string]*Scenario{"s1": {ID: "s1"}},
		ScenarioSpecs:   map[string]*Scenario{"s1": {Description: "dup"}},
	}

	err := cfg.mergeScenarioSpecs()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scenario_specs and scenarios file refs")
}

func TestMergeEvalSpecs_Conflict(t *testing.T) {
	cfg := &Config{
		LoadedEvals: map[string]*Eval{"e1": {ID: "e1"}},
		EvalSpecs:   map[string]*Eval{"e1": {Description: "dup"}},
	}

	err := cfg.mergeEvalSpecs()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "eval_specs and evals file refs")
}

func TestMergePromptSpecs_Conflict(t *testing.T) {
	cfg := &Config{
		LoadedPromptConfigs: map[string]*PromptConfigData{"chat": {}},
		PromptSpecs:         map[string]*prompt.Spec{"chat": {TaskType: "chat"}},
	}

	err := cfg.mergePromptSpecs()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompt_specs and prompt_configs file refs")
}
