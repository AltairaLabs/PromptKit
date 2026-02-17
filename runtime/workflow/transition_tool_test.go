package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransitionToolName(t *testing.T) {
	assert.Equal(t, "workflow__transition", TransitionToolName)
}

func TestBuildTransitionToolDescriptor(t *testing.T) {
	events := []string{"Escalate", "Resolve"}
	desc := BuildTransitionToolDescriptor(events)

	assert.Equal(t, TransitionToolName, desc.Name)
	assert.Equal(t, "workflow", desc.Namespace)
	assert.Contains(t, desc.Description, "Transition the workflow")

	// Parse and verify schema
	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))

	assert.Equal(t, "object", schema["type"])

	props := schema["properties"].(map[string]any)
	eventProp := props["event"].(map[string]any)
	assert.Equal(t, "string", eventProp["type"])
	assert.Equal(t, []any{"Escalate", "Resolve"}, eventProp["enum"])

	contextProp := props["context"].(map[string]any)
	assert.Equal(t, "string", contextProp["type"])

	required := schema["required"].([]any)
	assert.Equal(t, []any{"event", "context"}, required)
}

func TestBuildTransitionProviderDescriptor(t *testing.T) {
	events := []string{"Done", "Retry"}
	desc := BuildTransitionProviderDescriptor(events)

	assert.Equal(t, TransitionToolName, desc.Name)
	assert.Contains(t, desc.Description, "Transition the workflow")

	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))

	props := schema["properties"].(map[string]any)
	eventProp := props["event"].(map[string]any)
	assert.Equal(t, []any{"Done", "Retry"}, eventProp["enum"])
}

func TestBuildTransitionToolDescriptor_EmptyEvents(t *testing.T) {
	desc := BuildTransitionToolDescriptor([]string{})

	var schema map[string]any
	require.NoError(t, json.Unmarshal(desc.InputSchema, &schema))

	props := schema["properties"].(map[string]any)
	eventProp := props["event"].(map[string]any)
	assert.Equal(t, []any{}, eventProp["enum"])
}

func TestSortedEvents(t *testing.T) {
	onEvent := map[string]string{
		"Zebra":    "z_state",
		"Alpha":    "a_state",
		"Middle":   "m_state",
	}
	got := SortedEvents(onEvent)
	assert.Equal(t, []string{"Alpha", "Middle", "Zebra"}, got)
}

func TestSortedEvents_Empty(t *testing.T) {
	got := SortedEvents(nil)
	assert.Empty(t, got)
}
