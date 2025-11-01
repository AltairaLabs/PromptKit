package prompt_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// TestNewRegistryWithRepository verifies the new constructor works
func TestNewRegistryWithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()
	registry := prompt.NewRegistryWithRepository(repo)

	if registry == nil {
		t.Fatal("NewRegistryWithRepository() returned nil")
	}
}

// TestRegistry_LoadWithRepository tests loading prompts via repository
func TestRegistry_LoadWithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()

	// Register a test prompt
	repo.RegisterPrompt("test-task", &prompt.PromptConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.PromptSpec{
			TaskType:       "test-task",
			SystemTemplate: "You are a test assistant",
			RequiredVars:   []string{},
			OptionalVars:   map[string]string{},
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	assembled := registry.Load("test-task")
	if assembled == nil {
		t.Fatal("Load() returned nil")
	}

	if assembled.TaskType != "test-task" {
		t.Errorf("Expected task_type 'test-task', got '%s'", assembled.TaskType)
	}

	if assembled.SystemPrompt != "You are a test assistant" {
		t.Errorf("Expected system prompt 'You are a test assistant', got '%s'", assembled.SystemPrompt)
	}
}

// TestRegistry_LoadWithVars tests variable substitution
func TestRegistry_LoadWithVars_WithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()

	repo.RegisterPrompt("var-test", &prompt.PromptConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.PromptSpec{
			TaskType:       "var-test",
			SystemTemplate: "Hello {{name}}, welcome to {{place}}!",
			RequiredVars:   []string{"name"},
			OptionalVars:   map[string]string{"place": "Earth"},
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	vars := map[string]string{"name": "Alice", "place": "Wonderland"}
	assembled := registry.LoadWithVars("var-test", vars, "")

	if assembled == nil {
		t.Fatal("LoadWithVars() returned nil")
	}

	expected := "Hello Alice, welcome to Wonderland!"
	if assembled.SystemPrompt != expected {
		t.Errorf("Expected '%s', got '%s'", expected, assembled.SystemPrompt)
	}
}

// TestRegistry_ListTaskTypes tests listing available task types
func TestRegistry_ListTaskTypes_WithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()

	repo.RegisterPrompt("task1", &prompt.PromptConfig{
		Spec: prompt.PromptSpec{TaskType: "task1"},
	})
	repo.RegisterPrompt("task2", &prompt.PromptConfig{
		Spec: prompt.PromptSpec{TaskType: "task2"},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	// Force load to populate cache
	registry.LoadConfig("task1")
	registry.LoadConfig("task2")

	taskTypes := registry.ListTaskTypes()

	if len(taskTypes) != 2 {
		t.Errorf("Expected 2 task types, got %d", len(taskTypes))
	}
}

// TestRegistry_RegisterConfig tests programmatic config registration
func TestRegistry_RegisterConfig_WithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()
	registry := prompt.NewRegistryWithRepository(repo)

	config := &prompt.PromptConfig{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "PromptConfig",
		Spec: prompt.PromptSpec{
			TaskType:       "registered-task",
			SystemTemplate: "Registered prompt",
		},
	}

	err := registry.RegisterConfig("registered-task", config)
	if err != nil {
		t.Fatalf("RegisterConfig() failed: %v", err)
	}

	assembled := registry.Load("registered-task")
	if assembled == nil {
		t.Fatal("Load() after RegisterConfig() returned nil")
	}

	if assembled.SystemPrompt != "Registered prompt" {
		t.Errorf("Expected 'Registered prompt', got '%s'", assembled.SystemPrompt)
	}
}

// TestRegistry_LoadConfig tests loading raw config
func TestRegistry_LoadConfig_WithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()

	repo.RegisterPrompt("config-test", &prompt.PromptConfig{
		Spec: prompt.PromptSpec{
			TaskType:       "config-test",
			SystemTemplate: "Test",
			Version:        "v1.0.0",
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	config, err := registry.LoadConfig("config-test")
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	if config.Spec.Version != "v1.0.0" {
		t.Errorf("Expected version 'v1.0.0', got '%s'", config.Spec.Version)
	}
}

// TestRegistry_LoadNonexistent tests error handling for missing prompts
func TestRegistry_LoadNonexistent_WithRepository(t *testing.T) {
	repo := memory.NewMemoryPromptRepository()
	registry := prompt.NewRegistryWithRepository(repo)

	assembled := registry.Load("nonexistent")
	if assembled != nil {
		t.Error("Expected nil for nonexistent prompt, got a result")
	}
}
