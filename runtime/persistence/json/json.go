// Package json provides JSON file-based implementations of persistence repositories.
//
// This package can be used for production environments where JSON is preferred over YAML.
package json

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/common"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const jsonExt = ".json"

// Compile-time interface checks
var (
	_ persistence.PromptRepository = (*PromptRepository)(nil)
	_ persistence.ToolRepository   = (*ToolRepository)(nil)
)

// JSONPromptRepository loads prompts from JSON files on disk
type PromptRepository struct {
	*common.BasePromptRepository
}

// NewJSONPromptRepository creates a JSON file-based prompt repository
func NewJSONPromptRepository(basePath string, taskTypeToFile map[string]string) *PromptRepository {
	return &PromptRepository{
		BasePromptRepository: common.NewBasePromptRepository(
			basePath,
			taskTypeToFile,
			[]string{jsonExt},
			func(data []byte, v interface{}) error {
				return json.Unmarshal(data, v)
			},
		),
	}
}

// LoadPrompt is inherited from BasePromptRepository

// LoadFragment loads a fragment by name
func (r *PromptRepository) LoadFragment(name, relativePath, baseDir string) (*prompt.Fragment, error) {
	var fragmentPath string
	if relativePath != "" {
		fragmentPath = filepath.Join(baseDir, relativePath)
	} else {
		fragmentPath = filepath.Join(r.BasePath, "fragments", name+".json")
	}

	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read fragment: %w", err)
	}

	var fragment prompt.Fragment
	if err := json.Unmarshal(data, &fragment); err != nil {
		return nil, fmt.Errorf("failed to parse fragment JSON: %w", err)
	}

	fragment.SourceFile = fragmentPath
	return &fragment, nil
}

// ListPrompts is inherited from BasePromptRepository

// SavePrompt saves a prompt configuration (not yet implemented)
func (r *PromptRepository) SavePrompt(config *prompt.Config) error {
	return fmt.Errorf("not implemented")
}

// JSONToolRepository loads tools from JSON files on disk
type ToolRepository struct {
	basePath string
	tools    map[string]*tools.ToolDescriptor
}

// NewJSONToolRepository creates a JSON file-based tool repository
func NewJSONToolRepository(basePath string) *ToolRepository {
	return &ToolRepository{
		basePath: basePath,
		tools:    make(map[string]*tools.ToolDescriptor),
	}
}

// LoadTool loads a tool descriptor by name
func (r *ToolRepository) LoadTool(name string) (*tools.ToolDescriptor, error) {
	descriptor, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return descriptor, nil
}

// ListTools returns all available tool names
func (r *ToolRepository) ListTools() ([]string, error) {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names, nil
}

// SaveTool saves a tool descriptor (not yet implemented)
func (r *ToolRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	return fmt.Errorf("not implemented")
}

// LoadToolFromFile loads a tool from a JSON file
func (r *ToolRepository) LoadToolFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read tool file %s: %w", filename, err)
	}

	// Try K8s-style manifest first
	var temp map[string]interface{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return fmt.Errorf("failed to parse JSON tool file %s: %w", filename, err)
	}

	if apiVersion, hasAPI := temp["apiVersion"].(string); hasAPI && apiVersion != "" {
		// K8s manifest
		var toolConfig tools.ToolConfig
		if err := json.Unmarshal(data, &toolConfig); err != nil {
			return fmt.Errorf("failed to unmarshal K8s manifest %s: %w", filename, err)
		}

		if toolConfig.Kind != "Tool" {
			return fmt.Errorf("invalid kind: expected 'Tool', got '%s'", toolConfig.Kind)
		}
		if toolConfig.Metadata.Name == "" {
			return fmt.Errorf("missing metadata.name")
		}

		toolConfig.Spec.Name = toolConfig.Metadata.Name
		r.tools[toolConfig.Spec.Name] = &toolConfig.Spec
		return nil
	}

	// Legacy format
	var descriptor tools.ToolDescriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return fmt.Errorf("failed to unmarshal JSON for %s: %w", filename, err)
	}

	if descriptor.Name == "" {
		return fmt.Errorf("tool descriptor missing name")
	}

	r.tools[descriptor.Name] = &descriptor
	return nil
}

// LoadDirectory recursively loads all JSON tool files from a directory
func (r *ToolRepository) LoadDirectory(dirPath string) error {
	return filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" {
			return nil
		}

		_ = r.LoadToolFromFile(path)
		return nil
	})
}

// RegisterTool adds a tool descriptor directly
func (r *ToolRepository) RegisterTool(name string, descriptor *tools.ToolDescriptor) {
	r.tools[name] = descriptor
}
