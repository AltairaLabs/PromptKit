package workflow

import (
	"encoding/json"
	"sort"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// TransitionToolName is the qualified name of the workflow transition tool.
const TransitionToolName = "workflow__transition"

// transitionToolDescription is the shared description for the transition tool.
const transitionToolDescription = "Transition the workflow to a new state by triggering an event. " +
	"Use this when the conversation requires escalation, resolution, or any state change."

// BuildTransitionToolDescriptor creates a tools.ToolDescriptor for the
// workflow__transition tool with the given events as an enum constraint.
func BuildTransitionToolDescriptor(events []string) *tools.ToolDescriptor {
	schemaJSON := buildTransitionSchema(events)
	return &tools.ToolDescriptor{
		Name:        TransitionToolName,
		Namespace:   "workflow",
		Description: transitionToolDescription,
		InputSchema: schemaJSON,
	}
}

// BuildTransitionProviderDescriptor creates a providers.ToolDescriptor
// (minimal 4-field variant) for use with Arena's provider interface.
func BuildTransitionProviderDescriptor(events []string) *providers.ToolDescriptor {
	schemaJSON := buildTransitionSchema(events)
	return &providers.ToolDescriptor{
		Name:        TransitionToolName,
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

// buildTransitionSchema constructs the JSON schema for the transition tool.
func buildTransitionSchema(events []string) json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"event": map[string]any{
				"type":        "string",
				"enum":        events,
				"description": "The workflow event to trigger.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Summary of relevant context to carry forward.",
			},
		},
		"required": []string{"event", "context"},
	}
	schemaJSON, _ := json.Marshal(schema)
	return schemaJSON
}
