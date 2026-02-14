package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
  base_url: https://api.openai.com/v1
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
  pricing:
    input_cost_per_1k: 0.03
    output_cost_per_1k: 0.06
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
    concurrency: 4

  scenarios:
    - file: scenario1.yaml

  providers:
    - file: provider1.yaml

  judges:
    - name: test-judge
      provider: provider1
      model: gpt-4o-mini
  judge_defaults:
    prompt: judge-simple-criteria
    prompt_registry: ./prompts
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Config is nil")
	}

	if !config.Defaults.Verbose {
		t.Error("Expected verbose to be true")
	}

	if config.Defaults.Concurrency != 4 {
		t.Errorf("Expected concurrency 4, got %d", config.Defaults.Concurrency)
	}

	if len(config.Scenarios) != 1 {
		t.Errorf("Expected 1 scenario, got %d", len(config.Scenarios))
	}

	if len(config.Providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(config.Providers))
	}

	if len(config.Judges) != 1 {
		t.Errorf("Expected 1 judge, got %d", len(config.Judges))
	}
	if config.JudgeDefaults == nil || config.JudgeDefaults.Prompt != "judge-simple-criteria" {
		t.Errorf("Expected judge defaults to be set")
	}
	if len(config.LoadedJudges) != 1 {
		t.Errorf("Expected 1 loaded judge, got %d", len(config.LoadedJudges))
	} else {
		j := config.LoadedJudges["test-judge"]
		if j == nil || j.Model != "gpt-4o-mini" {
			t.Errorf("Expected loaded judge to resolve model override, got %#v", j)
		}
	}

	// Verify loaded resources
	if len(config.LoadedScenarios) != 1 {
		t.Errorf("Expected 1 loaded scenario, got %d", len(config.LoadedScenarios))
	}

	if len(config.LoadedProviders) != 1 {
		t.Errorf("Expected 1 loaded provider, got %d", len(config.LoadedProviders))
	}
}

