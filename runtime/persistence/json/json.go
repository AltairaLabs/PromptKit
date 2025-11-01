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
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Compile-time interface checks
var (
	_ persistence.PromptRepository = (*JSONPromptRepository)(nil)
	_ persistence.ToolRepository   = (*JSONToolRepository)(nil)
)

// JSONPromptRepository loads prompts from JSON files on disk
type JSONPromptRepository struct {
	basePath       string
	taskTypeToFile map[string]string
	cache          map[string]*prompt.PromptConfig
}

// NewJSONPromptRepository creates a JSON file-based prompt repository
func NewJSONPromptRepository(basePath string, taskTypeToFile map[string]string) *JSONPromptRepository {
	if taskTypeToFile == nil {
		taskTypeToFile = make(map[string]string)
	}
	return &JSONPromptRepository{
		basePath:       basePath,
		taskTypeToFile: taskTypeToFile,
		cache:          make(map[string]*prompt.PromptConfig),
	}
}

// LoadPrompt loads a prompt configuration by task type
func (r *JSONPromptRepository) LoadPrompt(taskType string) (*prompt.PromptConfig, error) {
	// Check cache
	if cached, ok := r.cache[taskType]; ok {
		return cached, nil
	}

	// Resolve file path
	filePath, err := r.resolveFilePath(taskType)
	if err != nil {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse JSON
	var config prompt.PromptConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate
	if err := validatePromptConfig(&config); err != nil {
		return nil, err
	}

	// Cache and return
	r.cache[taskType] = &config
	return &config, nil
}

func (r *JSONPromptRepository) resolveFilePath(taskType string) (string, error) {
	// Check explicit mapping first
	if filePath, ok := r.taskTypeToFile[taskType]; ok {
		if !filepath.IsAbs(filePath) {
			return filepath.Join(r.basePath, filePath), nil
		}
		return filePath, nil
	}

	// Search for matching file
	return r.searchForPrompt(taskType)
}

func (r *JSONPromptRepository) searchForPrompt(taskType string) (string, error) {
	patterns := []string{
		fmt.Sprintf("%s.json", taskType),
		fmt.Sprintf("%s.v*.json", taskType),
	}

	var foundFile string
	_ = filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		for _, pattern := range patterns {
			matched, _ := filepath.Match(pattern, filepath.Base(path))
			if matched {
				foundFile = path
				return filepath.SkipAll
			}
		}
		return nil
	})

	if foundFile != "" {
		return foundFile, nil
	}

	// Search by content
	_ = filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var config prompt.PromptConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil
		}

		if config.Spec.TaskType == taskType {
			foundFile = path
			return filepath.SkipAll
		}

		return nil
	})

	if foundFile == "" {
		return "", fmt.Errorf("no JSON file found for task type: %s", taskType)
	}

	return foundFile, nil
}

// LoadFragment loads a fragment by name
func (r *JSONPromptRepository) LoadFragment(name string, relativePath string, baseDir string) (*prompt.Fragment, error) {
	var fragmentPath string
	if relativePath != "" {
		fragmentPath = filepath.Join(baseDir, relativePath)
	} else {
		fragmentPath = filepath.Join(r.basePath, "fragments", name+".json")
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

// ListPrompts returns all available prompt task types
func (r *JSONPromptRepository) ListPrompts() ([]string, error) {
	taskTypes := []string{}

	if len(r.taskTypeToFile) > 0 {
		for taskType := range r.taskTypeToFile {
			taskTypes = append(taskTypes, taskType)
		}
		return taskTypes, nil
	}

	_ = filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var config prompt.PromptConfig
		if err := json.Unmarshal(data, &config); err != nil {
			return nil
		}

		if config.Spec.TaskType != "" {
			taskTypes = append(taskTypes, config.Spec.TaskType)
		}

		return nil
	})

	return taskTypes, nil
}

// SavePrompt saves a prompt configuration (not yet implemented)
func (r *JSONPromptRepository) SavePrompt(config *prompt.PromptConfig) error {
	return fmt.Errorf("not implemented")
}

func validatePromptConfig(config *prompt.PromptConfig) error {
	if config.APIVersion == "" {
		return fmt.Errorf("missing apiVersion")
	}
	if config.Kind != "PromptConfig" {
		return fmt.Errorf("invalid kind: expected PromptConfig, got %s", config.Kind)
	}
	if config.Spec.TaskType == "" {
		return fmt.Errorf("missing spec.task_type")
	}
	return nil
}

// JSONToolRepository loads tools from JSON files on disk
type JSONToolRepository struct {
	basePath string
	tools    map[string]*tools.ToolDescriptor
}

// NewJSONToolRepository creates a JSON file-based tool repository
func NewJSONToolRepository(basePath string) *JSONToolRepository {
	return &JSONToolRepository{
		basePath: basePath,
		tools:    make(map[string]*tools.ToolDescriptor),
	}
}

// LoadTool loads a tool descriptor by name
func (r *JSONToolRepository) LoadTool(name string) (*tools.ToolDescriptor, error) {
	descriptor, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return descriptor, nil
}

// ListTools returns all available tool names
func (r *JSONToolRepository) ListTools() ([]string, error) {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names, nil
}

// SaveTool saves a tool descriptor (not yet implemented)
func (r *JSONToolRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	return fmt.Errorf("not implemented")
}

// LoadToolFromFile loads a tool from a JSON file
func (r *JSONToolRepository) LoadToolFromFile(filename string) error {
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
func (r *JSONToolRepository) LoadDirectory(dirPath string) error {
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
func (r *JSONToolRepository) RegisterTool(name string, descriptor *tools.ToolDescriptor) {
	r.tools[name] = descriptor
}
