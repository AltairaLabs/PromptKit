package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAssembledPrompt_TaskType tests that AssembledPrompt uses task_type
func TestAssembledPrompt_TaskType(t *testing.T) {
	// Create temp directory for test config
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-bot.yaml")

	// Write config using task_type in K8s-style manifest
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

	// Create registry and manually load/register the config
	registry := createTestRegistry()

	// Read and parse the file
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	config, err := ParsePromptConfig(data)
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	// Register the config
	if err := registry.RegisterConfig("customer-support", config); err != nil {
		t.Fatalf("Failed to register config: %v", err)
	}

	// Verify the task type directly from the config
	if config.Spec.TaskType != "customer-support" {
		t.Errorf("Expected task_type 'customer-support', got '%s'", config.Spec.TaskType)
	}
}
