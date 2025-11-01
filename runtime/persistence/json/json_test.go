package json

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestJSONPromptRepository_Basic(t *testing.T) {
	t.Skip("JSON unmarshaling requires json struct tags - to be fixed in PromptSpec")
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
