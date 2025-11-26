// Package common provides shared functionality for persistence repositories.
package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// UnmarshalFunc is a function that unmarshals data into a prompt config
type UnmarshalFunc func([]byte, interface{}) error

// BasePromptRepository provides common prompt repository functionality
type BasePromptRepository struct {
	BasePath       string
	TaskTypeToFile map[string]string
	Cache          map[string]*prompt.Config
	Extensions     []string
	Unmarshal      UnmarshalFunc
}

// NewBasePromptRepository creates a new base repository
func NewBasePromptRepository(basePath string, taskTypeToFile map[string]string, extensions []string, unmarshal UnmarshalFunc) *BasePromptRepository {
	if taskTypeToFile == nil {
		taskTypeToFile = make(map[string]string)
	}
	return &BasePromptRepository{
		BasePath:       basePath,
		TaskTypeToFile: taskTypeToFile,
		Cache:          make(map[string]*prompt.Config),
		Extensions:     extensions,
		Unmarshal:      unmarshal,
	}
}

// LoadPrompt loads a prompt configuration by task type
func (r *BasePromptRepository) LoadPrompt(taskType string) (*prompt.Config, error) {
	// Check cache
	if cached, ok := r.Cache[taskType]; ok {
		return cached, nil
	}

	// Resolve file path
	filePath, err := r.ResolveFilePath(taskType)
	if err != nil {
		return nil, err
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse
	var config prompt.Config
	if err := r.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	// Validate
	if err := ValidatePromptConfig(&config); err != nil {
		return nil, err
	}

	// Cache and return
	r.Cache[taskType] = &config
	return &config, nil
}

// ResolveFilePath finds the file path for a given task type
func (r *BasePromptRepository) ResolveFilePath(taskType string) (string, error) {
	// Check explicit mapping first
	if filePath, ok := r.TaskTypeToFile[taskType]; ok {
		if !filepath.IsAbs(filePath) {
			return filepath.Join(r.BasePath, filePath), nil
		}
		return filePath, nil
	}

	// Search for matching file
	return r.SearchForPrompt(taskType)
}

// SearchForPrompt searches for a file matching the task type
func (r *BasePromptRepository) SearchForPrompt(taskType string) (string, error) {
	// First try filename-based search
	if foundFile := r.SearchByFilename(taskType); foundFile != "" {
		return foundFile, nil
	}

	// Then try content-based search
	if foundFile := r.SearchByContent(taskType); foundFile != "" {
		return foundFile, nil
	}

	return "", fmt.Errorf("no file found for task type: %s", taskType)
}

// SearchByFilename searches for files by filename patterns
func (r *BasePromptRepository) SearchByFilename(taskType string) string {
	var patterns []string
	for _, ext := range r.Extensions {
		patterns = append(patterns, fmt.Sprintf("%s%s", taskType, ext))
		patterns = append(patterns, fmt.Sprintf("%s.v*%s", taskType, ext))
	}

	var foundFile string
	_ = filepath.WalkDir(r.BasePath, func(path string, d os.DirEntry, err error) error {
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

// SearchByContent searches for files by parsing and checking task type
func (r *BasePromptRepository) SearchByContent(taskType string) string {
	var foundFile string
	_ = filepath.WalkDir(r.BasePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if !r.HasValidExtension(path) {
			return nil
		}

		if r.HasMatchingTaskType(path, taskType) {
			foundFile = path
			return filepath.SkipAll
		}

		return nil
	})

	return foundFile
}

// HasValidExtension checks if a file has a valid extension
func (r *BasePromptRepository) HasValidExtension(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, validExt := range r.Extensions {
		if ext == validExt {
			return true
		}
	}
	return false
}

// HasMatchingTaskType checks if a file contains the specified task type
func (r *BasePromptRepository) HasMatchingTaskType(path, taskType string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var config prompt.Config
	if err := r.Unmarshal(data, &config); err != nil {
		return false
	}

	return config.Spec.TaskType == taskType
}

// ListPrompts returns all available prompt task types
func (r *BasePromptRepository) ListPrompts() ([]string, error) {
	// If we have explicit mappings, use those
	if len(r.TaskTypeToFile) > 0 {
		taskTypes := make([]string, 0, len(r.TaskTypeToFile))
		for taskType := range r.TaskTypeToFile {
			taskTypes = append(taskTypes, taskType)
		}
		return taskTypes, nil
	}

	// Otherwise, scan directory
	taskTypes := []string{}
	err := filepath.WalkDir(r.BasePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		if !r.HasValidExtension(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		var config prompt.Config
		if err := r.Unmarshal(data, &config); err != nil {
			return nil
		}

		if config.Spec.TaskType != "" {
			taskTypes = append(taskTypes, config.Spec.TaskType)
		}

		return nil
	})

	return taskTypes, err
}

// ValidatePromptConfig validates the prompt configuration structure
func ValidatePromptConfig(config *prompt.Config) error {
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
