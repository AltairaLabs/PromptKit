package json

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestJSONPromptRepository_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "test.json")

	jsonContent := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {
    "task_type": "test-task",
    "version": "v1.0.0",
    "system_template": "Test prompt",
    "required_vars": [],
    "optional_vars": {}
  }
}`

	err := os.WriteFile(promptFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{"test-task": "test.json"}
	repo := NewJSONPromptRepository(tmpDir, mappings)

	config, err := repo.LoadPrompt("test-task")
	if err != nil {
		t.Fatalf("LoadPrompt() failed: %v", err)
	}

	if config.Spec.TaskType != "test-task" {
		t.Errorf("Expected task_type 'test-task', got '%s'", config.Spec.TaskType)
	}
}

func TestJSONPromptRepository_SearchByFilename(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "my-task.json")

	jsonContent := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {
    "task_type": "my-task",
    "version": "v1.0.0",
    "system_template": "Test prompt",
    "required_vars": [],
    "optional_vars": {}
  }
}`

	err := os.WriteFile(promptFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONPromptRepository(tmpDir, nil)

	config, err := repo.LoadPrompt("my-task")
	if err != nil {
		t.Fatalf("LoadPrompt() with filename search failed: %v", err)
	}

	if config.Spec.TaskType != "my-task" {
		t.Errorf("Expected task_type 'my-task', got '%s'", config.Spec.TaskType)
	}
}

func TestJSONPromptRepository_SearchByContent(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "something.json")

	jsonContent := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {
    "task_type": "content-task",
    "version": "v1.0.0",
    "system_template": "Test prompt",
    "required_vars": [],
    "optional_vars": {}
  }
}`

	err := os.WriteFile(promptFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONPromptRepository(tmpDir, nil)

	config, err := repo.LoadPrompt("content-task")
	if err != nil {
		t.Fatalf("LoadPrompt() with content search failed: %v", err)
	}

	if config.Spec.TaskType != "content-task" {
		t.Errorf("Expected task_type 'content-task', got '%s'", config.Spec.TaskType)
	}
}

func TestJSONPromptRepository_Cache(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "cached.json")

	jsonContent := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {
    "task_type": "cached-task",
    "version": "v1.0.0",
    "system_template": "Test prompt",
    "required_vars": [],
    "optional_vars": {}
  }
}`

	err := os.WriteFile(promptFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{"cached-task": "cached.json"}
	repo := NewJSONPromptRepository(tmpDir, mappings)

	// Load once
	config1, err := repo.LoadPrompt("cached-task")
	if err != nil {
		t.Fatalf("LoadPrompt() first call failed: %v", err)
	}

	// Load again (should use cache)
	config2, err := repo.LoadPrompt("cached-task")
	if err != nil {
		t.Fatalf("LoadPrompt() second call failed: %v", err)
	}

	// Should be the same pointer (cached)
	if config1 != config2 {
		t.Error("Expected cached result to be same pointer")
	}
}

func TestJSONPromptRepository_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	promptFile := filepath.Join(tmpDir, "invalid.json")

	err := os.WriteFile(promptFile, []byte("not valid json"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	mappings := map[string]string{"invalid": "invalid.json"}
	repo := NewJSONPromptRepository(tmpDir, mappings)

	_, err = repo.LoadPrompt("invalid")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

func TestJSONPromptRepository_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "missing apiVersion",
			content: `{
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {"task_type": "test"}
}`,
			wantErr: "missing apiVersion",
		},
		{
			name: "wrong kind",
			content: `{
  "apiVersion": "v1",
  "kind": "WrongKind",
  "metadata": {"name": "test"},
  "spec": {"task_type": "test"}
}`,
			wantErr: "invalid kind",
		},
		{
			name: "missing task_type",
			content: `{
  "apiVersion": "v1",
  "kind": "PromptConfig",
  "metadata": {"name": "test"},
  "spec": {}
}`,
			wantErr: "missing spec.task_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			promptFile := filepath.Join(tmpDir, "test.json")

			err := os.WriteFile(promptFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			mappings := map[string]string{"test": "test.json"}
			repo := NewJSONPromptRepository(tmpDir, mappings)

			_, err = repo.LoadPrompt("test")
			if err == nil {
				t.Errorf("Expected error containing %q, got nil", tt.wantErr)
			}
		})
	}
}

func TestJSONPromptRepository_LoadFragment(t *testing.T) {
	tmpDir := t.TempDir()
	fragmentsDir := filepath.Join(tmpDir, "fragments")
	err := os.MkdirAll(fragmentsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create fragments dir: %v", err)
	}

	fragmentFile := filepath.Join(fragmentsDir, "test-fragment.json")
	fragmentContent := `{
  "content": "Test fragment content"
}`

	err = os.WriteFile(fragmentFile, []byte(fragmentContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write fragment file: %v", err)
	}

	repo := NewJSONPromptRepository(tmpDir, nil)

	fragment, err := repo.LoadFragment("test-fragment", "", "")
	if err != nil {
		t.Fatalf("LoadFragment() failed: %v", err)
	}

	if fragment.Content != "Test fragment content" {
		t.Errorf("Expected content 'Test fragment content', got '%s'", fragment.Content)
	}
}

func TestJSONPromptRepository_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewJSONPromptRepository(tmpDir, nil)

	_, err := repo.LoadPrompt("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent prompt")
	}
}

