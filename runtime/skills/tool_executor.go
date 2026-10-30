package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Tool name constants for the skill__ namespace.
const (
	SkillActivateTool     = "skill__activate"
	SkillDeactivateTool   = "skill__deactivate"
	SkillReadResourceTool = "skill__read_resource"
	SkillNamespace        = "skill"
	SkillExecutorName     = "skill"
)

// Skill tool argument and result field names.
const (
	argName            = "name"
	argSkillName       = "skill_name"
	argPath            = "path"
	resultInstructions = "instructions"
	resultAddedTools   = "added_tools"
	resultRemovedTools = "removed_tools"
	resultContent      = "content"
)

// JSON Schema keywords and type names used in the skill tool schemas.
const (
	schemaKeyType        = "type"
	schemaKeyProperties  = "properties"
	schemaKeyDescription = "description"
	schemaKeyItems       = "items"
	schemaKeyRequired    = "required"
	schemaTypeObject     = "object"
	schemaTypeString     = "string"
	schemaTypeArray      = "array"
)

// BuildSkillActivateDescriptor returns the tool descriptor for skill__activate.
func BuildSkillActivateDescriptor() *tools.ToolDescriptor {
	schema := map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			argName: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyDescription: "Name of the skill to activate.",
			},
		},
		schemaKeyRequired: []string{argName},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultInstructions: map[string]any{schemaKeyType: schemaTypeString},
			resultAddedTools: map[string]any{
				schemaKeyType:  schemaTypeArray,
				schemaKeyItems: map[string]any{schemaKeyType: schemaTypeString},
			},
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
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			argName: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyDescription: "Name of the skill to deactivate.",
			},
		},
		schemaKeyRequired: []string{argName},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultRemovedTools: map[string]any{
				schemaKeyType:  schemaTypeArray,
				schemaKeyItems: map[string]any{schemaKeyType: schemaTypeString},
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
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			argSkillName: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyDescription: "Name of the skill owning the resource.",
			},
			argPath: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyDescription: "Relative path to the resource within the skill directory.",
			},
		},
		schemaKeyRequired: []string{argSkillName, argPath},
	}
	inputSchema, _ := json.Marshal(schema)

	outputSchema, _ := json.Marshal(map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultContent: map[string]any{schemaKeyType: schemaTypeString},
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
//
// Activation and deactivation land in the [ActiveSet] the caller attached with
// [WithActiveSet], so one ToolExecutor -- which is all a tools.Registry holds
// per name -- serves concurrent conversations without their active skills
// reaching each other (#2011).
func (e *ToolExecutor) Execute(
	ctx context.Context, tool *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	switch tool.Name {
	case SkillActivateTool:
		return e.executeActivate(ctx, args)
	case SkillDeactivateTool:
		return e.executeDeactivate(ctx, args)
	case SkillReadResourceTool:
		return e.executeReadResource(args)
	default:
		return nil, fmt.Errorf("unknown skill tool: %s", tool.Name)
	}
}

// activeSet returns the conversation's set from the context, falling back to
// the Executor's own set for hosts that have not adopted [WithActiveSet].
func (e *ToolExecutor) activeSet(ctx context.Context) *ActiveSet {
	if set := ActiveSetFromContext(ctx); set != nil {
		return set
	}
	return e.executor.OwnActiveSet()
}

func (e *ToolExecutor) executeActivate(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parsing activate args: %w", err)
	}

	act, err := e.executor.ActivateIn(e.activeSet(ctx), params.Name)
	if err != nil {
		return nil, err
	}

	addedTools := act.AddedTools
	if addedTools == nil {
		addedTools = []string{}
	}
	result := map[string]any{
		resultInstructions: act.Instructions,
		resultAddedTools:   addedTools,
	}
	return json.Marshal(result)
}

func (e *ToolExecutor) executeDeactivate(
	ctx context.Context, args json.RawMessage,
) (json.RawMessage, error) {
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parsing deactivate args: %w", err)
	}

	removedTools, err := e.executor.DeactivateIn(e.activeSet(ctx), params.Name)
	if err != nil {
		return nil, err
	}

	if removedTools == nil {
		removedTools = []string{}
	}
	result := map[string]any{
		resultRemovedTools: removedTools,
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
		resultContent: string(data),
	}
	return json.Marshal(result)
}
