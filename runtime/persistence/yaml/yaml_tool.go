package yaml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/common"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

const (
	yamlExt = ".yaml"
	ymlExt  = ".yml"
)

// Compile-time interface check
var _ persistence.ToolRepository = (*ToolRepository)(nil)

// YAMLToolRepository loads tools from YAML files on disk
type ToolRepository struct {
	basePath string
	tools    map[string]*tools.ToolDescriptor
}

// NewYAMLToolRepository creates a YAML file-based tool repository
func NewYAMLToolRepository(basePath string) *ToolRepository {
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

// SaveTool saves a tool descriptor to a YAML file using K8s manifest format.
// The file will be named <tool-name>.yaml in the repository's base path.
func (r *ToolRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	if descriptor == nil {
		return fmt.Errorf("descriptor cannot be nil")
	}
	if descriptor.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	// Build K8s manifest format
	toolConfig := tools.ToolConfig{
		APIVersion: "promptkit.altairalabs.io/v1",
		Kind:       "Tool",
		Spec:       *descriptor,
	}
	toolConfig.Metadata.Name = descriptor.Name

	// Marshal to YAML
	data, err := yaml.Marshal(toolConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal tool config: %w", err)
	}

	// Determine output file path
	filePath := filepath.Join(r.basePath, descriptor.Name+yamlExt)

	// Ensure directory exists
	if err := os.MkdirAll(r.basePath, common.DirPerm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", r.basePath, err)
	}

	// Write file
	if err := os.WriteFile(filePath, data, common.FilePerm); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	// Update in-memory cache
	r.tools[descriptor.Name] = descriptor

	return nil
}

// LoadToolFromFile loads a tool from a YAML file and registers it
func (r *ToolRepository) LoadToolFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read tool file %s: %w", filename, err)
	}

	temp, err := r.parseYAML(filename, data)
	if err != nil {
		return err
	}

	tempMap, ok := temp.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid YAML structure in %s", filename)
	}

	// Try K8s manifest first, fall back to legacy format
	if r.isK8sManifest(tempMap) {
		return r.loadK8sManifest(filename, temp)
	}

	return r.loadLegacyTool(filename, temp)
}

func (r *ToolRepository) parseYAML(filename string, data []byte) (interface{}, error) {
	var temp interface{}
	if err := yaml.Unmarshal(data, &temp); err != nil {
		return nil, fmt.Errorf("failed to parse YAML tool file %s: %w", filename, err)
	}
	return temp, nil
}

func (r *ToolRepository) isK8sManifest(tempMap map[string]interface{}) bool {
	apiVersion, hasAPI := tempMap["apiVersion"].(string)
	return hasAPI && apiVersion != ""
}

func (r *ToolRepository) loadK8sManifest(filename string, temp interface{}) error {
	toolConfig, err := r.convertToToolConfig(filename, temp)
	if err != nil {
		return err
	}

	if err := r.validateK8sManifest(filename, toolConfig); err != nil {
		return err
	}

	// Use metadata.name as tool name
	toolConfig.Spec.Name = toolConfig.Metadata.Name
	r.tools[toolConfig.Spec.Name] = &toolConfig.Spec
	return nil
}

func (r *ToolRepository) convertToToolConfig(filename string, temp interface{}) (*tools.ToolConfig, error) {
	jsonData, err := json.Marshal(temp)
	if err != nil {
		return nil, fmt.Errorf("failed to convert K8s manifest to JSON for %s: %w", filename, err)
	}

	var toolConfig tools.ToolConfig
	if err := json.Unmarshal(jsonData, &toolConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal K8s manifest %s: %w", filename, err)
	}

	return &toolConfig, nil
}

func (r *ToolRepository) validateK8sManifest(filename string, toolConfig *tools.ToolConfig) error {
	if toolConfig.Kind == "" {
		return fmt.Errorf("tool config %s is missing kind", filename)
	}
	if toolConfig.Kind != "Tool" {
		return fmt.Errorf("tool config %s has invalid kind: expected 'Tool', got '%s'", filename, toolConfig.Kind)
	}
	if toolConfig.Metadata.Name == "" {
		return fmt.Errorf("tool config %s is missing metadata.name", filename)
	}
	return nil
}

func (r *ToolRepository) loadLegacyTool(filename string, temp interface{}) error {
	descriptor, err := r.convertToDescriptor(filename, temp)
	if err != nil {
		return err
	}

	if descriptor.Name == "" {
		return fmt.Errorf("tool descriptor %s is missing name", filename)
	}

	r.tools[descriptor.Name] = descriptor
	return nil
}

func (r *ToolRepository) convertToDescriptor(filename string, temp interface{}) (*tools.ToolDescriptor, error) {
	jsonData, err := json.Marshal(temp)
	if err != nil {
		return nil, fmt.Errorf("failed to convert YAML to JSON for %s: %w", filename, err)
	}

	var descriptor tools.ToolDescriptor
	if err := json.Unmarshal(jsonData, &descriptor); err != nil {
		return nil, fmt.Errorf("failed to unmarshal converted JSON for %s: %w", filename, err)
	}

	return &descriptor, nil
}

// LoadDirectory recursively loads all YAML tool files from a directory
func (r *ToolRepository) LoadDirectory(dirPath string) error {
	return filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Only load YAML files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != yamlExt && ext != ymlExt {
			return nil
		} // Try to load the tool
		if err := r.LoadToolFromFile(path); err != nil {
			// Log error but continue processing other files
			return nil
		}

		return nil
	})
}

// RegisterTool adds a tool descriptor directly to the repository
func (r *ToolRepository) RegisterTool(name string, descriptor *tools.ToolDescriptor) {
	r.tools[name] = descriptor
}
