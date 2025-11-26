package memory

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestNewPromptRepository(t *testing.T) {
	repo := NewPromptRepository()
	if repo == nil {
		t.Fatal("NewPromptRepository() returned nil")
	}
}

func TestMemoryPromptRepository_LoadPrompt_NotFound(t *testing.T) {
	repo := NewPromptRepository()
	_, err := repo.LoadPrompt("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent prompt, got nil")
	}
}

func TestMemoryPromptRepository_RegisterAndLoadPrompt(t *testing.T) {
	repo := NewPromptRepository()

	config := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       "test-task",
			SystemTemplate: "Test system prompt",
		},
	}

	repo.RegisterPrompt("test-task", config)

	loaded, err := repo.LoadPrompt("test-task")
	if err != nil {
		t.Fatalf("LoadPrompt() failed: %v", err)
	}

	if loaded.Spec.TaskType != "test-task" {
		t.Errorf("Expected task_type 'test-task', got '%s'", loaded.Spec.TaskType)
	}

	if loaded.Spec.SystemTemplate != "Test system prompt" {
		t.Errorf("Expected system template 'Test system prompt', got '%s'", loaded.Spec.SystemTemplate)
	}
}

func TestMemoryPromptRepository_SavePrompt(t *testing.T) {
	repo := NewPromptRepository()

	config := &prompt.Config{
		Spec: prompt.Spec{
			TaskType:       "save-test",
			SystemTemplate: "Saved prompt",
		},
	}

	err := repo.SavePrompt(config)
	if err != nil {
		t.Fatalf("SavePrompt() failed: %v", err)
	}

	loaded, err := repo.LoadPrompt("save-test")
	if err != nil {
		t.Fatalf("LoadPrompt() after SavePrompt() failed: %v", err)
	}

	if loaded.Spec.SystemTemplate != "Saved prompt" {
		t.Errorf("Expected 'Saved prompt', got '%s'", loaded.Spec.SystemTemplate)
	}
}

func TestMemoryPromptRepository_ListPrompts_Empty(t *testing.T) {
	repo := NewPromptRepository()

	prompts, err := repo.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() failed: %v", err)
	}

	if len(prompts) != 0 {
		t.Errorf("Expected empty list, got %d prompts", len(prompts))
	}
}

func TestMemoryPromptRepository_ListPrompts_Multiple(t *testing.T) {
	repo := NewPromptRepository()

	repo.RegisterPrompt("task1", &prompt.Config{
		Spec: prompt.Spec{TaskType: "task1"},
	})
	repo.RegisterPrompt("task2", &prompt.Config{
		Spec: prompt.Spec{TaskType: "task2"},
	})
	repo.RegisterPrompt("task3", &prompt.Config{
		Spec: prompt.Spec{TaskType: "task3"},
	})

	prompts, err := repo.ListPrompts()
	if err != nil {
		t.Fatalf("ListPrompts() failed: %v", err)
	}

	if len(prompts) != 3 {
		t.Errorf("Expected 3 prompts, got %d", len(prompts))
	}

	// Check that all task types are present
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

func TestMemoryPromptRepository_LoadFragment_NotFound(t *testing.T) {
	repo := NewPromptRepository()

	_, err := repo.LoadFragment("nonexistent", "", "")
	if err == nil {
		t.Error("Expected error for nonexistent fragment, got nil")
	}
}

func TestMemoryPromptRepository_RegisterAndLoadFragment(t *testing.T) {
	repo := NewPromptRepository()

	fragment := &prompt.Fragment{
		Content: "Fragment content",
	}

	repo.RegisterFragment("test-fragment", fragment)

	loaded, err := repo.LoadFragment("test-fragment", "", "")
	if err != nil {
		t.Fatalf("LoadFragment() failed: %v", err)
	}

	if loaded.Content != "Fragment content" {
		t.Errorf("Expected content 'Fragment content', got '%s'", loaded.Content)
	}
}

func TestNewToolRepository(t *testing.T) {
	repo := NewToolRepository()
	if repo == nil {
		t.Fatal("NewToolRepository() returned nil")
	}
}

func TestMemoryToolRepository_LoadTool_NotFound(t *testing.T) {
	repo := NewToolRepository()

	_, err := repo.LoadTool("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent tool, got nil")
	}
}

func TestMemoryToolRepository_RegisterAndLoadTool(t *testing.T) {
	repo := NewToolRepository()

	descriptor := &tools.ToolDescriptor{
		Name:        "test-tool",
		Description: "Test tool description",
	}

	repo.RegisterTool("test-tool", descriptor)

	loaded, err := repo.LoadTool("test-tool")
	if err != nil {
		t.Fatalf("LoadTool() failed: %v", err)
	}

	if loaded.Name != "test-tool" {
		t.Errorf("Expected name 'test-tool', got '%s'", loaded.Name)
	}

	if loaded.Description != "Test tool description" {
		t.Errorf("Expected description 'Test tool description', got '%s'", loaded.Description)
	}
}

func TestMemoryToolRepository_SaveTool(t *testing.T) {
	repo := NewToolRepository()

	descriptor := &tools.ToolDescriptor{
		Name:        "save-test-tool",
		Description: "Saved tool",
	}

	err := repo.SaveTool(descriptor)
	if err != nil {
		t.Fatalf("SaveTool() failed: %v", err)
	}

	loaded, err := repo.LoadTool("save-test-tool")
	if err != nil {
		t.Fatalf("LoadTool() after SaveTool() failed: %v", err)
	}

	if loaded.Description != "Saved tool" {
		t.Errorf("Expected 'Saved tool', got '%s'", loaded.Description)
	}
}

func TestMemoryToolRepository_ListTools_Empty(t *testing.T) {
	repo := NewToolRepository()

	toolsList, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(toolsList) != 0 {
		t.Errorf("Expected empty list, got %d tools", len(toolsList))
	}
}

func TestMemoryToolRepository_ListTools_Multiple(t *testing.T) {
	repo := NewToolRepository()

	repo.RegisterTool("tool1", &tools.ToolDescriptor{Name: "tool1"})
	repo.RegisterTool("tool2", &tools.ToolDescriptor{Name: "tool2"})
	repo.RegisterTool("tool3", &tools.ToolDescriptor{Name: "tool3"})

	toolsList, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(toolsList) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(toolsList))
	}

	// Check that all tools are present
	found := make(map[string]bool)
	for _, name := range toolsList {
		found[name] = true
	}

	for _, expected := range []string{"tool1", "tool2", "tool3"} {
		if !found[expected] {
			t.Errorf("Expected to find tool '%s' in list", expected)
		}
	}
}
