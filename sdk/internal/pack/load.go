// Package pack provides internal pack loading functionality.
package pack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
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

	// Evals - Pack-level eval definitions (applied to all prompts unless overridden)
	Evals []evals.EvalDef `json:"evals,omitempty"`

	// Workflow - State-machine workflow config
	Workflow *WorkflowSpec `json:"workflow,omitempty"`

	// Agents - Agent configuration mapping prompts to A2A-compatible agent definitions
	Agents *AgentsConfig `json:"agents,omitempty"`

	// Skills - Skill sources for dynamic capability loading
	Skills []SkillSourceConfig `json:"skills,omitempty"`

	// FilePath is the path from which this pack was loaded.
	FilePath string `json:"-"`
}

// SkillSourceConfig represents a skill source in the pack.
type SkillSourceConfig struct {
	Dir          string `json:"dir,omitempty"`
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Preload      bool   `json:"preload,omitempty"`
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
	Evals          []evals.EvalDef `json:"evals,omitempty"`
	ModelOverrides map[string]any `json:"model_overrides,omitempty"`
}

// VariableBindingKind defines the type of resource a variable binds to.
type VariableBindingKind string

const (
	// BindingKindProject binds to project metadata (name, description, tags).
	BindingKindProject VariableBindingKind = "project"
	// BindingKindProvider binds to provider/model selection.
	BindingKindProvider VariableBindingKind = "provider"
	// BindingKindWorkspace binds to current workspace (name, namespace).
	BindingKindWorkspace VariableBindingKind = "workspace"
	// BindingKindSecret binds to Kubernetes Secret resources.
	BindingKindSecret VariableBindingKind = "secret"
	// BindingKindConfigMap binds to Kubernetes ConfigMap resources.
	BindingKindConfigMap VariableBindingKind = "configmap"
)

// VariableBindingFilter specifies criteria for filtering bound resources.
type VariableBindingFilter struct {
	// Capability filters resources by capability (e.g., "chat", "embeddings").
	Capability string `json:"capability,omitempty"`
	// Labels filters resources by label selectors.
	Labels map[string]string `json:"labels,omitempty"`
}

// VariableBinding defines how a variable binds to system resources.
type VariableBinding struct {
	// Kind specifies the type of resource to bind to.
	Kind VariableBindingKind `json:"kind"`
	// Field specifies which field of the resource to bind (e.g., "name", "model").
	Field string `json:"field,omitempty"`
	// AutoPopulate enables automatic population of this variable from the bound resource.
	AutoPopulate bool `json:"autoPopulate,omitempty"`
	// Filter specifies criteria for filtering bound resources.
	Filter *VariableBindingFilter `json:"filter,omitempty"`
}

// Variable represents a template variable.
type Variable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	// Binding enables automatic population from system resources and type-safe UI selection.
	Binding *VariableBinding `json:"binding,omitempty"`
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

// AgentsConfig maps prompts to A2A-compatible agent definitions.
type AgentsConfig struct {
	Entry   string               `json:"entry"`
	Members map[string]*AgentDef `json:"members"`
}

// AgentDef provides A2A Agent Card metadata for a single prompt.
type AgentDef struct {
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	InputModes  []string `json:"input_modes,omitempty"`
	OutputModes []string `json:"output_modes,omitempty"`
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
	if err := pack.ValidateWorkflow(); err != nil {
		return nil, err
	}

	// Validate agents section if present
	if err := pack.ValidateAgents(); err != nil {
		return nil, err
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
				Binding:     convertVariableBinding(v.Binding),
			}
		}
	}

	return cfg
}

// convertVariableBinding converts a pack VariableBinding to a prompt VariableBinding.
func convertVariableBinding(b *VariableBinding) *prompt.VariableBinding {
	if b == nil {
		return nil
	}
	result := &prompt.VariableBinding{
		Kind:         prompt.VariableBindingKind(b.Kind),
		Field:        b.Field,
		AutoPopulate: b.AutoPopulate,
	}
	if b.Filter != nil {
		result.Filter = &prompt.VariableBindingFilter{
			Capability: b.Filter.Capability,
			Labels:     b.Filter.Labels,
		}
	}
	return result
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
