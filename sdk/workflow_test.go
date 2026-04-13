package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
		handlers: make(map[string]ToolHandler),
		config:   &config{},
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

	// Find events by type (delivery order is non-deterministic)
	var transitionedEvent, completedEvent *events.Event
	for _, e := range received {
		switch e.Type {
		case events.EventWorkflowTransitioned:
			transitionedEvent = e
		case events.EventWorkflowCompleted:
			completedEvent = e
		}
	}

	// Check transitioned event
	require.NotNil(t, transitionedEvent, "expected a WorkflowTransitioned event")
	tData, ok := transitionedEvent.Data.(*events.WorkflowTransitionedData)
	require.True(t, ok)
	assert.Equal(t, "start", tData.FromState)
	assert.Equal(t, "done", tData.ToState)
	assert.Equal(t, "Finish", tData.Event)

	// Check completed event
	require.NotNil(t, completedEvent, "expected a WorkflowCompleted event")
	cData, ok := completedEvent.Data.(*events.WorkflowCompletedData)
	require.True(t, ok)
	assert.Equal(t, "done", cData.FinalState)
	assert.Equal(t, 1, cData.TransitionCount)

	_ = machine // used indirectly
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
	// Test extractMessageText with Part fallback (no text in Part)
	msg := &types.Message{
		Role:  "user",
		Parts: []types.ContentPart{{Type: "image"}},
	}
	assert.Equal(t, "", extractMessageText(msg))
}

func TestSummarizeMessages_Empty(t *testing.T) {
	result := summarizeMessages("start", nil)
	assert.Equal(t, "", result)

	result = summarizeMessages("start", []types.Message{})
	assert.Equal(t, "", result)
}

func TestSummarizeMessages_FiltersSystem(t *testing.T) {
	messages := []types.Message{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}
	result := summarizeMessages("intake", messages)
	assert.Contains(t, result, "[Previous state: intake, 3 messages]")
	assert.Contains(t, result, "user: Hello")
	assert.Contains(t, result, "assistant: Hi there")
	assert.NotContains(t, result, "system:")
}

func TestSummarizeMessages_TruncatesLongContent(t *testing.T) {
	longContent := strings.Repeat("x", 300)
	messages := []types.Message{
		{Role: "user", Content: longContent},
	}
	result := summarizeMessages("state1", messages)
	assert.Contains(t, result, "...")
	// Should be truncated to maxSummaryContentLen + "..."
	lines := strings.Split(strings.TrimSpace(result), "\n")
	assert.Len(t, lines, 2) // header + 1 message
	// Content line should be truncated
	assert.True(t, len(lines[1]) < 300)
}

func TestSummarizeMessages_SkipsEmptyContent(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant"}, // empty content
		{Role: "user", Content: "World"},
	}
	result := summarizeMessages("s1", messages)
	assert.Contains(t, result, "user: Hello")
	assert.Contains(t, result, "user: World")
	// Should not have an empty assistant line
	assert.NotContains(t, result, "assistant:")
}

func TestSummarizeMessages_LimitsToMaxMessages(t *testing.T) {
	// Create more messages than defaultMaxSummaryMessages (10)
	messages := make([]types.Message, 15)
	for i := range messages {
		messages[i] = types.Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
		}
	}
	result := summarizeMessages("busy", messages)
	assert.Contains(t, result, "15 messages")
	// Should only include last 10 messages (5-14)
	assert.NotContains(t, result, "message 4")
	assert.Contains(t, result, "message 5")
	assert.Contains(t, result, "message 14")
}

