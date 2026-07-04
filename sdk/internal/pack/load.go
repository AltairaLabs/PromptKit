// Package pack provides SDK-side loading and conversion for PromptPacks.
//
// The pack data types are the runtime's canonical types, aliased in alias.go.
// This file adds the behavior the SDK needs on top of them: schema-validated
// loading, and conversion into the runtime prompt.Registry / tool repository
// consumed by the pipeline.
package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// LoadOptions configures pack loading behavior.
type LoadOptions struct {
	// SkipSchemaValidation disables JSON schema validation during load.
	// Default is false (validation enabled).
	SkipSchemaValidation bool
}

// Load loads a pack from a JSON file.
// By default, the pack is validated against the PromptPack JSON schema.
// Use LoadOptions to customize behavior.
func Load(path string, opts ...LoadOptions) (*Pack, error) {
	// Merge options (last wins)
	var options LoadOptions
	for _, opt := range opts {
		options = opt
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	data, err := os.ReadFile(absPath) //nolint:gosec // Path is resolved to absolute
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pack not found: %s", absPath)
		}
		return nil, fmt.Errorf("failed to read pack: %w", err)
	}

	// Validate against schema unless skipped
	if !options.SkipSchemaValidation {
		if validationErr := ValidateAgainstSchema(data); validationErr != nil {
			return nil, validationErr
		}
	}

	pack, err := Parse(data)
	if err != nil {
		return nil, err
	}
	pack.FilePath = absPath

	return pack, nil
}

// Parse parses pack JSON data.
func Parse(data []byte) (*Pack, error) {
	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("failed to parse pack JSON: %w", err)
	}

	// Validate basic structure
	if len(pack.Prompts) == 0 {
		return nil, fmt.Errorf("pack contains no prompts")
	}

	// Validate workflow section if present
	if err := validateWorkflowSection(&pack); err != nil {
		return nil, err
	}

	// Validate agents section if present
	if err := validateAgentsSection(&pack); err != nil {
		return nil, err
	}

	return &pack, nil
}

// ToPromptRegistry builds a prompt.Registry from the pack so the SDK uses the
// same PromptAssemblyMiddleware as Arena.
func ToPromptRegistry(p *Pack) *prompt.Registry {
	repo := memory.NewPromptRepository()

	for taskType, packPrompt := range p.Prompts {
		repo.RegisterPrompt(taskType, packPrompt.ToPromptConfig(taskType))
	}

	for name, content := range p.Fragments {
		repo.RegisterFragment(name, &prompt.Fragment{
			Type:    "text",
			Content: content,
		})
	}

	return prompt.NewRegistryWithRepository(repo)
}

// ToToolRepository builds a memory.ToolRepository from the pack so the SDK uses
// the same tools.Registry as Arena. Pack tools are registered with mode "local"
// as a placeholder; the host binds the real execution mode.
func ToToolRepository(p *Pack) *memory.ToolRepository {
	repo := memory.NewToolRepository()

	for name, tool := range p.Tools {
		paramsJSON, err := json.Marshal(tool.Parameters)
		if err != nil {
			continue
		}

		desc := &tools.ToolDescriptor{
			Name:        name,
			Description: tool.Description,
			InputSchema: paramsJSON,
			Mode:        "local",
		}
		_ = repo.SaveTool(desc)
	}

	return repo
}
