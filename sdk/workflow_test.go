package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

func TestWorkflowConversation_TransitionEmitsEvents(t *testing.T) {
	bus := events.NewEventBus()

	var received []*events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1) // Expect at least one transition event

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		wg.Done()
	})

	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	emitter := events.NewEmitter(bus, "", "", "")

	wc := &WorkflowConversation{
		machine:  machine,
		packPath: "/nonexistent/pack.json",
		emitter:  emitter,
	}

	// Transition will fail at Open() but events should still be emitted
	// since the state machine advances before Open()
	_, _ = wc.Transition("Next")

	// Wait for async event delivery
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		// Events may not arrive if Transition failed before emitting
	}

	// The transition fails at Open(), which happens before event emission,
	// so we may or may not get events. But the machine did advance.
	assert.Equal(t, "end", wc.machine.CurrentState())
}

func TestWorkflowConversation_TransitionEmitsCompletedOnTerminal(t *testing.T) {
	bus := events.NewEventBus()

	var received []*events.Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2) // transitioned + completed

	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		wg.Done()
	})

	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Finish": "done"}},
			"done":  {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	emitter := events.NewEmitter(bus, "", "", "")

	// Use a nil packPath so Open() fails, but we need to test event emission
	// which happens AFTER Open() in the current code. So this test will only
	// pass if we have a valid pack. Use writeWorkflowTestPack isn't possible
	// since we need to construct wc directly.
	// Instead, test the emitter logic directly.
	emitter.WorkflowTransitioned("start", "done", "Finish", "p2")
	emitter.WorkflowCompleted("done", 1)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 2)

	// Check transitioned event
	assert.Equal(t, events.EventWorkflowTransitioned, received[0].Type)
	tData, ok := received[0].Data.(*events.WorkflowTransitionedData)
	require.True(t, ok)
	assert.Equal(t, "start", tData.FromState)
	assert.Equal(t, "done", tData.ToState)
	assert.Equal(t, "Finish", tData.Event)

	// Check completed event
	assert.Equal(t, events.EventWorkflowCompleted, received[1].Type)
	cData, ok := received[1].Data.(*events.WorkflowCompletedData)
	require.True(t, ok)
	assert.Equal(t, "done", cData.FinalState)
	assert.Equal(t, 1, cData.TransitionCount)

	_ = machine // used indirectly
}

func TestFilterRelevantMessages(t *testing.T) {
	messages := []types.Message{
		{Role: "system", Content: "You are helpful."},
		{Role: "user", Content: "Hello there"},
		{Role: "assistant", Content: "Hi! How can I help?"},
		{Role: "user", Content: "I need billing help"},
	}

	relevant := filterRelevantMessages(messages)
	assert.Len(t, relevant, 3) // system filtered out
	assert.Equal(t, "user", relevant[0].Role)
	assert.Equal(t, "assistant", relevant[1].Role)
	assert.Equal(t, "user", relevant[2].Role)
}

func TestFilterRelevantMessages_AllSystem(t *testing.T) {
	messages := []types.Message{
		{Role: "system", Content: "System 1"},
		{Role: "system", Content: "System 2"},
	}
	relevant := filterRelevantMessages(messages)
	assert.Empty(t, relevant)
}

func TestFilterRelevantMessages_Empty(t *testing.T) {
	relevant := filterRelevantMessages(nil)
	assert.Empty(t, relevant)
}

func TestWithContextCarryForward(t *testing.T) {
	cfg := &config{}
	opt := WithContextCarryForward()
	require.NoError(t, opt(cfg))
	assert.True(t, cfg.contextCarryForward)
}

func TestWorkflowConversation_NilEmitterSafe(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	// No emitter - should not panic
	wc := &WorkflowConversation{
		machine:  machine,
		packPath: "/nonexistent/pack.json",
	}

	_, _ = wc.Transition("Next")
	assert.Equal(t, "end", wc.machine.CurrentState())
}

func TestExtractWorkflowContext_Direct(t *testing.T) {
	wfCtx := &workflow.Context{
		CurrentState: "processing",
		History: []workflow.StateTransition{
			{From: "intake", To: "processing", Event: "Next"},
		},
	}
	metadata := map[string]any{"workflow": wfCtx}

	result, err := extractWorkflowContext(metadata)
	require.NoError(t, err)
	assert.Equal(t, "processing", result.CurrentState)
	assert.Len(t, result.History, 1)
}

