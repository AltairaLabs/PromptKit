// Package persistence provides abstract persistence layer for Runtime components.
//
// This package implements the Repository Pattern to decouple Runtime from storage
// implementations. It provides interfaces for loading prompts, tools, and fragments
// from various backends (YAML files, JSON files, memory, packs, etc.).
package persistence

import (
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// PromptRepository provides abstract access to prompt configurations
type PromptRepository interface {
	// LoadPrompt loads a prompt configuration by task type
	LoadPrompt(taskType string) (*prompt.PromptConfig, error)

	// LoadFragment loads a fragment by name and optional path
	LoadFragment(name string, relativePath string, baseDir string) (*prompt.Fragment, error)

	// ListPrompts returns all available prompt task types
	ListPrompts() ([]string, error)

	// SavePrompt saves a prompt configuration (for future write support)
	SavePrompt(config *prompt.PromptConfig) error
}

// ToolRepository provides abstract access to tool descriptors
type ToolRepository interface {
	// LoadTool loads a tool descriptor by name
	LoadTool(name string) (*tools.ToolDescriptor, error)

	// ListTools returns all available tool names
	ListTools() ([]string, error)

	// SaveTool saves a tool descriptor (for future write support)
	SaveTool(descriptor *tools.ToolDescriptor) error
}
