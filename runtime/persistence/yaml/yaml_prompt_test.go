package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

func TestNewYAMLPromptRepository(t *testing.T) {
	repo := NewYAMLPromptRepository("/tmp/test", nil)
	if repo == nil {
		t.Fatal("NewYAMLPromptRepository() returned nil")
	}
}

func TestYAMLPromptRepository_LoadPrompt_WithExplicitMapping(t *testing.T) {
	// Create a temporary directory with a test YAML file
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "test-prompt.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-support
spec:
  task_type: support
  version: v1.0.0
  description: Test prompt
  system_template: "You are a helpful assistant"
`

	err := os.WriteFile(promptFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create repository with explicit mapping
	mappings := map[string]string{
		"support": "test-prompt.yaml",
	}
	repo := NewYAMLPromptRepository(tmpDir, mappings)

	// Load prompt
	config, err := repo.LoadPrompt("support")
	if err != nil {
		t.Fatalf("LoadPrompt() failed: %v", err)
	}

	if config.Spec.TaskType != "support" {
		t.Errorf("Expected task_type 'support', got '%s'", config.Spec.TaskType)
	}

	if config.Spec.SystemTemplate != "You are a helpful assistant" {
		t.Errorf("Expected system template 'You are a helpful assistant', got '%s'", config.Spec.SystemTemplate)
	}
}

func TestYAMLPromptRepository_LoadPrompt_SearchByFilename(t *testing.T) {
	// Create a temporary directory with a test YAML file
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "customer-support.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: customer-support
spec:
  task_type: customer-support
  version: v1.0.0
  description: Customer support prompt
  system_template: "You are a customer support agent"
`

	err := os.WriteFile(promptFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create repository without explicit mapping (will search)
	repo := NewYAMLPromptRepository(tmpDir, nil)

	// Load prompt
	config, err := repo.LoadPrompt("customer-support")
	if err != nil {
		t.Fatalf("LoadPrompt() failed: %v", err)
	}

	if config.Spec.TaskType != "customer-support" {
		t.Errorf("Expected task_type 'customer-support', got '%s'", config.Spec.TaskType)
	}
}

func TestYAMLPromptRepository_LoadPrompt_SearchByTaskType(t *testing.T) {
	// Create a temporary directory with a test YAML file with different name
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "arbitrary-filename.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: test-prompt
spec:
  task_type: search-me
  version: v1.0.0
  description: Test search by task type
  system_template: "Found by task type"
`

	err := os.WriteFile(promptFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create repository without explicit mapping (will search)
	repo := NewYAMLPromptRepository(tmpDir, nil)

	// Load prompt by task type (not filename)
	config, err := repo.LoadPrompt("search-me")
	if err != nil {
		t.Fatalf("LoadPrompt() failed: %v", err)
	}

	if config.Spec.SystemTemplate != "Found by task type" {
		t.Errorf("Expected 'Found by task type', got '%s'", config.Spec.SystemTemplate)
	}
}

func TestYAMLPromptRepository_LoadPrompt_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLPromptRepository(tmpDir, nil)

	_, err := repo.LoadPrompt("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent prompt, got nil")
	}
}

func TestYAMLPromptRepository_LoadPrompt_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `this is not: [valid yaml`

	err := os.WriteFile(promptFile, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{
		"invalid": "invalid.yaml",
	}
	repo := NewYAMLPromptRepository(tmpDir, mappings)

	_, err = repo.LoadPrompt("invalid")
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestYAMLPromptRepository_LoadPrompt_MissingRequiredFields(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "incomplete.yaml")

	// Missing task_type
	incompleteYAML := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: incomplete
spec:
  system_template: "Missing task type"
`

	err := os.WriteFile(promptFile, []byte(incompleteYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{
		"incomplete": "incomplete.yaml",
	}
	repo := NewYAMLPromptRepository(tmpDir, mappings)

	_, err = repo.LoadPrompt("incomplete")
	if err == nil {
		t.Error("Expected validation error for missing task_type, got nil")
	}
}

func TestYAMLPromptRepository_LoadPrompt_Caching(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "cached.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: cached
spec:
  task_type: cached
  version: v1.0.0
  system_template: "Cached prompt"
`

	err := os.WriteFile(promptFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{
		"cached": "cached.yaml",
	}
	repo := NewYAMLPromptRepository(tmpDir, mappings)

	// Load first time
	config1, err := repo.LoadPrompt("cached")
	if err != nil {
		t.Fatalf("First LoadPrompt() failed: %v", err)
	}

	// Load second time (should be cached)
	config2, err := repo.LoadPrompt("cached")
	if err != nil {
		t.Fatalf("Second LoadPrompt() failed: %v", err)
	}

	// They should be the same instance (cached)
	if config1 != config2 {
		t.Error("Expected cached config to be same instance")
	}
}

func TestYAMLPromptRepository_ListPrompts_WithMappings(t *testing.T) {
	tmpDir := t.TempDir()

	mappings := map[string]string{
		"task1": "prompt1.yaml",
		"task2": "prompt2.yaml",
		"task3": "prompt3.yaml",
	}
	repo := NewYAMLPromptRepository(tmpDir, mappings)

	prompts, err := repo.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() failed: %v", err)
	}

	if len(prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(prompts))
	}

	// Check all task types are present
	found := make(map[string]bool)
	for _, p := range prompts {
		found[p] = true
	}

	for _, expected := range []string{"task1", "task2", "task3"} {
		if !found[expected] {
			t.Errorf("Expected to find task type '%s' in list", expected)
		}
	}
}

func TestYAMLPromptRepository_ListPrompts_ByScanning(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple YAML files
	files := []struct {
		name     string
		taskType string
	}{
		{"prompt1.yaml", "task-alpha"},
		{"prompt2.yaml", "task-beta"},
		{"prompt3.yaml", "task-gamma"},
	}

	for _, f := range files {
		yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: PromptConfig
metadata:
  name: ` + f.taskType + `
spec:
  task_type: ` + f.taskType + `
  system_template: "Test"
`
		err := os.WriteFile(filepath.Join(tmpDir, f.name), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write %s: %v", f.name, err)
		}
	}

	repo := NewYAMLPromptRepository(tmpDir, nil)

	prompts, err := repo.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() failed: %v", err)
	}

	if len(prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(prompts))
	}
}

func TestYAMLPromptRepository_LoadFragment(t *testing.T) {
	tmpDir := t.TempDir()
	fragmentsDir := filepath.Join(tmpDir, "fragments")
	err := os.MkdirAll(fragmentsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create fragments dir: %v", err)
	}

	fragmentFile := filepath.Join(fragmentsDir, "test-fragment.yaml")
	fragmentContent := `fragment_type: persona
version: v1.0.0
description: Test fragment
content: "This is fragment content"
`

	err = os.WriteFile(fragmentFile, []byte(fragmentContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write fragment file: %v", err)
	}

	repo := NewYAMLPromptRepository(tmpDir, nil)

	fragment, err := repo.LoadFragment("test-fragment", "", "")
	if err != nil {
		t.Fatalf("LoadFragment() failed: %v", err)
	}

	if fragment.Content != "This is fragment content" {
		t.Errorf("Expected 'This is fragment content', got '%s'", fragment.Content)
	}
}

func TestYAMLPromptRepository_LoadFragment_WithRelativePath(t *testing.T) {
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "custom")
	err := os.MkdirAll(customDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create custom dir: %v", err)
	}

	fragmentFile := filepath.Join(customDir, "custom-fragment.yaml")
	fragmentContent := `fragment_type: persona
version: v1.0.0
description: Custom fragment
content: "Custom fragment content"
`

	err = os.WriteFile(fragmentFile, []byte(fragmentContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write fragment file: %v", err)
	}

	repo := NewYAMLPromptRepository(tmpDir, nil)

	fragment, err := repo.LoadFragment("custom-fragment", "custom/custom-fragment.yaml", tmpDir)
	if err != nil {
		t.Fatalf("LoadFragment() failed: %v", err)
	}

	if fragment.Content != "Custom fragment content" {
		t.Errorf("Expected 'Custom fragment content', got '%s'", fragment.Content)
	}
}

func TestYAMLPromptRepository_SavePrompt(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLPromptRepository(tmpDir, nil)

	config := &prompt.Config{
		APIVersion: "promptkit.dev/v1",
		Kind:       "PromptConfig",
		Spec: prompt.Spec{
			TaskType:       "save-test",
			SystemTemplate: "Test system template",
		},
	}

	// Save the prompt
	err := repo.SavePrompt(config)
	if err != nil {
		t.Fatalf("SavePrompt() failed: %v", err)
	}

	// Verify file was created
	expectedPath := tmpDir + "/save-test.yaml"
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Fatalf("Expected file %s to exist after SavePrompt", expectedPath)
	}

	// Load it back and verify
	loaded, err := repo.LoadPrompt("save-test")
	if err != nil {
		t.Fatalf("LoadPrompt() after SavePrompt() failed: %v", err)
	}
	if loaded.Spec.TaskType != "save-test" {
		t.Errorf("Expected task_type 'save-test', got '%s'", loaded.Spec.TaskType)
	}
	if loaded.Spec.SystemTemplate != "Test system template" {
		t.Errorf("Expected system_template 'Test system template', got '%s'", loaded.Spec.SystemTemplate)
	}
}

func TestYAMLPromptRepository_SavePrompt_NilConfig(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLPromptRepository(tmpDir, nil)

	err := repo.SavePrompt(nil)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}
}

func TestYAMLPromptRepository_SavePrompt_EmptyTaskType(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLPromptRepository(tmpDir, nil)

	config := &prompt.Config{
		Spec: prompt.Spec{
			TaskType: "",
		},
	}

	err := repo.SavePrompt(config)
	if err == nil {
		t.Error("Expected error for empty task_type, got nil")
	}
}