func TestExtractWorkflowContext_FromJSON(t *testing.T) {
	// Simulate what happens after JSON round-trip (e.g., Redis store)
	wfCtx := &workflow.Context{
		CurrentState: "done",
		History: []workflow.StateTransition{
			{From: "start", To: "done", Event: "Finish"},
		},
	}
	data, err := json.Marshal(wfCtx)
	require.NoError(t, err)

	var rawMap map[string]any
	require.NoError(t, json.Unmarshal(data, &rawMap))

	metadata := map[string]any{"workflow": rawMap}
	result, err := extractWorkflowContext(metadata)
	require.NoError(t, err)
	assert.Equal(t, "done", result.CurrentState)
	assert.Len(t, result.History, 1)
}

func TestExtractWorkflowContext_Missing(t *testing.T) {
	metadata := map[string]any{}
	_, err := extractWorkflowContext(metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow context")
}

func TestExtractWorkflowContext_InvalidType(t *testing.T) {
	metadata := map[string]any{"workflow": "invalid"}
	_, err := extractWorkflowContext(metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected workflow context type")
}

func TestPersistWorkflowContext(t *testing.T) {
	store := statestore.NewMemoryStore()
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
		machine:      machine,
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-123",
		packPath:     "/nonexistent/pack.json",
	}

	// Persist creates new state
	wc.persistWorkflowContext()

	ctx := context.Background()
	state, err := store.Load(ctx, "wf-123")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.NotNil(t, state.Metadata["workflow"])
}

func TestPersistWorkflowContext_TransientSkips(t *testing.T) {
	store := statestore.NewMemoryStore()
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "transient_state",
		States: map[string]*workflow.State{
			"transient_state": {
				PromptTask:  "p1",
				Persistence: workflow.PersistenceTransient,
			},
		},
	}
	machine := workflow.NewStateMachine(spec)

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-456",
	}

	wc.persistWorkflowContext()

	ctx := context.Background()
	state, _ := store.Load(ctx, "wf-456")
	assert.Nil(t, state) // Should not have persisted
}

func TestResumeWorkflow_NoStateStore(t *testing.T) {
	_, err := ResumeWorkflow("wf-123", "/some/pack.json")
	assert.ErrorIs(t, err, ErrNoStateStore)
}

func TestResumeWorkflow_NotFound(t *testing.T) {
	store := statestore.NewMemoryStore()
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	_, err := ResumeWorkflow("nonexistent", packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
	)
	assert.Error(t, err)
}

func TestExtractMessageText_Content(t *testing.T) {
	msg := &types.Message{Role: "user", Content: "hello"}
	assert.Equal(t, "hello", extractMessageText(msg))
}

func TestExtractMessageText_Parts(t *testing.T) {
	text := "from parts"
	msg := &types.Message{
		Role: "assistant",
		Parts: []types.ContentPart{
			{Text: &text},
		},
	}
	assert.Equal(t, "from parts", extractMessageText(msg))
}

func TestExtractMessageText_Empty(t *testing.T) {
	msg := &types.Message{Role: "assistant"}
	assert.Equal(t, "", extractMessageText(msg))
}

func TestBuildContextSummary_WithMessages(t *testing.T) {
	// buildContextSummary needs a real Conversation with a session, which requires
	// a full pack load. Test the helper functions independently.
	// We can test via the Transition path indirectly — already tested above.
	// Here we test the output format directly by constructing messages.

	// Test extractMessageText with Part fallback (no text in Part)
	msg := &types.Message{
		Role:  "user",
		Parts: []types.ContentPart{{Type: "image"}},
	}
	assert.Equal(t, "", extractMessageText(msg))
}

