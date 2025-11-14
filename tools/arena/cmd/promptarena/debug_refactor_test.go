package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetConfigFileFromCmd(t *testing.T) {
	tests := []struct {
		name        string
		flagValue   string
		expectError bool
	}{
		{
			name:        "valid file path",
			flagValue:   "test.yaml",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("config", "", "config file")
			_ = cmd.Flags().Set("config", tt.flagValue)

			configFile, err := getConfigFile(cmd)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, configFile)
		})
	}
}

func TestPrintDebugHeaderOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printDebugHeader("test-config.yaml")

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Debug Mode")
	assert.Contains(t, output, "test-config.yaml")
}

func TestPrintConfigOverviewOutput(t *testing.T) {
	cfg := &config.Config{
		PromptConfigs: []config.PromptConfigRef{{ID: "test", File: "test.yaml"}},
		Providers:     []config.ProviderRef{{File: "provider1.yaml"}},
		Scenarios:     []config.ScenarioRef{{File: "scenario1.yaml"}},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   1024,
			Seed:        42,
			Concurrency: 4,
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printConfigOverview(cfg)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Configuration Overview")
	assert.Contains(t, output, "Providers:")
	assert.Contains(t, output, "Scenarios:")
	assert.Contains(t, output, "Temperature:")
	assert.Contains(t, output, "Max Tokens:")
}

func TestPrintScenariosOutput(t *testing.T) {
	tests := []struct {
		name      string
		scenarios map[string]*config.Scenario
	}{
		{
			name:      "empty scenarios",
			scenarios: map[string]*config.Scenario{},
		},
		{
			name: "single scenario",
			scenarios: map[string]*config.Scenario{
				"test1": {
					ID:          "test1",
					Description: "Test scenario 1",
					TaskType:    "predict",
					Turns: []config.TurnDefinition{
						{Role: "user", Content: "Hello"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			printScenarios(tt.scenarios)

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, err := buf.ReadFrom(r)
			require.NoError(t, err)

			output := buf.String()

			if len(tt.scenarios) > 0 {
				assert.Contains(t, output, "Scenarios")
				for id := range tt.scenarios {
					assert.Contains(t, output, id)
				}
			}
		})
	}
}

func TestPrintScenarioDetailsOutput(t *testing.T) {
	scenario := config.Scenario{
		ID:          "test-scenario",
		Description: "A test scenario",
		TaskType:    "predict",
		Turns: []config.TurnDefinition{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there"},
		},
		Constraints: map[string]interface{}{
			"max_length": 100,
			"required":   true,
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printScenarioDetails("test-scenario", scenario)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-scenario")
	assert.Contains(t, output, "Task Type:")
	assert.Contains(t, output, "Description:")
	assert.Contains(t, output, "Turns:")
	assert.Contains(t, output, "Constraints:")
}

func TestPrintProvidersOutput(t *testing.T) {
	tests := []struct {
		name      string
		providers map[string]*config.Provider
	}{
		{
			name:      "empty providers",
			providers: map[string]*config.Provider{},
		},
		{
			name: "single provider",
			providers: map[string]*config.Provider{
				"openai": {
					ID:    "openai",
					Type:  "openai",
					Model: "gpt-4",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			printProviders(tt.providers)

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			_, err := buf.ReadFrom(r)
			require.NoError(t, err)

			output := buf.String()

			if len(tt.providers) > 0 {
				assert.Contains(t, output, "Providers")
				for id := range tt.providers {
					assert.Contains(t, output, id)
				}
			}
		})
	}
}

func TestPrintProviderDetailsOutput(t *testing.T) {
	provider := config.Provider{
		ID:      "test-provider",
		Type:    "openai",
		Model:   "gpt-4",
		BaseURL: "https://api.openai.com/v1",
		Defaults: config.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   2048,
		},
		RateLimit: config.RateLimit{
			RPS:   10,
			Burst: 20,
		},
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printProviderDetails("test-provider", provider)

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-provider")
	assert.Contains(t, output, "Type:")
	assert.Contains(t, output, "Model:")
	assert.Contains(t, output, "Base URL:")
	assert.Contains(t, output, "Rate Limit:")
	assert.Contains(t, output, "Defaults:")
}

func TestRunDebug_WithValidConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arena.yaml")

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  providers:
    - file: provider1.yaml
  scenarios:
    - file: scenario1.yaml
  defaults:
    temperature: 0.7
    max_tokens: 1024
    seed: 42
    concurrency: 4
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Create dummy provider file
	providerPath := filepath.Join(tmpDir, "provider1.yaml")
	providerContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Provider
metadata:
  name: test-provider
spec:
  id: test-provider
  type: openai
  model: gpt-4
  base_url: https://api.openai.com/v1
  rate_limit:
    rps: 10
    burst: 20
  defaults:
    temperature: 0.7
    top_p: 1.0
    max_tokens: 2048
  pricing:
    input_cost_per_1k: 0.03
    output_cost_per_1k: 0.06
`
	err = os.WriteFile(providerPath, []byte(providerContent), 0644)
	require.NoError(t, err)

	// Create dummy scenario file
	scenarioPath := filepath.Join(tmpDir, "scenario1.yaml")
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  id: test-scenario
  task_type: predict
  description: A test scenario
  turns:
    - role: user
      content: Hello
`
	err = os.WriteFile(scenarioPath, []byte(scenarioContent), 0644)
	require.NoError(t, err)

	// Create command with config flag
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	_ = cmd.Flags().Set("config", configPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run debug
	err = runDebug(cmd)

	w.Close()
	os.Stdout = oldStdout

	// Check no error
	require.NoError(t, err)

	// Read captured output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()

	// Verify output contains expected sections
	assert.Contains(t, output, "Debug Mode")
	assert.Contains(t, output, configPath)
	assert.Contains(t, output, "Configuration Overview")
	assert.Contains(t, output, "Scenarios")
	assert.Contains(t, output, "test-scenario")
	assert.Contains(t, output, "Providers")
	assert.Contains(t, output, "test-provider")
	assert.Contains(t, output, "Debug complete!")
}

func TestRunDebug_InvalidConfigFile(t *testing.T) {
	// Create command with non-existent config file
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	_ = cmd.Flags().Set("config", "/nonexistent/path/config.yaml")

	// Run debug - should return error
	err := runDebug(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunDebug_InvalidConfigContent(t *testing.T) {
	// Create a temporary config file with invalid content
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	invalidContent := `this is not valid yaml content: [[[`
	err := os.WriteFile(configPath, []byte(invalidContent), 0644)
	require.NoError(t, err)

	// Create command with config flag
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	_ = cmd.Flags().Set("config", configPath)

	// Run debug - should return error
	err = runDebug(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunDebug_DirectoryPath(t *testing.T) {
	// Create a temporary directory with arena.yaml
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arena.yaml")

	configContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Arena
metadata:
  name: test-arena
spec:
  providers: []
  scenarios: []
  defaults:
    temperature: 0.7
    max_tokens: 1024
    seed: 42
    concurrency: 4
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Create command with directory path (not file path)
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "config file")
	_ = cmd.Flags().Set("config", tmpDir) // Pass directory, not file

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run debug - should automatically find arena.yaml in directory
	err = runDebug(cmd)

	w.Close()
	os.Stdout = oldStdout

	// Check no error
	require.NoError(t, err)

	// Read captured output
	var buf bytes.Buffer
	_, err = buf.ReadFrom(r)
	require.NoError(t, err)

	output := buf.String()

	// Verify output contains expected sections
	assert.Contains(t, output, "Debug Mode")
	assert.Contains(t, output, "Configuration Overview")
	assert.Contains(t, output, "Debug complete!")
}
