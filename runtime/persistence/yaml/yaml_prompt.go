// Package yaml provides YAML file-based implementations of persistence repositories.
//
// This package is primarily for Arena and development use, loading prompts and tools
// from YAML configuration files on disk.
package yaml

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/persistence"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/common"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// Compile-time interface check
var _ persistence.PromptRepository = (*PromptRepository)(nil)

// YAMLPromptRepository loads prompts from YAML files on disk
type PromptRepository struct {
	*common.BasePromptRepository
}

// NewYAMLPromptRepository creates a YAML file-based prompt repository
// If taskTypeToFile mappings are provided, they will be used for lookups.
// Otherwise, the repository will search the basePath directory.
func NewYAMLPromptRepository(basePath string, taskTypeToFile map[string]string) *PromptRepository {
	return &PromptRepository{
		BasePromptRepository: common.NewBasePromptRepository(
			basePath,
			taskTypeToFile,
			[]string{yamlExt, ymlExt},
			func(data []byte, v interface{}) error {
				return yaml.Unmarshal(data, v)
			},
		),
	}
}

// LoadPrompt is inherited from BasePromptRepository

// LoadFragment loads a fragment by name and optional path
func (r *PromptRepository) LoadFragment(name, relativePath, baseDir string) (*prompt.Fragment, error) {
	// Determine fragment path
	var fragmentPath string
	if relativePath != "" {
		fragmentPath = filepath.Join(baseDir, relativePath)
	} else {
		// Default to fragments/ subdirectory with .yaml extension
		fragmentPath = filepath.Join(r.BasePath, "fragments", name+".yaml")
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

// ListPrompts is inherited from BasePromptRepository

// SavePrompt saves a prompt configuration (not yet implemented)
func (r *PromptRepository) SavePrompt(config *prompt.Config) error {
	return fmt.Errorf("not implemented")
}