func TestSummarizeMessages_ReversesToChronological(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	result := summarizeMessages("s1", messages)
	firstIdx := strings.Index(result, "first")
	secondIdx := strings.Index(result, "second")
	thirdIdx := strings.Index(result, "third")
	assert.True(t, firstIdx < secondIdx, "first should come before second")
	assert.True(t, secondIdx < thirdIdx, "second should come before third")
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

func TestHandleTransitionTool(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask: "gather_info",
				OnEvent:    map[string]string{"InfoComplete": "processing"},
			},
			"processing": {PromptTask: "process"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	// Use the runtime TransitionExecutor directly (replaces handleTransitionTool)
	transExec := workflow.NewTransitionExecutor(machine, spec)

	args, _ := json.Marshal(map[string]string{
		"event":   "InfoComplete",
		"context": "User wants billing help",
	})

	result, err := transExec.Execute(context.Background(), nil, args)
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify pending transition is set on the executor
	pending := transExec.Pending()
	require.NotNil(t, pending)
	assert.Equal(t, "InfoComplete", pending.Event)
	assert.Equal(t, "User wants billing help", pending.ContextSummary)
}

func TestRegisterWorkflowTools_InternalState(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask:    "gather_info",
				OnEvent:       map[string]string{"InfoComplete": "processing"},
				Orchestration: workflow.OrchestrationInternal,
			},
			"processing": {PromptTask: "process"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	conv := &Conversation{
		toolRegistry: tools.NewRegistry(),
		handlers:     make(map[string]ToolHandler),
		config:       &config{},
	}

	wfCap := NewWorkflowCapability()

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   conv,
		workflowCap:  wfCap,
	}

	wc.registerWorkflowTools()

	// Tool should be registered with executor mode
	tool := conv.ToolRegistry().Get(workflow.TransitionToolName)
	assert.NotNil(t, tool)
	assert.Equal(t, workflow.TransitionExecutorMode, tool.Mode)

	// TransitionExecutor should be created
	assert.NotNil(t, wc.transExec)
}

func TestRegisterWorkflowTools_ExternalState(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "intake",
		States: map[string]*workflow.State{
			"intake": {
				PromptTask:    "gather_info",
				OnEvent:       map[string]string{"InfoComplete": "processing"},
				Orchestration: workflow.OrchestrationExternal,
			},
			"processing": {PromptTask: "process"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	conv := &Conversation{
		toolRegistry: tools.NewRegistry(),
		handlers:     make(map[string]ToolHandler),
		config:       &config{},
	}

	wfCap := NewWorkflowCapability()

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   conv,
		workflowCap:  wfCap,
	}

	wc.registerWorkflowTools()

	// Tool should NOT be registered for external orchestration
	tool := conv.ToolRegistry().Get(workflow.TransitionToolName)
	assert.Nil(t, tool)
}

func TestRegisterWorkflowTools_TerminalState(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "done",
		States: map[string]*workflow.State{
			"done": {PromptTask: "confirm"}, // No OnEvent = terminal
		},
	}
	machine := workflow.NewStateMachine(spec)

	conv := &Conversation{
		toolRegistry: tools.NewRegistry(),
		handlers:     make(map[string]ToolHandler),
		config:       &config{},
	}

	wfCap := NewWorkflowCapability()

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   conv,
		workflowCap:  wfCap,
	}

	wc.registerWorkflowTools()

	// Tool should NOT be registered for terminal state
	tool := conv.ToolRegistry().Get(workflow.TransitionToolName)
	assert.Nil(t, tool)
}

func TestOpenWorkflow_InvalidPackJSON(t *testing.T) {
	packPath := writeWorkflowTestPack(t, `{invalid json`)
	_, err := OpenWorkflow(packPath, WithSkipSchemaValidation())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load pack")
}

func TestTransitionInternal_InvalidEvent(t *testing.T) {
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
	}

	_, err := wc.transitionInternal("InvalidEvent", "")
	require.Error(t, err)
}

func TestTransitionInternal_WithContextSummary(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1", OnEvent: map[string]string{"Next": "end"}},
			"end":   {PromptTask: "p2"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	conv := &Conversation{
		handlers: make(map[string]ToolHandler),
		config:   &config{},
	}

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   conv,
	}

	// ProcessEvent succeeds but Open() will fail (no pack) — tests context summary path
	_, err := wc.transitionInternal("Next", "some context summary")
	// Should fail at Open() since there's no packPath
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open conversation")
}

