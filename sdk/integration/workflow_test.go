package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// workflowPackJSON defines a three-state workflow for integration tests.
// All non-terminal states use external orchestration so that transitions
// are driven by explicit Transition() calls rather than LLM tool calls.
const workflowPackJSON = `{
	"id": "workflow-test",
	"version": "1.0.0",
	"description": "Workflow integration test pack",
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {
				"prompt_task": "intake",
				"description": "Initial contact",
				"on_event": {
					"Escalate": "specialist",
					"Resolve": "closed"
				},
				"orchestration": "external"
			},
			"specialist": {
				"prompt_task": "specialist",
				"description": "Specialist handling",
				"on_event": {
					"Resolve": "closed"
				},
				"orchestration": "external"
			},
			"closed": {
				"prompt_task": "closed",
				"description": "Conversation complete"
			}
		}
	},
	"prompts": {
		"intake": {
			"id": "intake",
			"name": "Intake",
			"system_template": "You are handling initial contact."
		},
		"specialist": {
			"id": "specialist",
			"name": "Specialist",
			"system_template": "You are a specialist."
		},
		"closed": {
			"id": "closed",
			"name": "Closed",
			"system_template": "This conversation is closed."
		}
	}
}`

// openTestWorkflow creates a WorkflowConversation with a mock provider.
func openTestWorkflow(t *testing.T, opts ...sdk.Option) *sdk.WorkflowConversation {
	t.Helper()
	packPath := writePackFile(t, workflowPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	defaults := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	}
	allOpts := append(defaults, opts...)

	wc, err := sdk.OpenWorkflow(packPath, allOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	return wc
}

// ---------------------------------------------------------------------------
// 3.1 — Basic lifecycle
// ---------------------------------------------------------------------------

func TestWorkflow_BasicLifecycle(t *testing.T) {
	wc := openTestWorkflow(t)

	// Entry state is "intake"
	assert.Equal(t, "intake", wc.CurrentState())
	assert.False(t, wc.IsComplete())

	// Available events in intake
	avail := wc.AvailableEvents()
	assert.Contains(t, avail, "Escalate")
	assert.Contains(t, avail, "Resolve")

	// Send a message and verify we get a response
	resp, err := wc.Send(context.Background(), "Hello, I need help")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())

	// Transition intake → specialist
	newState, err := wc.Transition("Escalate")
	require.NoError(t, err)
	assert.Equal(t, "specialist", newState)
	assert.Equal(t, "specialist", wc.CurrentState())
	assert.False(t, wc.IsComplete())

	// Transition specialist → closed
	newState, err = wc.Transition("Resolve")
	require.NoError(t, err)
	assert.Equal(t, "closed", newState)
	assert.Equal(t, "closed", wc.CurrentState())
	assert.True(t, wc.IsComplete())
}

// ---------------------------------------------------------------------------
// 3.2 — Programmatic transitions (skip intermediate states)
// ---------------------------------------------------------------------------

func TestWorkflow_ProgrammaticTransitions(t *testing.T) {
	wc := openTestWorkflow(t)

	// Verify initial available events
	avail := wc.AvailableEvents()
	assert.Contains(t, avail, "Escalate")
	assert.Contains(t, avail, "Resolve")

	// Jump directly from intake → closed via Resolve
	newState, err := wc.Transition("Resolve")
	require.NoError(t, err)
	assert.Equal(t, "closed", newState)
	assert.True(t, wc.IsComplete())

	// Terminal state has no available events
	assert.Empty(t, wc.AvailableEvents())
}

// ---------------------------------------------------------------------------
// 3.3 — Event emission
// ---------------------------------------------------------------------------

func TestWorkflow_EventEmission(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	wc := openTestWorkflow(t, sdk.WithEventBus(bus))

	// Transition intake → specialist — should emit workflow.transitioned
	_, err := wc.Transition("Escalate")
	require.NoError(t, err)

	require.True(t, ec.waitForEvent(events.EventWorkflowTransitioned, 2*time.Second),
		"expected workflow.transitioned event")

	transitioned := ec.ofType(events.EventWorkflowTransitioned)
	require.Len(t, transitioned, 1)

	// Transition specialist → closed — should emit both transitioned and completed
	_, err = wc.Transition("Resolve")
	require.NoError(t, err)

	require.True(t, ec.waitForEvent(events.EventWorkflowCompleted, 2*time.Second),
		"expected workflow.completed event")

	transitioned = ec.ofType(events.EventWorkflowTransitioned)
	assert.Len(t, transitioned, 2, "expected two transition events total")

	completed := ec.ofType(events.EventWorkflowCompleted)
	assert.Len(t, completed, 1, "expected exactly one completed event")
}

// ---------------------------------------------------------------------------
// 3.4 — Invalid transition
// ---------------------------------------------------------------------------

func TestWorkflow_InvalidTransition(t *testing.T) {
	wc := openTestWorkflow(t)

	assert.Equal(t, "intake", wc.CurrentState())

	// Try a non-existent event
	_, err := wc.Transition("nonexistent_event")
	require.Error(t, err)
	assert.ErrorIs(t, err, workflow.ErrInvalidEvent)

	// State should remain unchanged
	assert.Equal(t, "intake", wc.CurrentState())
}
