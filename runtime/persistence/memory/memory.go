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
	_ persistence.PromptRepository = (*MemoryPromptRepository)(nil)
	_ persistence.ToolRepository   = (*MemoryToolRepository)(nil)
)

// MemoryPromptRepository stores prompts in memory (for testing/SDK)
type MemoryPromptRepository struct {
	prompts   map[string]*prompt.PromptConfig
	fragments map[string]*prompt.Fragment
}

// NewMemoryPromptRepository creates a new in-memory prompt repository
func NewMemoryPromptRepository() *MemoryPromptRepository {
	return &MemoryPromptRepository{
		prompts:   make(map[string]*prompt.PromptConfig),
		fragments: make(map[string]*prompt.Fragment),
	}
}

// LoadPrompt loads a prompt configuration by task type
func (r *MemoryPromptRepository) LoadPrompt(taskType string) (*prompt.PromptConfig, error) {
	config, ok := r.prompts[taskType]
	if !ok {
		return nil, fmt.Errorf("prompt not found: %s", taskType)
	}
	return config, nil
}

// LoadFragment loads a fragment by name
func (r *MemoryPromptRepository) LoadFragment(name string, relativePath string, baseDir string) (*prompt.Fragment, error) {
	fragment, ok := r.fragments[name]
	if !ok {
		return nil, fmt.Errorf("fragment not found: %s", name)
	}
	return fragment, nil
}

// ListPrompts returns all available prompt task types
func (r *MemoryPromptRepository) ListPrompts() ([]string, error) {
	taskTypes := make([]string, 0, len(r.prompts))
	for taskType := range r.prompts {
		taskTypes = append(taskTypes, taskType)
	}
	return taskTypes, nil
}

// SavePrompt saves a prompt configuration
func (r *MemoryPromptRepository) SavePrompt(config *prompt.PromptConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}
	if config.Spec.TaskType == "" {
		return fmt.Errorf("task_type cannot be empty")
	}
	r.prompts[config.Spec.TaskType] = config
	return nil
}

// RegisterPrompt adds a prompt to the in-memory store
func (r *MemoryPromptRepository) RegisterPrompt(taskType string, config *prompt.PromptConfig) {
	r.prompts[taskType] = config
}

// RegisterFragment adds a fragment to the in-memory store
func (r *MemoryPromptRepository) RegisterFragment(name string, fragment *prompt.Fragment) {
	r.fragments[name] = fragment
}

// MemoryToolRepository stores tools in memory (for testing/SDK)
type MemoryToolRepository struct {
	tools map[string]*tools.ToolDescriptor
}

// NewMemoryToolRepository creates a new in-memory tool repository
func NewMemoryToolRepository() *MemoryToolRepository {
	return &MemoryToolRepository{
		tools: make(map[string]*tools.ToolDescriptor),
	}
}

// LoadTool loads a tool descriptor by name
func (r *MemoryToolRepository) LoadTool(name string) (*tools.ToolDescriptor, error) {
	descriptor, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return descriptor, nil
}

// ListTools returns all available tool names
func (r *MemoryToolRepository) ListTools() ([]string, error) {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names, nil
}

// SaveTool saves a tool descriptor
func (r *MemoryToolRepository) SaveTool(descriptor *tools.ToolDescriptor) error {
	if descriptor == nil {
		return fmt.Errorf("descriptor cannot be nil")
	}
	if descriptor.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	r.tools[descriptor.Name] = descriptor
	return nil
}

// RegisterTool adds a tool to the in-memory store
func (r *MemoryToolRepository) RegisterTool(name string, descriptor *tools.ToolDescriptor) {
	r.tools[name] = descriptor
}