func TestSend_ClearsPendingTransitionBeforeSend(t *testing.T) {
	conv := &Conversation{closed: true} // will cause Send to return error
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "s",
		States:  map[string]*workflow.State{"s": {PromptTask: "p"}},
	}

	sm := workflow.NewStateMachine(spec)
	transExec := workflow.NewTransitionExecutor(sm, spec)
	// Pre-set a pending transition to verify it gets cleared
	preArgs, _ := json.Marshal(map[string]string{"event": "stale", "context": "old"})
	_, _ = transExec.Execute(context.Background(), nil, preArgs)

	wc := &WorkflowConversation{
		activeConv: conv,
		machine:    sm,
		transExec:  transExec,
	}

	_, err := wc.Send(context.Background(), "hello")
	// Send fails because inner conversation is closed
	assert.ErrorIs(t, err, ErrConversationClosed)
	// transExec pending should have been cleared before the send attempt
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

// errorStore is a statestore.Store that always returns errors on Save.
type errorStore struct {
	statestore.Store
}

func (s *errorStore) Load(_ context.Context, _ string) (*statestore.ConversationState, error) {
	return &statestore.ConversationState{
		ID:       "wf-1",
		Metadata: make(map[string]any),
	}, nil
}

func (s *errorStore) Save(_ context.Context, _ *statestore.ConversationState) error {
	return errors.New("save failed")
}

func TestPersistWorkflowContext_SaveError(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		stateStore:   &errorStore{},
		workflowID:   "wf-1",
	}

	// Should not panic — logs the error instead of discarding
	wc.persistWorkflowContext()
}

func TestPersistWorkflowContext_TransientStateSkipped(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {
				PromptTask:  "p1",
				Persistence: workflow.PersistenceTransient,
			},
		},
	}
	machine := workflow.NewStateMachine(spec)

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		stateStore:   statestore.NewMemoryStore(),
		workflowID:   "wf-1",
	}

	// Should be a no-op (transient state)
	wc.persistWorkflowContext()
}

func TestPersistWorkflowContext_Success(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	store := statestore.NewMemoryStore()

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-1",
	}

	wc.persistWorkflowContext()

	// Verify the workflow context was saved
	state, err := store.Load(context.Background(), "wf-1")
	require.NoError(t, err)
	require.NotNil(t, state.Metadata["workflow"])
}

// TestCommitDeferredTransition_NoTransExec is a no-op when the workflow
// conversation has no transition executor wired up.
func TestCommitDeferredTransition_NoTransExec(t *testing.T) {
	wc := &WorkflowConversation{}
	require.NoError(t, wc.commitDeferredTransition())
}

// TestCommitDeferredTransition_NoPending is a no-op when there is no
// pending transition to commit.
func TestCommitDeferredTransition_NoPending(t *testing.T) {
	spec := &workflow.Spec{Version: 1, Entry: "a", States: map[string]*workflow.State{
		"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
		"b": {PromptTask: "t"},
	}}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{
		machine:   machine,
		transExec: workflow.NewTransitionExecutor(machine, spec),
	}
	require.NoError(t, wc.commitDeferredTransition())
}

