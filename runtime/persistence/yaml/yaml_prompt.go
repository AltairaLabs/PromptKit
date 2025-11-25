// Package yaml provides YAML file-based implementations of persistence repositories.
//
// This package is primarily for Arena and development use, loading prompts and tools
// from YAML configuration files on disk.
package yaml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// Compile-time interface check
var _ persistence.PromptRepository = (*YAMLPromptRepository)(nil)

// YAMLPromptRepository loads prompts from YAML files on disk
type YAMLPromptRepository struct {
	basePath       string
	taskTypeToFile map[string]string // Explicit mappings
	cache          map[string]*prompt.PromptConfig
}

// NewYAMLPromptRepository creates a YAML file-based prompt repository
// If taskTypeToFile mappings are provided, they will be used for lookups.
// Otherwise, the repository will search the basePath directory.
func NewYAMLPromptRepository(basePath string, taskTypeToFile map[string]string) *YAMLPromptRepository {
	if taskTypeToFile == nil {
		taskTypeToFile = make(map[string]string)
	}
	return &YAMLPromptRepository{
		basePath:       basePath,
		taskTypeToFile: taskTypeToFile,
		cache:          make(map[string]*prompt.PromptConfig),
	}
}

// LoadPrompt loads a prompt configuration by task type
func (r *YAMLPromptRepository) LoadPrompt(taskType string) (*prompt.PromptConfig, error) {
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

	// Parse YAML
	var config prompt.PromptConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate
	if err := validatePromptConfig(&config); err != nil {
		return nil, err
	}

	// Cache and return
	r.cache[taskType] = &config
	return &config, nil
}

// resolveFilePath finds the file path for a given task type
func (r *YAMLPromptRepository) resolveFilePath(taskType string) (string, error) {
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

// searchForPrompt searches for a YAML file matching the task type
func (r *YAMLPromptRepository) searchForPrompt(taskType string) (string, error) {
	// First try filename-based search
	if foundFile := r.searchByFilename(taskType); foundFile != "" {
		return foundFile, nil
	}

	// Then try content-based search
	if foundFile := r.searchByContent(taskType); foundFile != "" {
		return foundFile, nil
	}

	return "", fmt.Errorf("no YAML file found for task type: %s", taskType)
}

func (r *YAMLPromptRepository) searchByFilename(taskType string) string {
	patterns := []string{
		fmt.Sprintf("%s.yaml", taskType),
		fmt.Sprintf("%s.yml", taskType),
		fmt.Sprintf("%s.v*.yaml", taskType),
		fmt.Sprintf("%s.v*.yml", taskType),
	}

	var foundFile string
	_ = filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		for _, pattern := range patterns {
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched { // NOSONAR: Pattern syntax is controlled and valid
				foundFile = path
				return filepath.SkipAll
			}
		}
		return nil
	})

	return foundFile
}

func (r *YAMLPromptRepository) searchByContent(taskType string) string {
	var foundFile string
	_ = filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if !r.isYAMLFile(path) {
			return nil
		}

		if r.hasMatchingTaskType(path, taskType) {
			foundFile = path
			return filepath.SkipAll
		}

		return nil
	})

	return foundFile
}

func (r *YAMLPromptRepository) isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == yamlExt || ext == ymlExt
}

func (r *YAMLPromptRepository) hasMatchingTaskType(path, taskType string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var config prompt.PromptConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return false
	}

	return config.Spec.TaskType == taskType
}

// LoadFragment loads a fragment by name and optional path
func (r *YAMLPromptRepository) LoadFragment(name, relativePath, baseDir string) (*prompt.Fragment, error) {
	// Determine fragment path
	var fragmentPath string
	if relativePath != "" {
		fragmentPath = filepath.Join(baseDir, relativePath)
	} else {
		// Default to fragments/ subdirectory with .yaml extension
		fragmentPath = filepath.Join(r.basePath, "fragments", name+".yaml")
	}

	// Read and parse
	data, err := os.ReadFile(fragmentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read fragment: %w", err)
	}

	var fragment prompt.Fragment
	if err := yaml.Unmarshal(data, &fragment); err != nil {
		return nil, fmt.Errorf("failed to parse fragment YAML: %w", err)
	}

	fragment.SourceFile = fragmentPath
	return &fragment, nil
}

// ListPrompts returns all available prompt task types
func (r *YAMLPromptRepository) ListPrompts() ([]string, error) {
	taskTypes := []string{}

	// If we have explicit mappings, use those
	if len(r.taskTypeToFile) > 0 {
		for taskType := range r.taskTypeToFile {
			taskTypes = append(taskTypes, taskType)
		}
		return taskTypes, nil
	}

	// Otherwise, scan directory and load configs to extract task types
	err := filepath.WalkDir(r.basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		// Only check YAML files
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Try to parse and extract task type
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var config prompt.PromptConfig
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil
		}

		if config.Spec.TaskType != "" {
			taskTypes = append(taskTypes, config.Spec.TaskType)
		}

		return nil
	})

	return taskTypes, err
}

// SavePrompt saves a prompt configuration (not yet implemented)
func (r *YAMLPromptRepository) SavePrompt(config *prompt.PromptConfig) error {
	return fmt.Errorf("not implemented")
}

// validatePromptConfig validates the prompt configuration structure
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
