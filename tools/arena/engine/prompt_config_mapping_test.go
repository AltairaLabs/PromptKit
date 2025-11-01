package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

// TestPromptConfigMapping_IDIsArbitrary tests that prompt config ID is just a reference identifier,
// not required to match task_type in the prompt file
func TestPromptConfigMapping_IDIsArbitrary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a prompt file with task_type="support" using K8s-style manifest
	promptFile := filepath.Join(tmpDir, "customer-support.yaml")
	promptContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: "support"
  version: "v1.0.0"
  description: "Customer support bot"
  system_template: "You are a helpful support agent."
`
	if err := os.WriteFile(promptFile, []byte(promptContent), 0644); err != nil {
		t.Fatalf("Failed to write prompt file: %v", err)
	}

	// Create scenario with task_type="support"
	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  task_type: "support"
  description: "Test Scenario"
  turns:
    - role: user
      content: "Hello"
`
	if err := os.WriteFile(scenarioFile, []byte(scenarioContent), 0644); err != nil {
		t.Fatalf("Failed to write scenario file: %v", err)
	}

	// Create config with arbitrary ID that doesn't match task_type
	cfg := &config.Config{
		PromptConfigs: []config.PromptConfigRef{
			{
				ID:   "my-custom-id", // Arbitrary ID, doesn't need to match task_type
				File: "customer-support.yaml",
			},
		},
		Scenarios: []config.ScenarioRef{
			{File: "scenario.yaml"},
		},
		Providers: []config.ProviderRef{},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   1500,
		},
	}

	// Create engine and load resources
	engine := newTestEngine(t, tmpDir, cfg)

	// Verify prompt registry was initialized with the file mapping
	if engine.promptRegistry == nil {
		t.Fatal("Expected prompt registry to be initialized")
	}

	// The prompt should be loadable by its task_type="support", not by ID="my-custom-id"
	prompt := engine.promptRegistry.LoadWithVars("support", map[string]string{}, "")
	if prompt == nil {
		t.Error("Expected to load prompt by task_type='support'")
	}

	// Loading by the arbitrary ID should fail (since ID is not task_type)
	promptByID := engine.promptRegistry.LoadWithVars("my-custom-id", map[string]string{}, "")
	if promptByID != nil {
		t.Error("Should not be able to load prompt by arbitrary ID 'my-custom-id'")
	}
}

// TestPromptConfigMapping_MissingTaskType tests that execution fails with clear error
// when scenario references a task_type that doesn't exist in any prompt config
func TestPromptConfigMapping_MissingTaskType(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a prompt file with task_type="support" using K8s-style manifest
	promptFile := filepath.Join(tmpDir, "support-bot.yaml")
	promptContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: "support"
  version: "v1.0.0"
  system_template: "You are a support agent."
`
	if err := os.WriteFile(promptFile, []byte(promptContent), 0644); err != nil {
		t.Fatalf("Failed to write prompt file: %v", err)
	}

	// Create scenario with task_type="sales" (doesn't exist in prompt configs)
	scenarioFile := filepath.Join(tmpDir, "scenario.yaml")
	scenarioContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Scenario
metadata:
  name: test-scenario
spec:
  task_type: "sales"
  description: "Test Scenario"
  turns:
    - role: user
      content: "Hello"
`
	if err := os.WriteFile(scenarioFile, []byte(scenarioContent), 0644); err != nil {
		t.Fatalf("Failed to write scenario file: %v", err)
	}

	cfg := &config.Config{
		PromptConfigs: []config.PromptConfigRef{
			{
				ID:   "support-config",
				File: "support-bot.yaml",
			},
		},
		Scenarios: []config.ScenarioRef{
			{File: "scenario.yaml"},
		},
		Providers: []config.ProviderRef{},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   1500,
		},
	}

	engine := newTestEngine(t, tmpDir, cfg)

	// Attempting to load a non-existent task_type should return nil
	prompt := engine.promptRegistry.LoadWithVars("sales", map[string]string{}, "")
	if prompt != nil {
		t.Error("Expected nil when loading non-existent task_type='sales'")
	}
}