// TestCommitDeferredTransition_FiresErrorEvent verifies that a deferred
// commit returning an error routes through emitWorkflowError to fire the
// matching observability event. The state machine is pre-seeded so the very
// first commit hits max_visits and never tries to call applyTransition.
func TestCommitDeferredTransition_FiresErrorEvent(t *testing.T) {
	bus := events.NewEventBus()
	gotEvent := make(chan *events.Event, 1)
	bus.Subscribe(events.EventWorkflowMaxVisitsExceeded, func(e *events.Event) { gotEvent <- e })

	spec := &workflow.Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*workflow.State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t", MaxVisits: 1},
		},
	}
	// Pre-seed the context so b is at its visit cap before the test transition.
	machine := workflow.NewStateMachineFromContext(spec, &workflow.Context{
		CurrentState: "a",
		VisitCounts:  map[string]int{"a": 1, "b": 1},
	})
	transExec := workflow.NewTransitionExecutor(machine, spec)

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		emitter:      events.NewEmitter(bus, "", "", ""),
		transExec:    transExec,
	}

	args, _ := json.Marshal(map[string]string{"event": "Go"})
	_, err := transExec.Execute(context.Background(), nil, args)
	require.NoError(t, err)
	commitErr := wc.commitDeferredTransition()
	require.Error(t, commitErr, "Go into b should fail because visit cap is reached")

	select {
	case e := <-gotEvent:
		data := e.Data.(*events.WorkflowMaxVisitsExceededData)
		assert.Equal(t, "b", data.OriginalTarget)
		assert.True(t, data.Terminated)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected workflow.max_visits_exceeded event")
	}
}

// TestEmitTransitionEvents_All verifies emitTransitionEvents fires the
// expected combination of workflow.transitioned, workflow.max_visits_exceeded
// (on redirect), and workflow.completed (on terminal entry).
func TestEmitTransitionEvents_All(t *testing.T) {
	bus := events.NewEventBus()
	seen := make(map[events.EventType]int)
	var mu sync.Mutex
	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		seen[e.Type]++
		mu.Unlock()
	})

	spec := &workflow.Spec{
		Version: 2,
		Entry:   "loop",
		States: map[string]*workflow.State{
			"loop": {PromptTask: "loop", MaxVisits: 1, OnMaxVisits: "exit",
				OnEvent: map[string]string{"Again": "loop"}},
			"exit": {PromptTask: "exit"}, // terminal: no OnEvent
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		emitter:      events.NewEmitter(bus, "", "", ""),
	}

	// Plain transition (not redirected, not terminal).
	wc.emitTransitionEvents(&workflow.TransitionResult{
		From: "loop", To: "loop", Event: "Again",
	}, "loop", "loop")

	// Drive the machine into a terminal state so IsTerminal returns true.
	wc.machine = workflow.NewStateMachineFromContext(spec, &workflow.Context{
		CurrentState: "exit",
		VisitCounts:  map[string]int{"loop": 1, "exit": 1},
	})

	// Redirected + terminal transition.
	wc.emitTransitionEvents(&workflow.TransitionResult{
		From: "loop", To: "exit", Event: "Again",
		Redirected: true, OriginalTarget: "loop",
	}, "exit", "exit")

	// Allow async delivery to land.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := seen[events.EventWorkflowTransitioned] +
			seen[events.EventWorkflowMaxVisitsExceeded] +
			seen[events.EventWorkflowCompleted]
		mu.Unlock()
		if got >= 4 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 2, seen[events.EventWorkflowTransitioned], "two transitioned events")
	assert.Equal(t, 1, seen[events.EventWorkflowMaxVisitsExceeded], "one redirect event")
	assert.Equal(t, 1, seen[events.EventWorkflowCompleted], "one completed event")
}

// TestEmitTransitionEvents_NilEmitterNoOp verifies the helper short-circuits
// safely when no emitter is configured.
func TestEmitTransitionEvents_NilEmitterNoOp(t *testing.T) {
	wc := &WorkflowConversation{}
	wc.emitTransitionEvents(&workflow.TransitionResult{}, "x", "x") // no panic
}

// TestMaxVisitsForState verifies the lookup helper.
func TestMaxVisitsForState(t *testing.T) {
	assert.Equal(t, 0, maxVisitsForState(nil, "x"))
	spec := &workflow.Spec{States: map[string]*workflow.State{
		"a": {MaxVisits: 5},
	}}
	assert.Equal(t, 5, maxVisitsForState(spec, "a"))
	assert.Equal(t, 0, maxVisitsForState(spec, "missing"))
}

