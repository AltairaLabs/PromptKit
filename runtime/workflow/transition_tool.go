package workflow

import (
	"encoding/json"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// TransitionToolName is the qualified name of the workflow transition tool.
const TransitionToolName = "workflow__transition"

// transitionToolDescription is the shared description for the transition tool.
const transitionToolDescription = "Transition the workflow to a new state by triggering an event. " +
	"Use this when the conversation requires escalation, resolution, or any state change."

// toolNamespace is the namespace the workflow tools are registered under.
const toolNamespace = "workflow"

// Argument names of the workflow tools' input schemas.
const (
	argEvent   = "event"
	argContext = "context"
	argName    = "name"
	argValue   = "value"
)

// JSON Schema keywords and type names used in the workflow tool schemas.
const (
	schemaKeyType        = "type"
	schemaKeyProperties  = "properties"
	schemaKeyDescription = "description"
	schemaKeyEnum        = "enum"
	schemaKeyRequired    = "required"
	schemaTypeObject     = "object"
	schemaTypeString     = "string"
)

// BuildTransitionToolDescriptor creates a tools.ToolDescriptor for the
// workflow__transition tool with the given events as an enum constraint.
func BuildTransitionToolDescriptor(events []string) *tools.ToolDescriptor {
	schemaJSON := buildTransitionSchema(events)
	return &tools.ToolDescriptor{
		Name:        TransitionToolName,
		Namespace:   toolNamespace,
		Description: transitionToolDescription,
		InputSchema: schemaJSON,
	}
}

// SortedEvents returns a sorted copy of the event keys from an OnEvent map.
func SortedEvents(onEvent map[string]string) []string {
	events := make([]string, 0, len(onEvent))
	for event := range onEvent {
		events = append(events, event)
	}
	sort.Strings(events)
	return events
}

// RegisterTransitionTool registers the workflow__transition tool in the given
// registry for the specified state's available events. Skips terminal states
// (no events) and externally orchestrated states.
//
// Both the SDK (WorkflowCapability) and Arena (engine) use this function.
func RegisterTransitionTool(registry *tools.Registry, state *State) {
	if registry == nil || state == nil || len(state.OnEvent) == 0 {
		return
	}
	if OrchestrationOf(state) == OrchestrationExternal {
		return
	}
	evts := SortedEvents(state.OnEvent)
	desc := BuildTransitionToolDescriptor(evts)
	_ = registry.Register(desc)
}

// buildTransitionSchema constructs the JSON schema for the transition tool.
func buildTransitionSchema(events []string) json.RawMessage {
	schema := map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			argEvent: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyEnum:        events,
				schemaKeyDescription: "The workflow event to trigger.",
			},
			argContext: map[string]any{
				schemaKeyType:        schemaTypeString,
				schemaKeyDescription: "Summary of relevant context to carry forward.",
			},
		},
		schemaKeyRequired: []string{argEvent, argContext},
	}
	schemaJSON, _ := json.Marshal(schema)
	return schemaJSON
}
