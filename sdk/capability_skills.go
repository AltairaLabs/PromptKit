package sdk

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// SkillsCapability provides skill activation/deactivation tools to conversations.
// Skills are loaded from directories or inline definitions and can be dynamically
// activated by the LLM via the skill__activate tool.
type SkillsCapability struct {
	sources   []skills.SkillSource
	selector  skills.SkillSelector
	maxActive int
	executor  *skills.Executor
}

// SkillsOption configures a SkillsCapability.
type SkillsOption func(*SkillsCapability)

// WithSkillSelector sets a custom skill selector for filtering available skills.
func WithSkillSelector(s skills.SkillSelector) SkillsOption {
	return func(c *SkillsCapability) {
		c.selector = s
	}
}

// WithMaxActiveSkills sets the maximum number of concurrently active skills.
func WithMaxActiveSkills(n int) SkillsOption {
	return func(c *SkillsCapability) {
		c.maxActive = n
	}
}

// NewSkillsCapability creates a new SkillsCapability from the given sources.
func NewSkillsCapability(
	sources []skills.SkillSource, opts ...SkillsOption,
) *SkillsCapability {
	c := &SkillsCapability{
		sources: sources,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the capability identifier.
func (c *SkillsCapability) Name() string { return capabilityNameSkills }

// Init discovers skills from sources and creates an executor.
func (c *SkillsCapability) Init(ctx CapabilityContext) error {
	reg := skills.NewRegistry()
	if err := reg.Discover(c.sources); err != nil {
		return fmt.Errorf("skills discovery: %w", err)
	}

	// Collect pack tool names from the prompt
	var packTools []string
	if prompt, ok := ctx.Pack.Prompts[ctx.PromptName]; ok {
		packTools = prompt.Tools
	}

	cfg := skills.ExecutorConfig{
		Registry:  reg,
		Selector:  c.selector,
		PackTools: packTools,
		MaxActive: c.maxActive,
	}
	c.executor = skills.NewExecutor(cfg)

	// Preload skills marked with preload: true
	for _, sk := range reg.PreloadedSkills() {
		_, _, _ = c.executor.Activate(sk.Name)
	}

	return nil
}

// RegisterTools registers the skill management tools into the registry.
func (c *SkillsCapability) RegisterTools(registry *tools.Registry) {
	if c.executor == nil {
		return
	}

	activateDesc := buildSkillActivateDescriptor()
	deactivateDesc := buildSkillDeactivateDescriptor()
	readResourceDesc := buildSkillReadResourceDescriptor()

	_ = registry.Register(activateDesc)
	_ = registry.Register(deactivateDesc)
	_ = registry.Register(readResourceDesc)

	registry.RegisterExecutor(newSkillExecutor(c.executor))
}

// Close is a no-op for SkillsCapability.
func (c *SkillsCapability) Close() error { return nil }

// Executor returns the underlying skills executor for testing.
func (c *SkillsCapability) Executor() *skills.Executor { return c.executor }

// --- Tool Descriptors ---

const (
	capabilityNameSkills  = "skills"
	skillActivateTool     = "skill__activate"
	skillDeactivateTool   = "skill__deactivate"
	skillReadResourceTool = "skill__read_resource"
	skillNamespace        = "skill"
	skillExecutorName     = "skill"
)

func buildSkillActivateDescriptor() *tools.ToolDescriptor {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the skill to activate.",
			},
		},
		"required": []string{"name"},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"instructions": map[string]any{"type": "string"},
			"added_tools":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	})

	return &tools.ToolDescriptor{
		Name:         skillActivateTool,
		Namespace:    skillNamespace,
		Description:  "Activate a skill to load its instructions and tools.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         skillExecutorName,
	}
}

func buildSkillDeactivateDescriptor() *tools.ToolDescriptor {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the skill to deactivate.",
			},
		},
		"required": []string{"name"},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"removed_tools": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	})

	return &tools.ToolDescriptor{
		Name:         skillDeactivateTool,
		Namespace:    skillNamespace,
		Description:  "Deactivate a skill to unload its instructions and tools.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         skillExecutorName,
	}
}

func buildSkillReadResourceDescriptor() *tools.ToolDescriptor {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_name": map[string]any{
				"type":        "string",
				"description": "Name of the skill owning the resource.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path to the resource within the skill directory.",
			},
		},
		"required": []string{"skill_name", "path"},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{"type": "string"},
		},
	})

	return &tools.ToolDescriptor{
		Name:         skillReadResourceTool,
		Namespace:    skillNamespace,
		Description:  "Read a resource file from within a skill's directory.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         skillExecutorName,
	}
}

// --- Skill Executor ---

// skillExecutor handles execution of skill__ tools by delegating to the Executor.
type skillExecutor struct {
	executor *skills.Executor
}

func newSkillExecutor(exec *skills.Executor) *skillExecutor {
	return &skillExecutor{executor: exec}
}

// Name returns the executor name used for mode matching.
func (e *skillExecutor) Name() string { return skillExecutorName }

// Execute dispatches a skill tool call to the appropriate executor method.
func (e *skillExecutor) Execute(
	tool *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	switch tool.Name {
	case skillActivateTool:
		return e.executeActivate(args)
	case skillDeactivateTool:
		return e.executeDeactivate(args)
	case skillReadResourceTool:
		return e.executeReadResource(args)
	default:
		return nil, fmt.Errorf("unknown skill tool: %s", tool.Name)
	}
}

func (e *skillExecutor) executeActivate(args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parsing activate args: %w", err)
	}

	instructions, addedTools, err := e.executor.Activate(params.Name)
	if err != nil {
		return nil, err
	}

	if addedTools == nil {
		addedTools = []string{}
	}
	result := map[string]any{
		"instructions": instructions,
		"added_tools":  addedTools,
	}
	return json.Marshal(result)
}

func (e *skillExecutor) executeDeactivate(args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parsing deactivate args: %w", err)
	}

	removedTools, err := e.executor.Deactivate(params.Name)
	if err != nil {
		return nil, err
	}

	if removedTools == nil {
		removedTools = []string{}
	}
	result := map[string]any{
		"removed_tools": removedTools,
	}
	return json.Marshal(result)
}

func (e *skillExecutor) executeReadResource(args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		SkillName string `json:"skill_name"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parsing read_resource args: %w", err)
	}

	data, err := e.executor.ReadResource(params.SkillName, params.Path)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"content": string(data),
	}
	return json.Marshal(result)
}
