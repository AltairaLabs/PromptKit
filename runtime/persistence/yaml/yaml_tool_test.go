package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestNewYAMLToolRepository(t *testing.T) {
	repo := NewYAMLToolRepository("/tmp/test")
	if repo == nil {
		t.Fatal("NewYAMLToolRepository() returned nil")
	}
}

func TestYAMLToolRepository_LoadTool_K8sManifest(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "test-tool.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_order_status
spec:
  description: Retrieves order status
  input_schema:
    type: object
    properties:
      order_id:
        type: string
    required:
      - order_id
  output_schema:
    type: object
    properties:
      status:
        type: string
  mode: mock
  timeout_ms: 2000
  mock_result:
    status: "shipped"
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err != nil {
		t.Fatalf("LoadToolFromFile() failed: %v", err)
	}

	descriptor, err := repo.LoadTool("get_order_status")
	if err != nil {
		t.Fatalf("LoadTool() failed: %v", err)
	}

	if descriptor.Name != "get_order_status" {
		t.Errorf("Expected name 'get_order_status', got '%s'", descriptor.Name)
	}

	if descriptor.Description != "Retrieves order status" {
		t.Errorf("Expected description 'Retrieves order status', got '%s'", descriptor.Description)
	}
}

func TestYAMLToolRepository_LoadTool_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLToolRepository(tmpDir)

	_, err := repo.LoadTool("nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent tool, got nil")
	}
}

func TestYAMLToolRepository_LoadTool_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `this is not: [valid yaml`

	err := os.WriteFile(toolFile, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestYAMLToolRepository_ListTools_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLToolRepository(tmpDir)

	tools, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(tools) != 0 {
		t.Errorf("Expected empty list, got %d tools", len(tools))
	}
}

func TestYAMLToolRepository_ListTools_Multiple(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple tool files
	tools := []string{"tool1", "tool2", "tool3"}
	for _, toolName := range tools {
		toolFile := filepath.Join(tmpDir, toolName+".yaml")
		yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: ` + toolName + `
spec:
  description: Test tool
  input_schema:
    type: object
  mode: mock
`
		err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write %s: %v", toolFile, err)
		}
	}

	repo := NewYAMLToolRepository(tmpDir)

	// Load all tools
	for _, toolName := range tools {
		toolFile := filepath.Join(tmpDir, toolName+".yaml")
		err := repo.LoadToolFromFile(toolFile)
		if err != nil {
			t.Fatalf("LoadToolFromFile(%s) failed: %v", toolName, err)
		}
	}

	toolsList, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(toolsList) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(toolsList))
	}

	// Check all tools are present
	found := make(map[string]bool)
	for _, name := range toolsList {
		found[name] = true
	}

	for _, expected := range tools {
		if !found[expected] {
			t.Errorf("Expected to find tool '%s' in list", expected)
		}
	}
}

func TestYAMLToolRepository_SaveTool(t *testing.T) {
	t.Run("nil descriptor returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewYAMLToolRepository(tmpDir)

		err := repo.SaveTool(nil)
		if err == nil {
			t.Error("Expected error for nil descriptor, got nil")
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewYAMLToolRepository(tmpDir)

		err := repo.SaveTool(&tools.ToolDescriptor{
			Description: "test tool",
		})
		if err == nil {
			t.Error("Expected error for empty name, got nil")
		}
	})

	t.Run("saves tool successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewYAMLToolRepository(tmpDir)

		descriptor := &tools.ToolDescriptor{
			Name:        "test-tool",
			Description: "A test tool",
			Mode:        "mock",
		}

		err := repo.SaveTool(descriptor)
		if err != nil {
			t.Fatalf("SaveTool() failed: %v", err)
		}

		// Verify file was created
		filePath := filepath.Join(tmpDir, "test-tool.yaml")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("Expected file to be created")
		}

		// Verify tool is in cache
		loaded, err := repo.LoadTool("test-tool")
		if err != nil {
			t.Fatalf("LoadTool() failed: %v", err)
		}
		if loaded.Name != "test-tool" {
			t.Errorf("Expected name 'test-tool', got '%s'", loaded.Name)
		}
	})

	t.Run("saved tool can be reloaded", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewYAMLToolRepository(tmpDir)

		descriptor := &tools.ToolDescriptor{
			Name:        "reload-test",
			Description: "Tool to test reloading",
			Mode:        "live",
			TimeoutMs:   5000,
		}

		err := repo.SaveTool(descriptor)
		if err != nil {
			t.Fatalf("SaveTool() failed: %v", err)
		}

		// Create new repo instance to test reload from file
		repo2 := NewYAMLToolRepository(tmpDir)
		err = repo2.LoadToolFromFile(filepath.Join(tmpDir, "reload-test.yaml"))
		if err != nil {
			t.Fatalf("LoadToolFromFile() failed: %v", err)
		}

		loaded, err := repo2.LoadTool("reload-test")
		if err != nil {
			t.Fatalf("LoadTool() failed: %v", err)
		}
		if loaded.Description != "Tool to test reloading" {
			t.Errorf("Expected description 'Tool to test reloading', got '%s'", loaded.Description)
		}
		if loaded.TimeoutMs != 5000 {
			t.Errorf("Expected timeout 5000, got %d", loaded.TimeoutMs)
		}
	})
}

func TestYAMLToolRepository_LoadDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create subdirectories with tools
	subDir1 := filepath.Join(tmpDir, "subdir1")
	subDir2 := filepath.Join(tmpDir, "subdir2")
	_ = os.MkdirAll(subDir1, 0755)
	_ = os.MkdirAll(subDir2, 0755)

	// Create tool files in different locations
	tools := []struct {
		dir  string
		name string
	}{
		{tmpDir, "root-tool"},
		{subDir1, "sub1-tool"},
		{subDir2, "sub2-tool"},
	}

	for _, tool := range tools {
		toolFile := filepath.Join(tool.dir, tool.name+".yaml")
		yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: ` + tool.name + `
spec:
  description: Test tool
  input_schema:
    type: object
  mode: mock
`
		err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write %s: %v", toolFile, err)
		}
	}

	repo := NewYAMLToolRepository(tmpDir)
	err := repo.LoadDirectory(tmpDir)
	if err != nil {
		t.Fatalf("LoadDirectory() failed: %v", err)
	}

	toolsList, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	if len(toolsList) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(toolsList))
	}
}
