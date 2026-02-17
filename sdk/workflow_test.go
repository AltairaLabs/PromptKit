package sdk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/workflow"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// workflowPackJSON is a test pack with a 3-state workflow.
const workflowPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "workflow-test-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"gather_info": {
			"id": "gather_info",
			"name": "Gather Info",
			"description": "Gather user information",
			"version": "1.0.0",
			"system_template": "You gather information."
		},
		"process": {
			"id": "process",
			"name": "Process",
			"description": "Process the request",
			"version": "1.0.0",
			"system_template": "You process requests."
		},
		"confirm": {
			"id": "confirm",
			"name": "Confirm",
			"description": "Confirm resolution",
			"version": "1.0.0",
			"system_template": "You confirm results."
		}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {
				"prompt_task": "gather_info",
				"on_event": {
					"InfoComplete": "processing",
					"NeedMore": "intake"
				}
			},
			"processing": {
				"prompt_task": "process",
				"on_event": {
					"Done": "confirmation",
					"Retry": "processing"
				}
			},
			"confirmation": {
				"prompt_task": "confirm"
			}
		}
	}
}`

// noWorkflowPackJSON is a test pack without a workflow section.
const noWorkflowPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "no-workflow-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"description": "Chat prompt",
			"version": "1.0.0",
			"system_template": "You are a chatbot."
		}
	}
}`

func writeWorkflowTestPack(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestOpenWorkflow_NoWorkflow(t *testing.T) {
	packPath := writeWorkflowTestPack(t, noWorkflowPackJSON)
	_, err := OpenWorkflow(packPath, WithSkipSchemaValidation())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoWorkflow))
}

func TestOpenWorkflow_NonexistentPack(t *testing.T) {
	_, err := OpenWorkflow("/nonexistent/path/pack.json")
	require.Error(t, err)
}

func TestOpenWorkflow_InitialState(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	wc, err := OpenWorkflow(packPath, WithSkipSchemaValidation())
	// OpenWorkflow will fail at provider detection since no API key is set,
	// but we can test the pack loading and workflow detection path.
	// If it fails, it should fail at the Open() step, not at workflow validation.
	if err != nil {
		// Expected: provider detection failure, not workflow error
		assert.NotErrorIs(t, err, ErrNoWorkflow)
		return
	}
	defer wc.Close()

	assert.Equal(t, "intake", wc.CurrentState())
	assert.Equal(t, "gather_info", wc.CurrentPromptTask())
	assert.False(t, wc.IsComplete())
	assert.Contains(t, wc.AvailableEvents(), "InfoComplete")
	assert.Contains(t, wc.AvailableEvents(), "NeedMore")
}

func TestConvertWorkflowSpec(t *testing.T) {
	sdkSpec := &pack.WorkflowSpec{
		Version: 1,
		Entry:   "start",
		States: map[string]*pack.WorkflowState{
			"start": {
				PromptTask:    "greeting",
				Description:   "Entry point",
				OnEvent:       map[string]string{"Next": "end"},
				Persistence:   "persistent",
				Orchestration: "internal",
			},
			"end": {
				PromptTask: "farewell",
			},
		},
		Engine: map[string]any{"timeout": 300},
	}

	spec := convertWorkflowSpec(sdkSpec)

	assert.Equal(t, 1, spec.Version)
	assert.Equal(t, "start", spec.Entry)
	assert.Len(t, spec.States, 2)

	start := spec.States["start"]
	assert.Equal(t, "greeting", start.PromptTask)
	assert.Equal(t, "Entry point", start.Description)
	assert.Equal(t, "end", start.OnEvent["Next"])
	assert.Equal(t, workflow.PersistencePersistent, start.Persistence)
	assert.Equal(t, workflow.OrchestrationInternal, start.Orchestration)

	assert.Equal(t, 300, spec.Engine["timeout"])
}

func TestWorkflowConversation_ClosedErrors(t *testing.T) {
	// Test that closed workflow returns errors
	wc := &WorkflowConversation{closed: true}

	_, err := wc.Send(nil, "hello")
	assert.ErrorIs(t, err, ErrWorkflowClosed)

	_, err = wc.Transition("SomeEvent")
	assert.ErrorIs(t, err, ErrWorkflowClosed)
}

