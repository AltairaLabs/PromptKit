// Package common provides shared functionality for persistence repositories.
package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// UnmarshalFunc is a function that unmarshals data into a prompt config
type UnmarshalFunc func([]byte, interface{}) error

// MarshalFunc is a function that marshals a prompt config to bytes
type MarshalFunc func(interface{}) ([]byte, error)

// BasePromptRepository provides common prompt repository functionality
type BasePromptRepository struct {
	BasePath       string
	TaskTypeToFile map[string]string
	Cache          map[string]*prompt.Config
	Extensions     []string
	Unmarshal      UnmarshalFunc
	Marshal        MarshalFunc
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
	err := filepath.WalkDir(r.BasePath, func(path string, d os.DirEntry, err error) error {
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
	if err != nil {
		logger.Debug("Failed to walk directory for filename search",
			"base_path", r.BasePath, "task_type", taskType, "error", err)
	}

	return foundFile
}

// SearchByContent searches for files by parsing and checking task type
func (r *BasePromptRepository) SearchByContent(taskType string) string {
	var foundFile string
	err := filepath.WalkDir(r.BasePath, func(path string, d os.DirEntry, err error) error {
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
	if err != nil {
		logger.Debug("Failed to walk directory for content search",
			"base_path", r.BasePath, "task_type", taskType, "error", err)
	}

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

// File permission constants for persistence operations
const (
	DirPerm  = 0o750 // Directory permissions: rwxr-x---
	FilePerm = 0o600 // File permissions: rw-------
)

// SavePrompt saves a prompt configuration to disk
func (r *BasePromptRepository) SavePrompt(config *prompt.Config) error {
	if r.Marshal == nil {
		return fmt.Errorf("save not supported: marshal function not configured")
	}
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.Spec.TaskType == "" {
		return fmt.Errorf("task_type cannot be empty")
	}

	// Determine output file path
	filePath := r.resolveOutputPath(config.Spec.TaskType)

	// Marshal config to bytes
	data, err := r.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(filePath, data, FilePerm); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	// Update cache
	r.Cache[config.Spec.TaskType] = config

	// Update mapping if not already present
	if _, exists := r.TaskTypeToFile[config.Spec.TaskType]; !exists {
		r.TaskTypeToFile[config.Spec.TaskType] = filePath
	}

	return nil
}

// resolveOutputPath determines the file path for saving a prompt
func (r *BasePromptRepository) resolveOutputPath(taskType string) string {
	// Check explicit mapping first
	if filePath, ok := r.TaskTypeToFile[taskType]; ok {
		if filepath.IsAbs(filePath) {
			return filePath
		}
		return filepath.Join(r.BasePath, filePath)
	}

	// Default to basePath/taskType with first extension
	ext := ".yaml"
	if len(r.Extensions) > 0 {
		ext = r.Extensions[0]
	}
	return filepath.Join(r.BasePath, taskType+ext)
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