// TestPromptConfigMapping_DuplicateTaskType tests that having multiple prompt files
// with the same task_type causes a clear error
func TestPromptConfigMapping_DuplicateTaskType(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first prompt file with task_type="support" using K8s-style manifest
	promptFile1 := filepath.Join(tmpDir, "support-v1.yaml")
	promptContent1 := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support-v1
spec:
  task_type: "support"
  version: "v1.0.0"
  system_template: "Version 1"
`
	if err := os.WriteFile(promptFile1, []byte(promptContent1), 0644); err != nil {
		t.Fatalf("Failed to write prompt file 1: %v", err)
	}

	// Create second prompt file with same task_type="support" using K8s-style manifest
	promptFile2 := filepath.Join(tmpDir, "support-v2.yaml")
	promptContent2 := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support-v2
spec:
  task_type: "support"
  version: "v2.0.0"
  system_template: "Version 2"
`
	if err := os.WriteFile(promptFile2, []byte(promptContent2), 0644); err != nil {
		t.Fatalf("Failed to write prompt file 2: %v", err)
	}

	cfg := &config.Config{
		PromptConfigs: []config.PromptConfigRef{
			{
				ID:   "support-v1",
				File: "support-v1.yaml",
			},
			{
				ID:   "support-v2",
				File: "support-v2.yaml",
			},
		},
		Scenarios: []config.ScenarioRef{},
		Providers: []config.ProviderRef{},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   1500,
		},
	}

	// NewEngine should fail with a clear error about duplicate task_type
	_, err := newTestEngineWithError(t, tmpDir, cfg)
	if err == nil {
		t.Fatal("Expected error for duplicate task_type, got nil")
	}

	// Error should mention both files and the duplicate task_type
	errMsg := err.Error()
	if !contains(errMsg, "support") {
		t.Errorf("Error should mention duplicate task_type 'support', got: %s", errMsg)
	}
	if !contains(errMsg, "support-v1.yaml") || !contains(errMsg, "support-v2.yaml") {
		t.Errorf("Error should mention both conflicting files, got: %s", errMsg)
	}
}

// TestPromptConfigMapping_MultipleTaskTypes tests that multiple different task_types work correctly
func TestPromptConfigMapping_MultipleTaskTypes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create prompt file with task_type="support" using K8s-style manifest
	supportFile := filepath.Join(tmpDir, "support.yaml")
	supportContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: support
spec:
  task_type: "support"
  version: "v1.0.0"
  system_template: "You are a support agent."
`
	if err := os.WriteFile(supportFile, []byte(supportContent), 0644); err != nil {
		t.Fatalf("Failed to write support file: %v", err)
	}

	// Create prompt file with task_type="sales" using K8s-style manifest
	salesFile := filepath.Join(tmpDir, "sales.yaml")
	salesContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: sales
spec:
  task_type: "sales"
  version: "v1.0.0"
  system_template: "You are a sales agent."
`
	if err := os.WriteFile(salesFile, []byte(salesContent), 0644); err != nil {
		t.Fatalf("Failed to write sales file: %v", err)
	}

	cfg := &config.Config{
		PromptConfigs: []config.PromptConfigRef{
			{
				ID:   "config-1", // Arbitrary IDs
				File: "support.yaml",
			},
			{
				ID:   "config-2",
				File: "sales.yaml",
			},
		},
		Scenarios: []config.ScenarioRef{},
		Providers: []config.ProviderRef{},
		Defaults: config.Defaults{
			Temperature: 0.7,
			MaxTokens:   1500,
		},
	}

	engine := newTestEngine(t, tmpDir, cfg)

	// Both task_types should be loadable
	supportPrompt := engine.promptRegistry.LoadWithVars("support", map[string]string{}, "")
	if supportPrompt == nil {
		t.Error("Expected to load prompt by task_type='support'")
	}

	salesPrompt := engine.promptRegistry.LoadWithVars("sales", map[string]string{}, "")
	if salesPrompt == nil {
		t.Error("Expected to load prompt by task_type='sales'")
	}

	// Verify they loaded the correct prompts
	if supportPrompt != nil && !contains(supportPrompt.SystemPrompt, "support agent") {
		t.Error("Support prompt has wrong content")
	}

	if salesPrompt != nil && !contains(salesPrompt.SystemPrompt, "sales agent") {
		t.Error("Sales prompt has wrong content")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(s) == len(substr) && s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
