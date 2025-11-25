package yaml

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestYAMLToolRepository_LoadToolFromFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "not-yaml.yaml")

	invalidYAML := `this is: [not valid yaml: {missing close`

	err := os.WriteFile(toolFile, []byte(invalidYAML), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for invalid YAML structure")
	}
}

func TestYAMLToolRepository_LoadToolFromFile_LegacyFormat(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "legacy.yaml")

	yamlContent := `name: legacy_tool
description: Legacy format tool
input_schema:
  type: object
  properties:
    param:
      type: string
mode: mock
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err != nil {
		t.Fatalf("LoadToolFromFile() with legacy format failed: %v", err)
	}

	descriptor, err := repo.LoadTool("legacy_tool")
	if err != nil {
		t.Fatalf("LoadTool() failed: %v", err)
	}

	if descriptor.Name != "legacy_tool" {
		t.Errorf("Expected name 'legacy_tool', got '%s'", descriptor.Name)
	}

	if descriptor.Description != "Legacy format tool" {
		t.Errorf("Expected description 'Legacy format tool', got '%s'", descriptor.Description)
	}
}

func TestYAMLToolRepository_LoadToolFromFile_MissingName(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "no-name.yaml")

	yamlContent := `description: Tool without name
input_schema:
  type: object
mode: mock
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for legacy tool missing name")
	}
}

func TestYAMLToolRepository_LoadToolFromFile_InvalidKind(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "invalid-kind.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: NotATool
metadata:
  name: invalid_kind_tool
spec:
  description: Invalid kind
  input_schema:
    type: object
  mode: mock
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for invalid kind")
	}
}

func TestYAMLToolRepository_LoadToolFromFile_MissingKind(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "missing-kind.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
metadata:
  name: missing_kind_tool
spec:
  description: Missing kind
  input_schema:
    type: object
  mode: mock
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for missing kind")
	}
}

func TestYAMLToolRepository_LoadToolFromFile_MissingMetadataName(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "no-metadata-name.yaml")

	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata: {}
spec:
  description: Missing metadata name
  input_schema:
    type: object
  mode: mock
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for missing metadata.name")
	}
}

func TestYAMLToolRepository_LoadDirectory_SkipNonYAML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create YAML tool
	yamlFile := filepath.Join(tmpDir, "tool.yaml")
	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: yaml_tool
spec:
  description: YAML tool
  input_schema:
    type: object
  mode: mock
`
	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write YAML file: %v", err)
	}

	// Create non-YAML file (should be skipped)
	txtFile := filepath.Join(tmpDir, "readme.txt")
	err = os.WriteFile(txtFile, []byte("This is not a tool file"), 0644)
	if err != nil {
		t.Fatalf("Failed to write txt file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadDirectory(tmpDir)
	if err != nil {
		t.Fatalf("LoadDirectory() failed: %v", err)
	}

	tools, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	// Should only load the YAML file, not the txt file
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool (YAML only), got %d", len(tools))
	}
}

func TestYAMLToolRepository_LoadDirectory_ErrorInFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid YAML tool (should be silently skipped)
	invalidFile := filepath.Join(tmpDir, "invalid.yaml")
	invalidContent := `this is: [not valid yaml`
	err := os.WriteFile(invalidFile, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	// Create valid YAML tool
	validFile := filepath.Join(tmpDir, "valid.yaml")
	validContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: valid_tool
spec:
  description: Valid tool
  input_schema:
    type: object
  mode: mock
`
	err = os.WriteFile(validFile, []byte(validContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write valid file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	// Should not return error even if one file is invalid
	err = repo.LoadDirectory(tmpDir)
	if err != nil {
		t.Fatalf("LoadDirectory() should not fail on invalid file: %v", err)
	}

	tools, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	// Should load only the valid tool
	if len(tools) != 1 {
		t.Errorf("Expected 1 tool (valid only), got %d", len(tools))
	}
}

func TestYAMLToolRepository_LoadDirectory_BothYAMLExtensions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .yaml file
	yamlFile := filepath.Join(tmpDir, "tool1.yaml")
	yamlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: tool1
spec:
  description: Tool with .yaml
  input_schema:
    type: object
  mode: mock
`
	err := os.WriteFile(yamlFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write .yaml file: %v", err)
	}

	// Create .yml file
	ymlFile := filepath.Join(tmpDir, "tool2.yml")
	ymlContent := `apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: tool2
spec:
  description: Tool with .yml
  input_schema:
    type: object
  mode: mock
`
	err = os.WriteFile(ymlFile, []byte(ymlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write .yml file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadDirectory(tmpDir)
	if err != nil {
		t.Fatalf("LoadDirectory() failed: %v", err)
	}

	tools, err := repo.ListTools()
	if err != nil {
		t.Fatalf("ListTools() failed: %v", err)
	}

	// Should load both .yaml and .yml files
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools (.yaml and .yml), got %d", len(tools))
	}
}

func TestYAMLToolRepository_RegisterTool(t *testing.T) {
	repo := NewYAMLToolRepository("/tmp/test")

	descriptor := &tools.ToolDescriptor{
		Name:        "registered_tool",
		Description: "Manually registered tool",
		Mode:        "mock",
	}

	repo.RegisterTool("registered_tool", descriptor)

	loadedDescriptor, err := repo.LoadTool("registered_tool")
	if err != nil {
		t.Fatalf("LoadTool() after RegisterTool failed: %v", err)
	}

	if loadedDescriptor.Name != "registered_tool" {
		t.Errorf("Expected name 'registered_tool', got '%s'", loadedDescriptor.Name)
	}

	if loadedDescriptor.Description != "Manually registered tool" {
		t.Errorf("Expected description 'Manually registered tool', got '%s'", loadedDescriptor.Description)
	}
}

func TestYAMLToolRepository_LoadToolFromFile_ReadError(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewYAMLToolRepository(tmpDir)

	// Try to load non-existent file
	err := repo.LoadToolFromFile(filepath.Join(tmpDir, "nonexistent.yaml"))
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestYAMLToolRepository_InvalidYAMLStructure(t *testing.T) {
	tmpDir := t.TempDir()
	toolFile := filepath.Join(tmpDir, "invalid-structure.yaml")

	// Valid YAML but not a map (top-level array)
	yamlContent := `- item1
- item2
- item3
`

	err := os.WriteFile(toolFile, []byte(yamlContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	repo := NewYAMLToolRepository(tmpDir)
	err = repo.LoadToolFromFile(toolFile)
	if err == nil {
		t.Error("Expected error for invalid YAML structure (not a map)")
	}
}