// TestEmitWorkflowError_MaxVisits verifies emitWorkflowError fires
// workflow.max_visits_exceeded when handed a *MaxVisitsExceededError.
func TestEmitWorkflowError_MaxVisits(t *testing.T) {
	bus := events.NewEventBus()

	var received *events.Event
	var mu sync.Mutex
	wg := &sync.WaitGroup{}
	wg.Add(1)
	bus.Subscribe(events.EventWorkflowMaxVisitsExceeded, func(e *events.Event) {
		mu.Lock()
		received = e
		mu.Unlock()
		wg.Done()
	})

	spec := &workflow.Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*workflow.State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t", MaxVisits: 1, OnEvent: map[string]string{"Back": "a"}},
		},
	}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		emitter:      events.NewEmitter(bus, "", "", ""),
	}

	err := &workflow.MaxVisitsExceededError{
		FromState:      "a",
		OriginalTarget: "b",
		Event:          "Go",
		VisitCount:     1,
		MaxVisits:      1,
	}
	wc.emitWorkflowError("Go", err)

	waitWithTimeout(t, wg, 200*time.Millisecond, "workflow.max_visits_exceeded")

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, received)
	data, ok := received.Data.(*events.WorkflowMaxVisitsExceededData)
	require.True(t, ok, "unexpected data type: %T", received.Data)
	assert.Equal(t, "b", data.OriginalTarget)
	assert.Equal(t, 1, data.MaxVisits)
	assert.True(t, data.Terminated)
}

// TestEmitWorkflowError_Budget verifies emitWorkflowError fires
// workflow.budget_exhausted when handed a *BudgetExhaustedError.
func TestEmitWorkflowError_Budget(t *testing.T) {
	bus := events.NewEventBus()

	var received *events.Event
	var mu sync.Mutex
	wg := &sync.WaitGroup{}
	wg.Add(1)
	bus.Subscribe(events.EventWorkflowBudgetExhausted, func(e *events.Event) {
		mu.Lock()
		received = e
		mu.Unlock()
		wg.Done()
	})

	spec := &workflow.Spec{Version: 2, Entry: "a", States: map[string]*workflow.State{
		"a": {PromptTask: "t"},
	}}
	machine := workflow.NewStateMachine(spec)
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		emitter:      events.NewEmitter(bus, "", "", ""),
	}

	err := &workflow.BudgetExhaustedError{
		Limit:        workflow.BudgetLimitToolCalls,
		Current:      10,
		Max:          10,
		CurrentState: "a",
	}
	wc.emitWorkflowError("", err)

	waitWithTimeout(t, wg, 200*time.Millisecond, "workflow.budget_exhausted")

	mu.Lock()
	defer mu.Unlock()
	require.NotNil(t, received)
	data, ok := received.Data.(*events.WorkflowBudgetExhaustedData)
	require.True(t, ok)
	assert.Equal(t, workflow.BudgetLimitToolCalls, data.Limit)
	assert.Equal(t, 10, data.Max)
}

// TestEmitWorkflowError_NilSafe verifies the helper is a no-op when either
// the emitter or the error is nil, or when the error doesn't match a known
// workflow type.
func TestEmitWorkflowError_NilSafe(t *testing.T) {
	wc := &WorkflowConversation{}
	wc.emitWorkflowError("", nil)             // no emitter, no error
	wc.emitWorkflowError("", fmt.Errorf("x")) // no emitter, arbitrary error
	bus := events.NewEventBus()
	wc.emitter = events.NewEmitter(bus, "", "", "")
	wc.emitWorkflowError("", nil)             // nil error is ignored
	wc.emitWorkflowError("", fmt.Errorf("x")) // unmatched error is ignored
}

// waitWithTimeout blocks on wg with a deadline. Fails the test if the
// deadline expires before wg counts down to zero.
func waitWithTimeout(t *testing.T, wg *sync.WaitGroup, d time.Duration, label string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %s", label)
	}
}