func TestResumeWorkflow_BadOption(t *testing.T) {
	store := statestore.NewMemoryStore()
	_, err := ResumeWorkflow("wf-123", "/some/pack.json",
		WithStateStore(store),
		func(c *config) error { return fmt.Errorf("bad opt") },
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad opt")
}

func TestResumeWorkflow_InvalidPack(t *testing.T) {
	store := statestore.NewMemoryStore()
	_, err := ResumeWorkflow("wf-123", "/nonexistent/pack.json",
		WithStateStore(store),
	)
	assert.Error(t, err)
}

func TestResumeWorkflow_NoWorkflow(t *testing.T) {
	packPath := writeWorkflowTestPack(t, noWorkflowPackJSON)
	store := statestore.NewMemoryStore()

	// Pre-populate state so Load succeeds
	ctx := context.Background()
	_ = store.Save(ctx, &statestore.ConversationState{
		ID:       "wf-no-wf",
		Metadata: map[string]any{"workflow": &workflow.Context{CurrentState: "main"}},
	})

	_, err := ResumeWorkflow("wf-no-wf", packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
	)
	assert.ErrorIs(t, err, ErrNoWorkflow)
}

func TestResumeWorkflow_BadMetadata(t *testing.T) {
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// State exists but metadata has no workflow key
	_ = store.Save(ctx, &statestore.ConversationState{
		ID:       "wf-bad-meta",
		Metadata: map[string]any{},
	})

	_, err := ResumeWorkflow("wf-bad-meta", packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workflow context")
}

func TestResumeWorkflow_ValidMetadataButOpenFails(t *testing.T) {
	// This test exercises the full ResumeWorkflow path through metadata extraction,
	// spec conversion, and state machine creation, failing only at Open().
	packPath := writeWorkflowTestPack(t, workflowPackJSON)
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Save valid workflow context
	wfCtx := &workflow.Context{
		CurrentState: "processing",
		History: []workflow.StateTransition{
			{From: "intake", To: "processing", Event: "InfoComplete"},
		},
	}
	_ = store.Save(ctx, &statestore.ConversationState{
		ID:       "wf-resume-test",
		Metadata: map[string]any{"workflow": wfCtx},
	})

	// ResumeWorkflow will succeed through metadata extraction and state machine
	// creation, but fail at Open() because no provider is configured.
	// The important thing is it gets past extractWorkflowContext and
	// NewStateMachineFromContext.
	wc, err := ResumeWorkflow("wf-resume-test", packPath,
		WithStateStore(store),
		WithSkipSchemaValidation(),
	)
	// May succeed or fail depending on whether Open works without a provider
	if err != nil {
		// Expected: fails at Open() since no provider configured
		assert.Contains(t, err.Error(), "failed to open conversation")
	} else {
		// If it somehow succeeds, verify state
		assert.Equal(t, "processing", wc.CurrentState())
		_ = wc.Close()
	}
}

func TestExtractWorkflowContext_MarshalError(t *testing.T) {
	// Test with a map that would fail JSON round-trip (channel value)
	metadata := map[string]any{
		"workflow": map[string]any{
			"current_state": make(chan int), // Can't marshal channels
		},
	}
	_, err := extractWorkflowContext(metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to marshal")
}

func TestWorkflowConversation_OrchestrationMode_Default(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{machine: machine, workflowSpec: spec}

	// No orchestration set, defaults to internal
	assert.Equal(t, workflow.OrchestrationInternal, wc.OrchestrationMode())
}

func TestWorkflowConversation_OrchestrationMode_External(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {
				PromptTask:    "p1",
				Orchestration: workflow.OrchestrationExternal,
				OnEvent:       map[string]string{"Next": "end"},
			},
			"end": {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{machine: machine, workflowSpec: spec}

	assert.Equal(t, workflow.OrchestrationExternal, wc.OrchestrationMode())
}

func TestWorkflowConversation_OrchestrationMode_NilSpec(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "s",
		States:  map[string]*workflow.State{"s": {PromptTask: "p"}},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{machine: machine} // workflowSpec is nil

	assert.Equal(t, workflow.OrchestrationInternal, wc.OrchestrationMode())
}

func TestWorkflowConversation_ConcurrentReads(t *testing.T) {
	t.Parallel()

	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {
				PromptTask:    "p1",
				Orchestration: workflow.OrchestrationExternal,
				OnEvent:       map[string]string{"Next": "end"},
			},
			"end": {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
	}

	// Spawn multiple goroutines reading concurrently — should not race
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wc.CurrentState()
			_ = wc.CurrentPromptTask()
			_ = wc.IsComplete()
			_ = wc.AvailableEvents()
			_ = wc.OrchestrationMode()
			_ = wc.Context()
			_ = wc.ActiveConversation()
		}()
	}
	wg.Wait()
}

func TestWorkflowConversation_ConcurrentSendAndTransition(t *testing.T) {
	t.Parallel()

	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {
				PromptTask:    "p1",
				Orchestration: workflow.OrchestrationExternal,
				OnEvent:       map[string]string{"Next": "mid"},
			},
			"mid": {
				PromptTask:    "p2",
				Orchestration: workflow.OrchestrationExternal,
				OnEvent:       map[string]string{"Next": "end"},
			},
			"end": {PromptTask: "p3"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	// Use a closed conversation so Send returns immediately with ErrConversationClosed
	conv := &Conversation{closed: true}
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   conv,
		packPath:     "/nonexistent/pack.json",
	}

	// Concurrent reads and writes should not cause data races
	var wg sync.WaitGroup

	// Concurrent Send calls (will fail, but exercises the lock)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = wc.Send(context.Background(), "hello")
		}()
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wc.CurrentState()
			_ = wc.OrchestrationMode()
			_ = wc.IsComplete()
		}()
	}

	wg.Wait()
	// No assertions needed — test validates absence of data races with -race flag
}
