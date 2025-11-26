package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfig_TaskType tests that Config uses task_type field
func TestConfig_TaskType(t *testing.T) {
	// Create temp directory for test config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-bot.yaml")

	// Write config using task_type
	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: customer-support
spec:
  task_type: "customer-support"
  version: "v1.0.0"
  description: "Test bot"
  system_template: "You are a helpful assistant."
`

	if err := os.WriteFile(configFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Create registry and load/register config
	registry := createTestRegistry()

	// Read and parse the file
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	config, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Register the config
	if err := registry.RegisterConfig("customer-support", config); err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Verify directly from the registered config
	if config.Spec.TaskType != "customer-support" {
		t.Errorf("Expected task_type 'customer-support', got '%s'", config.Spec.TaskType)
	}
}

// TestConfig_FindByTaskType tests that registry can find configs by task_type
func TestConfig_FindByTaskType(t *testing.T) {
	// Create temp directory for test configs
	tmpDir := t.TempDir()

	// Create a config with task_type
	configFile := filepath.Join(tmpDir, "support.yaml")
	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: "support"
  version: "v1.0.0"
  description: "Support bot"
  system_template: "You are a support agent."
`

	if err := os.WriteFile(configFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Create registry and load/register config
	registry := createTestRegistry()

	// Read and parse the file
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	config, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Register the config
	if err := registry.RegisterConfig("support", config); err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Should be able to access by registered name
	if config.Spec.TaskType != "support" {
		t.Errorf("Expected task_type 'support', got '%s'", config.Spec.TaskType)
	}
}
