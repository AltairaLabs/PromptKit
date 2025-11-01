package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
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
  description: Test scenario
  turns:
    - user: "Hello"
`
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	if err := os.WriteFile(scenarioPath, []byte(scenarioContent), 0644); err != nil {
		t.Fatalf("Failed to write test scenario: %v", err)
	}

	// Create provider file
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test-provider
spec:
  id: provider1
  type: openai
  model: gpt-4
  base_url: https://api.openai.com/v1
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 100
  pricing:
    input_cost_per_1k: 0.01
    output_cost_per_1k: 0.02
`
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	if err := os.WriteFile(providerPath, []byte(providerContent), 0644); err != nil {
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
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
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
  task_type: support
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
  name: test-provider
spec:
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

	if provider.ID != "test-provider" {
		t.Errorf("Expected ID 'test-provider', got '%s'", provider.ID)
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