func TestJSONToolRepository_LoadDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create tool files in different directories
	toolFile1 := filepath.Join(tmpDir, "tool1.json")
	toolFile2 := filepath.Join(subDir, "tool2.json")

	jsonContent1 := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "Tool",
  "metadata": {"name": "tool1"},
  "spec": {
    "description": "Test tool 1",
    "input_schema": {"type": "object"},
    "mode": "mock"
  }
}`

	jsonContent2 := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "Tool",
  "metadata": {"name": "tool2"},
  "spec": {
    "description": "Test tool 2",
    "input_schema": {"type": "object"},
    "mode": "mock"
  }
}`

	err = os.WriteFile(toolFile1, []byte(jsonContent1), 0644)
	if err != nil {
		t.Fatalf("Failed to write tool1: %v", err)
	}

	err = os.WriteFile(toolFile2, []byte(jsonContent2), 0644)
	if err != nil {
		t.Fatalf("Failed to write tool2: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	err = repo.LoadDirectory(tmpDir)
	if err != nil {
		t.Fatalf("LoadDirectory() failed: %v", err)
	}

	tools, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("Expected 2 tools after LoadDirectory, got %d", len(tools))
	}
}

func TestJSONToolRepository_LegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "legacy-tool.json")

	jsonContent := `{
  "name": "legacy_tool",
  "description": "Legacy format tool",
  "input_schema": {"type": "object"},
  "mode": "mock"
}`

	err := os.WriteFile(toolFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err != nil {
		t.Fatalf("LoadToolFromFile() with legacy format failed: %v", err)
	}

	descriptor, err := repo.LoadTool("legacy_tool")
	if err != nil {
		t.Fatalf("LoadTool() failed: %v", err)
	}

	if descriptor.Description != "Legacy format tool" {
		t.Errorf("Expected description 'Legacy format tool', got '%s'", descriptor.Description)
	}
}

func TestJSONToolRepository_InvalidKind(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "invalid.json")

	jsonContent := `{
  "apiVersion": "v1",
  "kind": "NotATool",
  "metadata": {"name": "invalid"},
  "spec": {}
}`

	err := os.WriteFile(toolFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for invalid kind")
	}
}

func TestJSONToolRepository_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "no-name.json")

	jsonContent := `{
  "description": "Tool without name",
  "input_schema": {"type": "object"},
  "mode": "mock"
}`

	err := os.WriteFile(toolFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for missing name")
	}
}

func TestJSONPromptRepository_ListPrompts(t *testing.T) {
	tmpDir := t.TempDir()

	mappings := map[string]string{
		"task1": "file1.json",
		"task2": "file2.json",
	}
	repo := NewJSONPromptRepository(tmpDir, mappings)

	prompts, err := repo.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() failed: %v", err)
	}

	if len(prompts) != 2 {
		t.Errorf("Expected 2 prompts, got %d", len(prompts))
	}
}

func TestJSONPromptRepository_SavePrompt_NotImplemented(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewJSONPromptRepository(tmpDir, nil)

	config := &prompt.PromptConfig{}
	err := repo.SavePrompt(config)
	if err == nil {
		t.Error("Expected 'not implemented' error, got nil")
	}
}

func TestJSONToolRepository_Basic(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "test-tool.json")

	jsonContent := `{
  "apiVersion": "promptkit.altairalabs.ai/v1alpha1",
  "kind": "Tool",
  "metadata": {"name": "test_tool"},
  "spec": {
    "description": "Test tool",
    "input_schema": {"type": "object"},
    "mode": "mock"
  }
}`

	err := os.WriteFile(toolFile, []byte(jsonContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewJSONToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err != nil {
		t.Fatalf("LoadToolFromFile() failed: %v", err)
	}

	descriptor, err := repo.LoadTool("test_tool")
	if err != nil {
		t.Fatalf("LoadTool() failed: %v", err)
	}

	if descriptor.Name != "test_tool" {
		t.Errorf("Expected name 'test_tool', got '%s'", descriptor.Name)
	}
}

func TestJSONToolRepository_ListTools(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewJSONToolRepository(tmpDir)

	// Register tools directly
	repo.RegisterTool("tool1", &tools.ToolDescriptor{Name: "tool1"})
	repo.RegisterTool("tool2", &tools.ToolDescriptor{Name: "tool2"})

	toolsList, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(toolsList) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(toolsList))
	}
}

func TestJSONToolRepository_SaveTool_NotImplemented(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewJSONToolRepository(tmpDir)

	err := repo.SaveTool(nil)
	if err == nil {
		t.Error("Expected 'not implemented' error, got nil")
	}
}
