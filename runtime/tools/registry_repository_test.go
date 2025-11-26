package tools_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TestNewRegistryWithRepository verifies the new constructor works
func TestNewRegistryWithRepository(t *testing.T) {
	repo := memory.NewToolRepository()
	registry := tools.NewRegistryWithRepository(repo)

	if registry == nil {
		t.Fatal("NewRegistryWithRepository() returned nil")
	}
}

// TestRegistry_GetWithRepository tests loading tools via repository
func TestRegistry_GetWithRepository(t *testing.T) {
	repo := memory.NewToolRepository()

	// Register a test tool
	repo.RegisterTool("test-tool", &tools.ToolDescriptor{
		Name:        "test-tool",
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	})

	registry := tools.NewRegistryWithRepository(repo)

	tool := registry.Get("test-tool")
	if tool == nil {
		t.Fatal("Get() returned nil")
	}

	if tool.Name != "test-tool" {
		t.Errorf("Expected name 'test-tool', got '%s'", tool.Name)
	}

	if tool.Description != "A test tool" {
		t.Errorf("Expected description 'A test tool', got '%s'", tool.Description)
	}
}

// TestRegistry_ListWithRepository tests listing tools
func TestRegistry_ListWithRepository(t *testing.T) {
	repo := memory.NewToolRepository()

	repo.RegisterTool("tool1", &tools.ToolDescriptor{
		Name:        "tool1",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})
	repo.RegisterTool("tool2", &tools.ToolDescriptor{
		Name:        "tool2",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})

	registry := tools.NewRegistryWithRepository(repo)

	toolNames := registry.List()

	if len(toolNames) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(toolNames))
	}

	// Check that both tools are in the list
	hasTool1, hasTool2 := false, false
	for _, name := range toolNames {
		if name == "tool1" {
			hasTool1 = true
		}
		if name == "tool2" {
			hasTool2 = true
		}
	}

	if !hasTool1 || !hasTool2 {
		t.Errorf("Expected to find 'tool1' and 'tool2' in list, got %v", toolNames)
	}
}

// TestRegistry_RegisterWithRepository tests registering new tools
func TestRegistry_RegisterWithRepository(t *testing.T) {
	repo := memory.NewToolRepository()
	registry := tools.NewRegistryWithRepository(repo)

	descriptor := &tools.ToolDescriptor{
		Name:        "new-tool",
		Description: "A newly registered tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}

	err := registry.Register(descriptor)
	if err != nil {
		t.Fatalf("Register() failed: %v", err)
	}

	// Verify it can be retrieved
	tool := registry.Get("new-tool")
	if tool == nil {
		t.Fatal("Get() after Register() returned nil")
	}

	if tool.Description != "A newly registered tool" {
		t.Errorf("Expected description 'A newly registered tool', got '%s'", tool.Description)
	}
}

// TestRegistry_GetNonexistent tests error handling for missing tools
func TestRegistry_GetNonexistent_WithRepository(t *testing.T) {
	repo := memory.NewToolRepository()
	registry := tools.NewRegistryWithRepository(repo)

	tool := registry.Get("nonexistent")
	if tool != nil {
		t.Error("Expected nil for nonexistent tool, got a result")
	}
}

// TestRegistry_CachingWithRepository verifies that tools are cached after first load
func TestRegistry_CachingWithRepository(t *testing.T) {
	repo := memory.NewToolRepository()

	repo.RegisterTool("cached-tool", &tools.ToolDescriptor{
		Name:        "cached-tool",
		Description: "Original description",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	})

	registry := tools.NewRegistryWithRepository(repo)

	// First call - loads from repository
	tool1 := registry.Get("cached-tool")
	if tool1 == nil {
		t.Fatal("First Get() returned nil")
	}

	// Second call - should return cached version
	tool2 := registry.Get("cached-tool")
	if tool2 == nil {
		t.Fatal("Second Get() returned nil")
	}

	// Should be the same pointer (cached)
	if tool1 != tool2 {
		t.Error("Expected cached tool to return same pointer")
	}
}
