package workflow

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
		"Zebra":  "z_state",
		"Alpha":  "a_state",
		"Middle": "m_state",
	}
	got := SortedEvents(onEvent)
	assert.Equal(t, []string{"Alpha", "Middle", "Zebra"}, got)
}

func TestSortedEvents_Empty(t *testing.T) {
	got := SortedEvents(nil)
	assert.Empty(t, got)
}

func TestRegisterTransitionTool(t *testing.T) {
	registry := tools.NewRegistry()
	state := &State{OnEvent: map[string]string{"Escalate": "s2"}}

	RegisterTransitionTool(registry, state)

	desc := registry.Get(TransitionToolName)
	require.NotNil(t, desc)
	assert.Equal(t, TransitionToolName, desc.Name)
}

func TestRegisterTransitionTool_NilState(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterTransitionTool(registry, nil)
	assert.Nil(t, registry.Get(TransitionToolName))
}

func TestRegisterTransitionTool_TerminalState(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterTransitionTool(registry, &State{})
	assert.Nil(t, registry.Get(TransitionToolName))
}

func TestRegisterTransitionTool_ExternalOrchestration(t *testing.T) {
	registry := tools.NewRegistry()
	state := &State{
		OnEvent:       map[string]string{"Escalate": "s2"},
		Orchestration: OrchestrationExternal,
	}
	RegisterTransitionTool(registry, state)
	assert.Nil(t, registry.Get(TransitionToolName))
}
