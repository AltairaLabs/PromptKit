// Package memory provides in-memory implementations of persistence repositories.
//
// This package is primarily for testing and SDK use, allowing prompts and tools
// to be registered programmatically without file system dependencies.
package memory

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Compile-time interface checks
var (
	_ persistence.PromptRepository = (*PromptRepository)(nil)
	_ persistence.ToolRepository   = (*ToolRepository)(nil)
)

// PromptRepository stores prompts in memory (for testing/SDK)
type PromptRepository struct {
	prompts   map[string]*prompt.Config
	fragments map[string]*prompt.Fragment
}

// NewPromptRepository creates a new in-memory prompt repository
func NewPromptRepository() *PromptRepository {
	return &PromptRepository{
		prompts:   make(map[string]*prompt.Config),
		fragments: make(map[string]*prompt.Fragment),
	}
}

// LoadPrompt loads a prompt configuration by task type
func (r *PromptRepository) LoadPrompt(taskType string) (*prompt.Config, error) {
	config, ok := r.prompts[taskType]
	if !ok {
		return nil, fmt.Errorf("prompt not found: %s", taskType)
	}
	return config, nil
}

// LoadFragment loads a fragment by name
func (r *PromptRepository) LoadFragment(name, relativePath, baseDir string) (*prompt.Fragment, error) {
	fragment, ok := r.fragments[name]
	if !ok {
		return nil, fmt.Errorf("fragment not found: %s", name)
	}
	return fragment, nil
}

// ListPrompts returns all available prompt task types
func (r *PromptRepository) ListPrompts() ([]string, error) {
	taskTypes := make([]string, 0, len(r.prompts))
	for taskType := range r.prompts {
		taskTypes = append(taskTypes, taskType)
	}
	return taskTypes, nil
}

// SavePrompt saves a prompt configuration
func (r *PromptRepository) SavePrompt(config *prompt.Config) error {
	if config == nil {
		return persistence.ErrNilConfig
	}
	if config.Spec.TaskType == "" {
		return persistence.ErrEmptyTaskType
	}
	r.prompts[config.Spec.TaskType] = config
	return nil
}

// RegisterPrompt adds a prompt to the in-memory store
func (r *PromptRepository) RegisterPrompt(taskType string, config *prompt.Config) {
	r.prompts[taskType] = config
}

// RegisterFragment adds a fragment to the in-memory store
func (r *PromptRepository) RegisterFragment(name string, fragment *prompt.Fragment) {
	r.fragments[name] = fragment
}

// ToolRepository stores tools in memory (for testing/SDK)
type ToolRepository struct {
	tools map[string]*tools.ToolDescriptor
}

// NewToolRepository creates a new in-memory tool repository
func NewToolRepository() *ToolRepository {
	return &ToolRepository{
		tools: make(map[string]*tools.ToolDescriptor),
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

// SaveTool saves a tool descriptor
func (r *ToolRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	if descriptor == nil {
		return persistence.ErrNilDescriptor
	}
	if descriptor.Name == "" {
		return persistence.ErrEmptyToolName
	}
	r.tools[descriptor.Name] = descriptor
	return nil
}

// RegisterTool adds a tool to the in-memory store
func (r *ToolRepository) RegisterTool(name string, descriptor *tools.ToolDescriptor) {
	r.tools[name] = descriptor
}
