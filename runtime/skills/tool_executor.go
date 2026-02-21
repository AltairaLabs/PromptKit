package skills

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Tool name constants for the skill__ namespace.
const (
	SkillActivateTool     = "skill__activate"
	SkillDeactivateTool   = "skill__deactivate"
	SkillReadResourceTool = "skill__read_resource"
	SkillNamespace        = "skill"
	SkillExecutorName     = "skill"
)

// BuildSkillActivateDescriptor returns the tool descriptor for skill__activate.
func BuildSkillActivateDescriptor() *tools.ToolDescriptor {
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
		Name:         SkillActivateTool,
		Namespace:    SkillNamespace,
		Description:  "Activate a skill to load its instructions and tools.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         SkillExecutorName,
	}
}

// BuildSkillActivateDescriptorWithIndex returns a skill__activate descriptor
// whose Description includes the available-skills index so the LLM can
// discover which skills are available.
func BuildSkillActivateDescriptorWithIndex(index string) *tools.ToolDescriptor {
	desc := BuildSkillActivateDescriptor()
	desc.Description = "Activate a skill to load its instructions and tools.\n\n" + index
	return desc
}

// BuildSkillDeactivateDescriptor returns the tool descriptor for skill__deactivate.
func BuildSkillDeactivateDescriptor() *tools.ToolDescriptor {
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
		Name:         SkillDeactivateTool,
		Namespace:    SkillNamespace,
		Description:  "Deactivate a skill to unload its instructions and tools.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         SkillExecutorName,
	}
}

// BuildSkillReadResourceDescriptor returns the tool descriptor for skill__read_resource.
func BuildSkillReadResourceDescriptor() *tools.ToolDescriptor {
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
		Name:         SkillReadResourceTool,
		Namespace:    SkillNamespace,
		Description:  "Read a resource file from within a skill's directory.",
		InputSchema:  inputSchema,
		OutputSchema: outputSchema,
		Mode:         SkillExecutorName,
	}
}

// ToolExecutor handles execution of skill__ tools by delegating to the Executor.
type ToolExecutor struct {
	executor *Executor
}

// NewToolExecutor creates a new ToolExecutor wrapping the given skills Executor.
func NewToolExecutor(exec *Executor) *ToolExecutor {
	return &ToolExecutor{executor: exec}
}

// Name returns the executor name used for mode matching.
func (e *ToolExecutor) Name() string { return SkillExecutorName }

// Execute dispatches a skill tool call to the appropriate executor method.
func (e *ToolExecutor) Execute(
	tool *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	switch tool.Name {
	case SkillActivateTool:
		return e.executeActivate(args)
	case SkillDeactivateTool:
		return e.executeDeactivate(args)
	case SkillReadResourceTool:
		return e.executeReadResource(args)
	default:
		return nil, fmt.Errorf("unknown skill tool: %s", tool.Name)
	}
}

func (e *ToolExecutor) executeActivate(args json.RawMessage) (json.RawMessage, error) {
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

func (e *ToolExecutor) executeDeactivate(args json.RawMessage) (json.RawMessage, error) {
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

func (e *ToolExecutor) executeReadResource(args json.RawMessage) (json.RawMessage, error) {
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