func TestLoadConfig_InvalidFile(t *testing.T) {
	_, err := LoadConfig("nonexistent.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	invalidContent := "this is not: valid: yaml:"
	err := os.WriteFile(configPath, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestLoadScenario(t *testing.T) {
	tmpDir := t.TempDir()
	scenarioPath := filepath.Join(tmpDir, "test-scenario.yaml")

	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: test-scenario
  task_type: support
  description: Test scenario for loading
  turns:
    - role: user
      content: "Hello"
`

	err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}

	if scenario == nil {
		t.Fatal("Scenario is nil")
	}

	if scenario.ID != "test-scenario" {
		t.Errorf("Expected ID 'test-scenario', got '%s'", scenario.ID)
	}

	if scenario.TaskType != "support" {
		t.Errorf("Expected task_type 'support', got '%s'", scenario.TaskType)
	}

	if len(scenario.Turns) != 1 {
		t.Errorf("Expected 1 turn, got %d", len(scenario.Turns))
	}
}

func TestLoadProvider(t *testing.T) {
	tmpDir := t.TempDir()
	providerPath := filepath.Join(tmpDir, "test-provider.yaml")

	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    max_tokens: 1000
    top_p: 1.0
  pricing:
    input_cost_per_1k: 0.03
    output_cost_per_1k: 0.06
`

	err := os.WriteFile(providerPath, []byte(providerContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	provider, err := LoadProvider(providerPath)
	if err != nil {
		t.Fatalf("LoadProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("Provider is nil")
	}

	if provider.ID != "provider1" {
		t.Errorf("Expected ID 'provider1', got '%s'", provider.ID)
	}

	if provider.Type != "openai" {
		t.Errorf("Expected type 'openai', got '%s'", provider.Type)
	}

	if provider.Model != "gpt-4" {
		t.Errorf("Expected model 'gpt-4', got '%s'", provider.Model)
	}

	if provider.Defaults.Temperature != 0.7 {
		t.Errorf("Expected temperature 0.7, got %f", provider.Defaults.Temperature)
	}

	if provider.Defaults.MaxTokens != 1000 {
		t.Errorf("Expected max_tokens 1000, got %d", provider.Defaults.MaxTokens)
	}

	if provider.Pricing.InputCostPer1K != 0.03 {
		t.Errorf("Expected input cost 0.03, got %f", provider.Pricing.InputCostPer1K)
	}

	if provider.Pricing.OutputCostPer1K != 0.06 {
		t.Errorf("Expected output cost 0.06, got %f", provider.Pricing.OutputCostPer1K)
	}
}

func TestResolveFilePath(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		relPath    string
		want       string
	}{
		{
			name:       "Relative path",
			configPath: "/path/to/config.yaml",
			relPath:    "scenario.yaml",
			want:       "/path/to/scenario.yaml",
		},
		{
			name:       "Absolute path",
			configPath: "/path/to/config.yaml",
			relPath:    "/absolute/scenario.yaml",
			want:       "/absolute/scenario.yaml",
		},
		{
			name:       "Path with subdirectory",
			configPath: "/path/to/config.yaml",
			relPath:    "scenarios/test.yaml",
			want:       "/path/to/scenarios/test.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveFilePath(tt.configPath, tt.relPath)
			if got != tt.want {
				t.Errorf("ResolveFilePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{}

	// Check zero values are as expected
	if cfg.Defaults.Verbose {
		t.Error("Expected verbose to be false by default")
	}

	if cfg.Defaults.Concurrency != 0 {
		t.Errorf("Expected concurrency 0 by default, got %d", cfg.Defaults.Concurrency)
	}

	// Test setting values
	cfg.Defaults.Verbose = true
	cfg.Defaults.Concurrency = 10

	if !cfg.Defaults.Verbose {
		t.Error("Failed to set verbose to true")
	}

	if cfg.Defaults.Concurrency != 10 {
		t.Errorf("Expected concurrency 10, got %d", cfg.Defaults.Concurrency)
	}
}

func TestLoadConfig_WithSelfPlay(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
  base_url: https://api.openai.com/v1
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
  pricing:
    input_cost_per_1k: 0.03
    output_cost_per_1k: 0.06
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	// Create persona file
	personaContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Persona
metadata:
  name: test-persona
spec:
  description: Test persona
  system_prompt: "You are a helpful assistant"
  goals:
    - Be helpful
  style:
    verbosity: medium
    challenge_level: high
`
	personaPath := filepath.Join(tmpDir, "persona1.yaml")
	if err := os.WriteFile(personaPath, []byte(personaContent), 0600); err != nil {
		t.Fatalf("Failed to write test persona: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
    concurrency: 4

  scenarios:
    - file: scenario1.yaml

  providers:
    - file: provider1.yaml

  self_play:
    enabled: true
    personas:
      - file: persona1.yaml
    roles:
      - id: role1
        provider: provider1
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Config is nil")
	}

	// Check self-play configuration
	if config.SelfPlay == nil {
		t.Fatal("SelfPlay config is nil")
	}

	if !config.SelfPlay.Enabled {
		t.Error("Expected self-play to be enabled")
	}

	if len(config.SelfPlay.Personas) != 1 {
		t.Errorf("Expected 1 persona, got %d", len(config.SelfPlay.Personas))
	}

	if len(config.SelfPlay.Roles) != 1 {
		t.Errorf("Expected 1 role, got %d", len(config.SelfPlay.Roles))
	}

	if config.SelfPlay.Roles[0].ID != "role1" {
		t.Errorf("Expected role ID 'role1', got '%s'", config.SelfPlay.Roles[0].ID)
	}

	if config.SelfPlay.Roles[0].Provider != "provider1" {
		t.Errorf("Expected role provider 'provider1', got '%s'", config.SelfPlay.Roles[0].Provider)
	}

	// Check loaded personas
	if len(config.LoadedPersonas) != 1 {
		t.Errorf("Expected 1 loaded persona, got %d", len(config.LoadedPersonas))
	}

	persona, exists := config.LoadedPersonas["test-persona"]
	if !exists {
		t.Fatal("Expected persona 'test-persona' to be loaded")
	}

	if persona.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("Expected persona system prompt 'You are a helpful assistant', got '%s'", persona.SystemPrompt)
	}
}

func TestLoadConfig_LoadPromptConfigsError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create config that references a nonexistent prompt file
	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  prompt_configs:
    - id: test
      file: nonexistent.yaml
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error when prompt file doesn't exist")
	}
}

func TestLoadConfig_LoadToolsError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create config that references a nonexistent tool file
	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  tools:
    - file: nonexistent.yaml
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error when tool file doesn't exist")
	}
}

func TestLoadConfigWithEvals(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create eval file
	evalContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: test-eval
spec:
  id: eval1
  description: Test evaluation
  recording:
    path: test-recording.json
    type: session
  turns:
    - all_turns:
        assertions:
          - type: content_includes
            params:
              patterns: ["test"]
  conversation_assertions:
    - type: llm_judge_conversation
      params:
        judge: default
        criteria: "Test criteria"
  tags:
    - test
  mode: instant
`
	evalPath := filepath.Join(tmpDir, "eval1.yaml")
	if err := os.WriteFile(evalPath, []byte(evalContent), 0600); err != nil {
		t.Fatalf("Failed to write test eval: %v", err)
	}

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true

  evals:
    - file: eval1.yaml

  providers:
    - file: provider1.yaml
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Config is nil")
	}

	if len(config.LoadedEvals) != 1 {
		t.Errorf("Expected 1 loaded eval, got %d", len(config.LoadedEvals))
	}

	eval, ok := config.LoadedEvals["test-eval"]
	if !ok {
		t.Fatal("Expected eval 'test-eval' to be loaded")
	}

	if eval.ID != "test-eval" {
		t.Errorf("Expected eval ID 'test-eval', got %q", eval.ID)
	}

	if eval.Description != "Test evaluation" {
		t.Errorf("Expected description 'Test evaluation', got %q", eval.Description)
	}

	if eval.Recording.Path == "" {
		t.Error("Expected recording path to be set")
	}
	if !filepath.IsAbs(eval.Recording.Path) {
		t.Errorf("Expected absolute recording path, got %q", eval.Recording.Path)
	}
	if filepath.Base(eval.Recording.Path) != "test-recording.json" {
		t.Errorf("Expected recording filename 'test-recording.json', got %q", filepath.Base(eval.Recording.Path))
	}

	if eval.Mode != "instant" {
		t.Errorf("Expected mode 'instant', got %q", eval.Mode)
	}

	if len(eval.Tags) != 1 || eval.Tags[0] != "test" {
		t.Errorf("Expected tags ['test'], got %v", eval.Tags)
	}
}

func TestLoadEval(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()

	evalContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Eval
metadata:
  name: direct-eval
spec:
  id: direct-eval-id
  description: Direct eval test
  recording:
    path: session.recording.json
  turns:
    - all_turns:
        assertions:
          - type: content_includes
            params:
              patterns: ["test"]
`
	evalPath := filepath.Join(tmpDir, "direct-eval.yaml")
	if err := os.WriteFile(evalPath, []byte(evalContent), 0600); err != nil {
		t.Fatalf("Failed to write test eval: %v", err)
	}

	eval, err := LoadEval(evalPath)
	if err != nil {
		t.Fatalf("LoadEval failed: %v", err)
	}

	if eval.ID != "direct-eval" {
		t.Errorf("Expected ID 'direct-eval', got %q", eval.ID)
	}

	if eval.Description != "Direct eval test" {
		t.Errorf("Expected description 'Direct eval test', got %q", eval.Description)
	}

	if eval.Recording.Path == "" {
		t.Error("Expected recording path to be set")
	}
	if !filepath.IsAbs(eval.Recording.Path) {
		t.Errorf("Expected absolute recording path, got %q", eval.Recording.Path)
	}
	if filepath.Base(eval.Recording.Path) != "session.recording.json" {
		t.Errorf("Expected recording filename 'session.recording.json', got %q", filepath.Base(eval.Recording.Path))
	}
}

func TestLoadEval_InvalidFile(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	_, err := LoadEval("/nonexistent/eval.yaml")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestLoadEval_InvalidYAML(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	evalPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(evalPath, []byte("invalid: yaml: content:"), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadEval(evalPath)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}

func TestLoadConfigWithEvalsError(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	// Reference nonexistent eval file
	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
  evals:
    - file: nonexistent-eval.yaml
  providers:
    - file: provider1.yaml
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error when eval file doesn't exist")
	}
}

func TestLoadConfig_WithTools(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider1
spec:
  id: provider1
  type: openai
  model: gpt-4
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	// Create tool file
	toolContent := `{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get weather information",
    "parameters": {
      "type": "object",
      "properties": {
        "location": {
          "type": "string",
          "description": "The city name"
        }
      },
      "required": ["location"]
    }
  }
}`
	toolPath := filepath.Join(tmpDir, "weather-tool.json")
	if err := os.WriteFile(toolPath, []byte(toolContent), 0600); err != nil {
		t.Fatalf("Failed to write test tool: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
  scenarios:
    - file: scenario1.yaml
  providers:
    - file: provider1.yaml
  tools:
    - file: weather-tool.json
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(config.LoadedTools) != 1 {
		t.Errorf("Expected 1 loaded tool, got %d", len(config.LoadedTools))
	}

	if config.LoadedTools[0].FilePath != "weather-tool.json" {
		t.Errorf("Expected tool file path 'weather-tool.json', got %s", config.LoadedTools[0].FilePath)
	}
}

func TestLoadConfig_WithToolsError(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Reference nonexistent tool file
	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
  scenarios:
    - file: scenario1.yaml
  tools:
    - file: nonexistent-tool.json
`

	err := os.WriteFile(configPath, []byte(configContent), 0600)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = LoadConfig(configPath)
	if err == nil {
		t.Error("Expected error when tool file doesn't exist")
	}
}

func TestLoadProvider_Error(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()

	// Try to load a nonexistent provider file
	_, err := LoadProvider(filepath.Join(tmpDir, "nonexistent.yaml"))
	if err == nil {
		t.Error("Expected error when loading nonexistent provider file")
	}
}

func TestLoadProvider_InvalidYAML(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	providerPath := filepath.Join(tmpDir, "bad-provider.yaml")

	// Create invalid YAML content
	invalidContent := `invalid: yaml: content: [[[`
	if err := os.WriteFile(providerPath, []byte(invalidContent), 0600); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	_, err := LoadProvider(providerPath)
	if err == nil {
		t.Error("Expected error when loading invalid YAML")
	}
}

// ============================================================================
// Capability Tests
// ============================================================================

func TestLoadProvider_WithCapabilities(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	providerPath := filepath.Join(tmpDir, "provider-with-caps.yaml")

	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test-provider
spec:
  id: test-provider
  type: openai
  model: gpt-4o
  capabilities:
    - text
    - streaming
    - vision
    - tools
    - json
  defaults:
    temperature: 0.7
    max_tokens: 1000
    top_p: 1.0
`

	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	provider, err := LoadProvider(providerPath)
	if err != nil {
		t.Fatalf("LoadProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("Provider is nil")
	}

	if len(provider.Capabilities) != 5 {
		t.Errorf("Expected 5 capabilities, got %d", len(provider.Capabilities))
	}

	expectedCaps := []string{"text", "streaming", "vision", "tools", "json"}
	for i, cap := range expectedCaps {
		if provider.Capabilities[i] != cap {
			t.Errorf("Expected capability[%d] = %q, got %q", i, cap, provider.Capabilities[i])
		}
	}
}

func TestLoadProvider_WithoutCapabilities(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	providerPath := filepath.Join(tmpDir, "provider-no-caps.yaml")

	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test-provider
spec:
  id: test-provider
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    max_tokens: 1000
    top_p: 1.0
`

	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	provider, err := LoadProvider(providerPath)
	if err != nil {
		t.Fatalf("LoadProvider failed: %v", err)
	}

	if provider == nil {
		t.Fatal("Provider is nil")
	}

	if len(provider.Capabilities) != 0 {
		t.Errorf("Expected 0 capabilities, got %d", len(provider.Capabilities))
	}
}

func TestLoadScenario_WithRequiredCapabilities(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	scenarioPath := filepath.Join(tmpDir, "scenario-with-caps.yaml")

	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: vision-test
spec:
  id: vision-test
  task_type: test
  description: Test vision capability
  required_capabilities:
    - vision
    - streaming
  turns:
    - role: user
      content: "Describe this image"
`

	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}

	if scenario == nil {
		t.Fatal("Scenario is nil")
	}

	if len(scenario.RequiredCapabilities) != 2 {
		t.Errorf("Expected 2 required capabilities, got %d", len(scenario.RequiredCapabilities))
	}

	expectedCaps := []string{"vision", "streaming"}
	for i, cap := range expectedCaps {
		if scenario.RequiredCapabilities[i] != cap {
			t.Errorf("Expected required_capabilities[%d] = %q, got %q", i, cap, scenario.RequiredCapabilities[i])
		}
	}
}

func TestLoadScenario_WithoutRequiredCapabilities(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	scenarioPath := filepath.Join(tmpDir, "scenario-no-caps.yaml")

	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: basic-test
spec:
  id: basic-test
  task_type: test
  description: Basic test scenario
  turns:
    - role: user
      content: "Hello"
`

	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}

	if scenario == nil {
		t.Fatal("Scenario is nil")
	}

	if len(scenario.RequiredCapabilities) != 0 {
		t.Errorf("Expected 0 required capabilities, got %d", len(scenario.RequiredCapabilities))
	}
}

func TestLoadConfig_ProviderCapabilitiesPopulated(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  required_capabilities:
    - vision
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider with capabilities
	provider1Content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider-vision
spec:
  id: provider-vision
  type: openai
  model: gpt-4o
  capabilities:
    - text
    - streaming
    - vision
    - tools
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
`
	provider1Path := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(provider1Path, []byte(provider1Content), 0600); err != nil {
		t.Fatalf("Failed to write test provider1: %v", err)
	}

	// Create provider without vision capability
	provider2Content := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: provider-text-only
spec:
  id: provider-text-only
  type: openai
  model: gpt-4
  capabilities:
    - text
    - streaming
    - tools
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
`
	provider2Path := filepath.Join(tmpDir, "provider2.yaml")
	if err := os.WriteFile(provider2Path, []byte(provider2Content), 0600); err != nil {
		t.Fatalf("Failed to write test provider2: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
    concurrency: 4

  scenarios:
    - file: scenario1.yaml

  providers:
    - file: provider1.yaml
    - file: provider2.yaml
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Config is nil")
	}

	// Verify ProviderCapabilities map is populated
	if config.ProviderCapabilities == nil {
		t.Fatal("ProviderCapabilities map is nil")
	}

	if len(config.ProviderCapabilities) != 2 {
		t.Errorf("Expected 2 providers in ProviderCapabilities, got %d", len(config.ProviderCapabilities))
	}

	// Check provider-vision capabilities
	visionCaps, exists := config.ProviderCapabilities["provider-vision"]
	if !exists {
		t.Error("Expected provider-vision in ProviderCapabilities")
	} else {
		if len(visionCaps) != 4 {
			t.Errorf("Expected 4 capabilities for provider-vision, got %d", len(visionCaps))
		}
		hasVision := false
		for _, cap := range visionCaps {
			if cap == "vision" {
				hasVision = true
				break
			}
		}
		if !hasVision {
			t.Error("Expected provider-vision to have 'vision' capability")
		}
	}

	// Check provider-text-only capabilities
	textCaps, exists := config.ProviderCapabilities["provider-text-only"]
	if !exists {
		t.Error("Expected provider-text-only in ProviderCapabilities")
	} else {
		if len(textCaps) != 3 {
			t.Errorf("Expected 3 capabilities for provider-text-only, got %d", len(textCaps))
		}
		hasVision := false
		for _, cap := range textCaps {
			if cap == "vision" {
				hasVision = true
				break
			}
		}
		if hasVision {
			t.Error("Expected provider-text-only NOT to have 'vision' capability")
		}
	}

	// Verify loaded scenario has required capabilities
	scenario, exists := config.LoadedScenarios["test-scenario"]
	if !exists {
		t.Fatal("Expected scenario 'test-scenario' to be loaded")
	}
	if len(scenario.RequiredCapabilities) != 1 {
		t.Errorf("Expected 1 required capability, got %d", len(scenario.RequiredCapabilities))
	}
	if scenario.RequiredCapabilities[0] != "vision" {
		t.Errorf("Expected required capability 'vision', got %q", scenario.RequiredCapabilities[0])
	}
}

func TestLoadConfig_ProviderWithoutCapabilities_EmptyInMap(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.yaml")

	// Create scenario file
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: scenario1
  task_type: test
  description: Test scenario
  turns:
    - role: user
      content: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider WITHOUT capabilities
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: legacy-provider
spec:
  id: legacy-provider
  type: openai
  model: gpt-4
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 1000
`
	providerPath := filepath.Join(tmpDir, "provider.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  defaults:
    verbose: true
  scenarios:
    - file: scenario1.yaml
  providers:
    - file: provider.yaml
`

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// ProviderCapabilities map should be initialized but not contain the provider
	// since it has no capabilities
	if config.ProviderCapabilities == nil {
		t.Fatal("ProviderCapabilities map should be initialized")
	}

	_, exists := config.ProviderCapabilities["legacy-provider"]
	if exists {
		t.Error("Provider without capabilities should not be in ProviderCapabilities map")
	}
}

func TestLoadScenario_MultipleCapabilityCombination(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	scenarioPath := filepath.Join(tmpDir, "combo-scenario.yaml")

	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: combo-test
spec:
  id: combo-test
  task_type: test
  description: Test multiple capability combination
  required_capabilities:
    - streaming
    - tools
    - vision
  streaming: true
  turns:
    - role: user
      content: "Look at this image and call a tool"
`

	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0600); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	scenario, err := LoadScenario(scenarioPath)
	if err != nil {
		t.Fatalf("LoadScenario failed: %v", err)
	}

	if len(scenario.RequiredCapabilities) != 3 {
		t.Errorf("Expected 3 required capabilities, got %d", len(scenario.RequiredCapabilities))
	}

	expectedCaps := map[string]bool{"streaming": true, "tools": true, "vision": true}
	for _, cap := range scenario.RequiredCapabilities {
		if !expectedCaps[cap] {
			t.Errorf("Unexpected capability: %s", cap)
		}
		delete(expectedCaps, cap)
	}
	if len(expectedCaps) > 0 {
		t.Errorf("Missing expected capabilities: %v", expectedCaps)
	}

	if !scenario.Streaming {
		t.Error("Expected streaming to be true")
	}
}

func TestLoadProvider_AllCapabilityTypes(t *testing.T) {
	t.Setenv("PROMPTKIT_SCHEMA_SOURCE", "local")
	tmpDir := t.TempDir()
	providerPath := filepath.Join(tmpDir, "full-caps-provider.yaml")

	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: full-caps
spec:
  id: full-caps
  type: gemini
  model: gemini-2.5-pro
  capabilities:
    - text
    - streaming
    - vision
    - tools
    - json
    - audio
    - video
    - documents
  defaults:
    temperature: 0.7
    max_tokens: 1000
    top_p: 1.0
`

	if err := os.WriteFile(providerPath, []byte(providerContent), 0600); err != nil {
		t.Fatalf("Failed to write test provider: %v", err)
	}

	provider, err := LoadProvider(providerPath)
	if err != nil {
		t.Fatalf("LoadProvider failed: %v", err)
	}

	if len(provider.Capabilities) != 8 {
		t.Errorf("Expected 8 capabilities, got %d", len(provider.Capabilities))
	}

	expectedCaps := map[string]bool{
		"text":      true,
		"streaming": true,
		"vision":    true,
		"tools":     true,
		"json":      true,
		"audio":     true,
		"video":     true,
		"documents": true,
	}

	for _, cap := range provider.Capabilities {
		if !expectedCaps[cap] {
			t.Errorf("Unexpected capability: %s", cap)
		}
		delete(expectedCaps, cap)
	}
	if len(expectedCaps) > 0 {
		t.Errorf("Missing expected capabilities: %v", expectedCaps)
	}
}

func TestLoadPackFile_Success(t *testing.T) {
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packContent := `{"$schema":"https://example.com","id":"test-pack","name":"Test Pack","version":"1.0.0","prompts":{}}`
	if err := os.WriteFile(packPath, []byte(packContent), 0600); err != nil {
		t.Fatalf("Failed to write test pack: %v", err)
	}

	cfg := &Config{PackFile: "test.pack.json", ConfigDir: tmpDir}
	configPath := filepath.Join(tmpDir, "config.yaml")
	err := cfg.loadPackFile(configPath)
	if err != nil {
		t.Fatalf("loadPackFile failed: %v", err)
	}
	if cfg.LoadedPack == nil {
		t.Fatal("Expected LoadedPack to be set")
	}
	if cfg.LoadedPack.ID != "test-pack" {
		t.Errorf("Expected pack ID 'test-pack', got %q", cfg.LoadedPack.ID)
	}
}

func TestLoadPackFile_NotFound(t *testing.T) {
	cfg := &Config{PackFile: "nonexistent.pack.json", ConfigDir: t.TempDir()}
	configPath := filepath.Join(cfg.ConfigDir, "config.yaml")
	err := cfg.loadPackFile(configPath)
	if err == nil {
		t.Fatal("Expected error for nonexistent pack file")
	}
}
