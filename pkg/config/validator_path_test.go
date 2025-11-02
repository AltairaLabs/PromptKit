package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidatorResolvesRelativePaths tests that the validator properly resolves
// file paths relative to the config file's directory
func TestValidatorResolvesRelativePaths(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create subdirectories
	promptsDir := filepath.Join(configDir, "prompts")
	providersDir := filepath.Join(configDir, "providers")
	scenariosDir := filepath.Join(configDir, "scenarios")
	if err := os.MkdirAll(promptsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(providersDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scenariosDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create the actual files
	promptFile := filepath.Join(promptsDir, "test-prompt.yaml")
	if err := os.WriteFile(promptFile, []byte("fragments: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	providerFile := filepath.Join(providersDir, "test-provider.yaml")
	providerContent := `id: test-provider
type: openai
model: gpt-4
pricing:
  input_cost_per_1k: 0.01
  output_cost_per_1k: 0.03
`
	if err := os.WriteFile(providerFile, []byte(providerContent), 0644); err != nil {
		t.Fatal(err)
	}

	scenarioFile := filepath.Join(scenariosDir, "test-scenario.yaml")
	scenarioContent := `id: test-scenario
task_type: test
description: Test scenario
turns: []
`
	if err := os.WriteFile(scenarioFile, []byte(scenarioContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config with relative paths
	configPath := filepath.Join(configDir, "arena.yaml")
	cfg := &Config{
		PromptConfigs: []PromptConfigRef{
			{
				ID:   "test",
				File: "prompts/test-prompt.yaml",
			},
		},
		Providers: []ProviderRef{
			{File: "providers/test-provider.yaml"},
		},
		Scenarios: []ScenarioRef{
			{File: "scenarios/test-scenario.yaml"},
		},
		Defaults: Defaults{
			ConfigDir: "",
		},
	}

	// Validate - should pass because files exist relative to config
	validator := NewConfigValidatorWithPath(cfg, configPath)
	err := validator.Validate()

	if err != nil {
		t.Errorf("Validation failed but should have passed: %v", err)
	}

	warnings := validator.GetWarnings()
	if len(warnings) > 0 {
		t.Logf("Got %d warnings (expected for missing personas): %v", len(warnings), warnings)
	}
}

// TestValidatorRejectsNonexistentRelativePaths tests that validator correctly
// identifies when relative paths don't resolve to real files
func TestValidatorRejectsNonexistentRelativePaths(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arena.yaml")

	cfg := &Config{
		PromptConfigs: []PromptConfigRef{
			{
				ID:   "test",
				File: "prompts/nonexistent.yaml",
			},
		},
		Providers: []ProviderRef{
			{File: "providers/nonexistent.yaml"},
		},
		Scenarios: []ScenarioRef{
			{File: "scenarios/nonexistent.yaml"},
		},
	}

	validator := NewConfigValidatorWithPath(cfg, configPath)
	err := validator.Validate()

	if err == nil {
		t.Error("Validation passed but should have failed for nonexistent files")
	}

	// Should have 3 errors (prompt, provider, scenario)
	if len(validator.errors) != 3 {
		t.Errorf("Expected 3 errors, got %d: %v", len(validator.errors), validator.errors)
	}
}

// TestValidatorAbsolutePaths tests that absolute paths work correctly
func TestValidatorAbsolutePaths(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with absolute path
	promptFile := filepath.Join(tmpDir, "test-prompt.yaml")
	if err := os.WriteFile(promptFile, []byte("fragments: {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(tmpDir, "arena.yaml")
	cfg := &Config{
		PromptConfigs: []PromptConfigRef{
			{
				ID:   "test",
				File: promptFile, // absolute path
			},
		},
		Defaults: Defaults{
			ConfigDir: "",
		},
	}

	validator := NewConfigValidatorWithPath(cfg, configPath)
	err := validator.Validate()

	if err != nil {
		t.Errorf("Validation failed for absolute path: %v", err)
	}
}

// TestValidatorRealWorldExample tests the actual customer-support example
func TestValidatorRealWorldExample(t *testing.T) {
	// This test uses the actual example config if it exists
	configPath := "../../examples/customer-support/arena.yaml"

	// Check if the example exists (skip if not)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skip("customer-support example not found, skipping test")
	}

	// Load the config
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Validate with path
	validator := NewConfigValidatorWithPath(cfg, configPath)
	err = validator.Validate()

	if err != nil {
		t.Errorf("Validation failed for customer-support example: %v", err)
		t.Logf("Errors: %v", validator.errors)
	}

	// Log warnings (personas are expected to be missing)
	if len(validator.GetWarnings()) > 0 {
		t.Logf("Warnings: %v", validator.GetWarnings())
	}
}
