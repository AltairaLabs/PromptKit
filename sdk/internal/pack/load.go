// Package pack provides internal pack loading functionality.
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

// Pack represents a loaded prompt pack.
// This is the SDK's view of a pack, optimized for runtime use.
type Pack struct {
	// Identity
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`

	// Prompts - Map of prompt name -> Prompt
	Prompts map[string]*Prompt `json:"prompts"`

	// Tools - Map of tool name -> Tool
	Tools map[string]*Tool `json:"tools,omitempty"`

	// Fragments - Map of fragment name -> content
	Fragments map[string]string `json:"fragments,omitempty"`

	// FilePath is the path from which this pack was loaded.
	FilePath string `json:"-"`
}

// Prompt represents a prompt definition within a pack.
type Prompt struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Version        string         `json:"version"`
	SystemTemplate string         `json:"system_template"`
	Variables      []Variable     `json:"variables,omitempty"`
	Tools          []string       `json:"tools,omitempty"`
	ToolPolicy     *ToolPolicy    `json:"tool_policy,omitempty"`
	MediaConfig    *MediaConfig   `json:"media,omitempty"`
	Parameters     *Parameters    `json:"parameters,omitempty"`
	Validators     []Validator    `json:"validators,omitempty"`
	ModelOverrides map[string]any `json:"model_overrides,omitempty"`
}

// Variable represents a template variable.
type Variable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// Tool represents a tool definition.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

// ToolPolicy represents tool usage policy.
type ToolPolicy struct {
	ToolChoice          string   `json:"tool_choice,omitempty"`
	MaxRounds           int      `json:"max_rounds,omitempty"`
	MaxToolCallsPerTurn int      `json:"max_tool_calls_per_turn,omitempty"`
	Blocklist           []string `json:"blocklist,omitempty"`
}

// MediaConfig represents media/multimodal configuration.
type MediaConfig struct {
	AllowedTypes []string `json:"allowed_types,omitempty"`
	MaxSize      int      `json:"max_size,omitempty"`
}

// Parameters represents model parameters.
type Parameters struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

// Validator represents a validator configuration.
type Validator struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// Load loads a pack from a JSON file.
func Load(path string) (*Pack, error) {
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

	return &pack, nil
}

// GetPrompt returns a prompt by name, or nil if not found.
func (p *Pack) GetPrompt(name string) *Prompt {
	return p.Prompts[name]
}

// GetTool returns a tool by name, or nil if not found.
func (p *Pack) GetTool(name string) *Tool {
	if p.Tools == nil {
		return nil
	}
	return p.Tools[name]
}

// ListPrompts returns all prompt names in the pack.
func (p *Pack) ListPrompts() []string {
	names := make([]string, 0, len(p.Prompts))
	for name := range p.Prompts {
		names = append(names, name)
	}
	return names
}

// ListTools returns all tool names in the pack.
func (p *Pack) ListTools() []string {
	if p.Tools == nil {
		return nil
	}
	names := make([]string, 0, len(p.Tools))
	for name := range p.Tools {
		names = append(names, name)
	}
	return names
}

// ToPromptRegistry creates a prompt.Registry from the pack.
// This allows the SDK to use the same PromptAssemblyMiddleware as Arena.
func (p *Pack) ToPromptRegistry() *prompt.Registry {
	repo := memory.NewPromptRepository()

	// Convert each pack prompt to a prompt.Config and register it
	for taskType, packPrompt := range p.Prompts {
		cfg := packPrompt.ToPromptConfig(taskType)
		repo.RegisterPrompt(taskType, cfg)
	}

	// Register fragments if any
	for name, content := range p.Fragments {
		repo.RegisterFragment(name, &prompt.Fragment{
			Type:    "text",
			Content: content,
		})
	}

	return prompt.NewRegistryWithRepository(repo)
}

// ToPromptConfig converts a pack Prompt to a prompt.Config.
func (pr *Prompt) ToPromptConfig(taskType string) *prompt.Config {
	cfg := &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       taskType,
			Version:        pr.Version,
			Description:    pr.Description,
			SystemTemplate: pr.SystemTemplate,
			AllowedTools:   pr.Tools,
		},
	}

	// Convert variables
	if len(pr.Variables) > 0 {
		cfg.Spec.Variables = make([]prompt.VariableMetadata, len(pr.Variables))
		for i, v := range pr.Variables {
			cfg.Spec.Variables[i] = prompt.VariableMetadata{
				Name:        v.Name,
				Type:        v.Type,
				Description: v.Description,
				Required:    v.Required,
				Default:     v.Default,
			}
		}
	}

	return cfg
}

// ToToolRepository creates a memory.ToolRepository from the pack.
// This allows the SDK to use the same tools.Registry as Arena.
func (p *Pack) ToToolRepository() *memory.ToolRepository {
	repo := memory.NewToolRepository()

	// Register all tools from the pack
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