func TestWorkflowConversation_TerminalTransition(t *testing.T) {
	// Create a workflow at a terminal state
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "done",
		States: map[string]*workflow.State{
			"done": {PromptTask: "confirm"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	wc := &WorkflowConversation{
		machine: machine,
	}

	assert.True(t, wc.IsComplete())

	_, err := wc.Transition("SomeEvent")
	assert.ErrorIs(t, err, ErrWorkflowTerminal)
}

func TestWorkflowConversation_Context(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	wc := &WorkflowConversation{
		machine: machine,
	}

	ctx := wc.Context()
	assert.Equal(t, "start", ctx.CurrentState)
	assert.Empty(t, ctx.History)
}

func TestWorkflowConversation_CloseIdempotent(t *testing.T) {
	wc := &WorkflowConversation{}
	assert.NoError(t, wc.Close())
	assert.NoError(t, wc.Close()) // second close is safe
}

func TestWorkflowConversation_ActiveConversation(t *testing.T) {
	conv := &Conversation{}
	wc := &WorkflowConversation{
		activeConv: conv,
	}
	assert.Same(t, conv, wc.ActiveConversation())
}

func TestWorkflowConversation_Accessors(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {PromptTask: "gather", OnEvent: map[string]string{
				"Done": "processing",
				"Back": "intake",
			}},
			"processing": {PromptTask: "process", OnEvent: map[string]string{
				"Finish": "done",
			}},
			"done": {PromptTask: "confirm"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{machine: machine}

	assert.Equal(t, "intake", wc.CurrentState())
	assert.Equal(t, "gather", wc.CurrentPromptTask())
	assert.False(t, wc.IsComplete())
	events := wc.AvailableEvents()
	assert.Len(t, events, 2)
	assert.Contains(t, events, "Done")
	assert.Contains(t, events, "Back")
}

func TestWorkflowConversation_TransitionInvalidEvent(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{machine: machine}

	_, err := wc.Transition("BadEvent")
	assert.ErrorIs(t, err, workflow.ErrInvalidEvent)
}

func TestWorkflowConversation_TransitionSuccess(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	// Create a minimal workflow with a nil activeConv (Close on nil is safe)
	// Transition will fail at Open() since we don't have a real pack, but
	// we can verify the state machine advances
	wc := &WorkflowConversation{
		machine:  machine,
		packPath: "/nonexistent/pack.json",
	}

	newState, err := wc.Transition("Next")
	// Open will fail since pack doesn't exist, but state machine should have advanced
	assert.Error(t, err)
	assert.Empty(t, newState) // error means no state returned
	// The machine did advance internally
	assert.Equal(t, "end", wc.machine.CurrentState())
}

func TestWorkflowConversation_SendDelegatesToConversation(t *testing.T) {
	// Test that Send on non-closed wc reaches the active conversation
	// Create a conversation with closed=true to get a known error
	conv := &Conversation{closed: true}
	wc := &WorkflowConversation{
		activeConv: conv,
		machine: workflow.NewStateMachine(&workflow.Spec{
			Version: 1,
			Entry:   "s",
			States:  map[string]*workflow.State{"s": {PromptTask: "p"}},
		}),
	}

	_, err := wc.Send(nil, "hello")
	// Should get ErrConversationClosed from the inner conversation, not ErrWorkflowClosed
	assert.ErrorIs(t, err, ErrConversationClosed)
}

func TestWorkflowConversation_CloseWithActiveConv(t *testing.T) {
	// Test that Close properly closes the active conversation
	conv := &Conversation{
		handlers:  make(map[string]ToolHandler),
		config:    &config{},
	}
	wc := &WorkflowConversation{
		activeConv: conv,
	}
	assert.NoError(t, wc.Close())
	assert.True(t, wc.closed)
}

func TestOpenWorkflow_BadOption(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	_, err := OpenWorkflow(packPath, func(c *config) error {
		return fmt.Errorf("bad option")
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad option")
}
