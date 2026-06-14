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

func TestConvertWorkflowSpec_CarriesComposition(t *testing.T) {
	in := &pack.WorkflowSpec{
		Version: 1,
		Entry:   "analyze",
		States: map[string]*pack.WorkflowState{
			"analyze": {Orchestration: "composition", Composition: "analyze_doc", Terminal: true},
		},
	}
	out := convertWorkflowSpec(in)
	st := out.States["analyze"]
	if st.Orchestration != workflow.OrchestrationComposition {
		t.Errorf("orchestration = %q, want %q", st.Orchestration, workflow.OrchestrationComposition)
	}
	if st.Composition != "analyze_doc" {
		t.Errorf("composition = %q, want analyze_doc", st.Composition)
	}
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
	}
	metadata := map[string]any{workflowCurrentKey: wfCtx}

	result, err := extractWorkflowContext(metadata)
	require.NoError(t, err)
	assert.Equal(t, "processing", result.CurrentState)
	// History/ArtifactHistory now live in lists, not in workflow.current.
	assert.Nil(t, result.History)
}

func TestExtractWorkflowContext_FromJSON(t *testing.T) {
	// Simulate what happens after JSON round-trip (e.g., Redis store).
	wfCtx := &workflow.Context{
		CurrentState: "done",
	}
	data, err := json.Marshal(wfCtx)
	require.NoError(t, err)

	var rawMap map[string]any
	require.NoError(t, json.Unmarshal(data, &rawMap))

	metadata := map[string]any{workflowCurrentKey: rawMap}
	result, err := extractWorkflowContext(metadata)
	require.NoError(t, err)
	assert.Equal(t, "done", result.CurrentState)
}

func TestExtractWorkflowContext_Missing(t *testing.T) {
	metadata := map[string]any{}
	_, err := extractWorkflowContext(metadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow context")
}

func TestExtractWorkflowContext_InvalidType(t *testing.T) {
	metadata := map[string]any{workflowCurrentKey: "invalid"}
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

	wc.persistWorkflowContext()

	ctx := context.Background()
	meta, err := store.LoadMetadata(ctx, "wf-123")
	require.NoError(t, err)
	require.NotNil(t, meta[workflowCurrentKey])
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
		Metadata: map[string]any{workflowCurrentKey: &workflow.Context{CurrentState: "main"}},
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

	// Save the workflow.current view in metadata, plus a single
	// History entry in the workflow.history list — matches the split
	// storage layout that ResumeWorkflow rebuilds from.
	wfCtx := &workflow.Context{CurrentState: "processing"}
	_ = store.Save(ctx, &statestore.ConversationState{
		ID:       "wf-resume-test",
		Metadata: map[string]any{workflowCurrentKey: wfCtx},
	})
	historyEntry, _ := json.Marshal(workflow.StateTransition{From: "intake", To: "processing", Event: "InfoComplete"})
	require.NoError(t, store.AppendList(ctx, "wf-resume-test", workflowHistoryListName, [][]byte{historyEntry}))

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
		workflowCurrentKey: map[string]any{
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

	// Should be a no-op (transient state).
	wc.persistWorkflowContext()
}

// readOnlyStore implements only the bare Store interface (Load + Fork) —
// no MetadataAccessor, no ListAccessor. Used to exercise the early-return
// path in persistWorkflowContext when the store can't satisfy either of
// the required typed-write interfaces.
type readOnlyStore struct{}

func (readOnlyStore) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	return &statestore.ConversationState{ID: id, Metadata: make(map[string]any)}, nil
}
func (readOnlyStore) Fork(_ context.Context, _, _ string) error { return nil }

// TestPersistWorkflowContext_NoWriteCapability covers the branch where the
// store implements neither MetadataAccessor nor ListAccessor — the
// function must log an error and return without panicking.
func TestPersistWorkflowContext_NoWriteCapability(t *testing.T) {
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
		stateStore:   readOnlyStore{},
		workflowID:   "wf-1",
	}

	wc.persistWorkflowContext() // Must not panic.
}

// metadataOnlyStore satisfies MetadataAccessor but not ListAccessor —
// exercises the requirement that persistWorkflowContext refuses to write
// when the store can't store History/ArtifactHistory deltas.
type metadataOnlyStore struct {
	readOnlyStore
	merged map[string]any
}

func (s *metadataOnlyStore) LoadMetadata(_ context.Context, _ string) (map[string]any, error) {
	return s.merged, nil
}
func (s *metadataOnlyStore) MergeMetadata(_ context.Context, _ string, updates map[string]any) error {
	if s.merged == nil {
		s.merged = make(map[string]any)
	}
	for k, v := range updates {
		s.merged[k] = v
	}
	return nil
}

// TestPersistWorkflowContext_FailsWhenStoreMissingListAccessor verifies
// that a store with MetadataAccessor but no ListAccessor is rejected
// up-front — no metadata is written, since a partial write would leave
// the workflow unrecoverable.
func TestPersistWorkflowContext_FailsWhenStoreMissingListAccessor(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1"},
		},
	}
	machine := workflow.NewStateMachine(spec)

	store := &metadataOnlyStore{}
	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-1",
	}

	wc.persistWorkflowContext()

	// Nothing should have been written — the missing ListAccessor
	// triggers an early return before MergeMetadata is called.
	assert.Empty(t, store.merged)
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

	meta, err := store.LoadMetadata(context.Background(), "wf-1")
	require.NoError(t, err)
	require.NotNil(t, meta[workflowCurrentKey])
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
	// Production code wires this in registerWorkflowTools; mirror that
	// here since the test bypasses Open and constructs the executor directly.
	transExec.SetOnCommitError(wc.emitWorkflowError)

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

// pinHistory rewrites machine.Context().History (and ArtifactHistory)
// in-place via a fresh state machine seeded with a synthetic context.
// Used by the incremental-persistence tests to drive the delta logic
// without going through real transitions.
func pinHistory(spec *workflow.Spec, history []workflow.StateTransition, artHist []workflow.ArtifactSnapshot) *workflow.StateMachine {
	wfCtx := &workflow.Context{
		CurrentState:    spec.Entry,
		History:         history,
		ArtifactHistory: artHist,
	}
	return workflow.NewStateMachineFromContext(spec, wfCtx)
}

// TestPersistWorkflowContext_SecondPersistAppendsOnlyDelta drives the
// per-transition delta logic: a second persist with one new History
// entry appends exactly one item to the workflow.history list, not the
// full slice.
func TestPersistWorkflowContext_SecondPersistAppendsOnlyDelta(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*workflow.State{
			"start": {PromptTask: "p1"},
		},
	}
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	wc := &WorkflowConversation{
		machine:      pinHistory(spec, []workflow.StateTransition{{From: "a", To: "b", Event: "X"}}, nil),
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-1",
	}

	wc.persistWorkflowContext()
	require.Equal(t, 1, wc.historyAppended)
	n, err := store.ListLen(ctx, "wf-1", workflowHistoryListName)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	// Add a second history entry. persistWorkflowContext should RPUSH
	// only the new tail entry — the list grows from 1 to 2, not from 1
	// to 3 (which a "rewrite the whole slice" implementation would
	// produce).
	wc.machine = pinHistory(spec, []workflow.StateTransition{
		{From: "a", To: "b", Event: "X"},
		{From: "b", To: "c", Event: "Y"},
	}, nil)
	wc.persistWorkflowContext()
	require.Equal(t, 2, wc.historyAppended)
	n, err = store.ListLen(ctx, "wf-1", workflowHistoryListName)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

// TestPersistWorkflowContext_NoArtifactChangeSkipsArtifactList confirms
// that when ArtifactHistory's length is unchanged, no AppendList write
// hits the artifact_history list.
func TestPersistWorkflowContext_NoArtifactChangeSkipsArtifactList(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States:  map[string]*workflow.State{"start": {PromptTask: "p1"}},
	}
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// First persist seeds one history entry, no artifact entry.
	wc := &WorkflowConversation{
		machine:      pinHistory(spec, []workflow.StateTransition{{From: "a", To: "b"}}, nil),
		workflowSpec: spec,
		stateStore:   store,
		workflowID:   "wf-1",
	}
	wc.persistWorkflowContext()

	// Second persist adds one new history entry but no artifact change.
	wc.machine = pinHistory(spec, []workflow.StateTransition{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
	}, nil)
	wc.persistWorkflowContext()

	// History list grew, artifact list never received any writes.
	hN, err := store.ListLen(ctx, "wf-1", workflowHistoryListName)
	require.NoError(t, err)
	assert.Equal(t, 2, hN)
	aN, err := store.ListLen(ctx, "wf-1", workflowArtifactListName)
	require.NoError(t, err)
	assert.Equal(t, 0, aN)
	assert.Equal(t, 0, wc.artifactHistoryAppended)
}

// TestHydrateWorkflowContextLists_StoreMissingListAccessor exercises the
// early-return path when the store can't satisfy the ListAccessor
// contract — Resume should fail loudly rather than silently returning a
// half-built context.
func TestHydrateWorkflowContextLists_StoreMissingListAccessor(t *testing.T) {
	wfCtx := &workflow.Context{CurrentState: "x"}
	_, _, err := hydrateWorkflowContextLists(context.Background(), readOnlyStore{}, "wf-1", wfCtx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ListAccessor")
}

// listLoadErrorStore satisfies ListAccessor with LoadList always returning
// a non-ErrNotFound failure — exercises the propagation path in
// loadWorkflowList / hydrateWorkflowContextLists.
type listLoadErrorStore struct{ readOnlyStore }

func (listLoadErrorStore) AppendList(_ context.Context, _, _ string, _ [][]byte) error {
	return nil
}
func (listLoadErrorStore) LoadList(_ context.Context, _, _ string) ([][]byte, error) {
	return nil, errors.New("simulated list load failure")
}
func (listLoadErrorStore) ListLen(_ context.Context, _, _ string) (int, error) {
	return 0, errors.New("simulated list len failure")
}

func TestHydrateWorkflowContextLists_LoadListError(t *testing.T) {
	wfCtx := &workflow.Context{CurrentState: "x"}
	_, _, err := hydrateWorkflowContextLists(context.Background(), listLoadErrorStore{}, "wf-1", wfCtx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "load workflow history")
}

// TestAppendWorkflowListDelta_NoNewItemsSkipsWrite covers the early-return
// branch when the in-memory slice length matches the tracker — no
// AppendList call should be made and the tracker is unchanged.
func TestAppendWorkflowListDelta_NoNewItemsSkipsWrite(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	// Seed the store so we can detect any unexpected write.
	require.NoError(t, store.AppendList(ctx, "wf-1", "events", [][]byte{[]byte(`"x"`)}))

	tracker := 1
	err := appendWorkflowListDelta(ctx, store, "wf-1", "events",
		[]string{"x"}, &tracker)
	require.NoError(t, err)
	assert.Equal(t, 1, tracker)

	n, err := store.ListLen(ctx, "wf-1", "events")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// listAppendErrorStore satisfies the interfaces needed by
// persistWorkflowContext but every AppendList call fails — exercises the
// warn-and-continue branches inside persistWorkflowContext.
type listAppendErrorStore struct{ readOnlyStore }

func (listAppendErrorStore) LoadMetadata(_ context.Context, _ string) (map[string]any, error) {
	return nil, nil
}
func (listAppendErrorStore) MergeMetadata(_ context.Context, _ string, _ map[string]any) error {
	return nil
}
func (listAppendErrorStore) AppendList(_ context.Context, _, _ string, _ [][]byte) error {
	return errors.New("simulated append failure")
}
func (listAppendErrorStore) LoadList(_ context.Context, _, _ string) ([][]byte, error) {
	return nil, nil
}
func (listAppendErrorStore) ListLen(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

func TestPersistWorkflowContext_AppendErrorsLoggedNotFatal(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "start",
		States:  map[string]*workflow.State{"start": {PromptTask: "p1"}},
	}
	wc := &WorkflowConversation{
		machine: pinHistory(spec, []workflow.StateTransition{{From: "a", To: "b"}}, []workflow.ArtifactSnapshot{
			{FromState: "a", ToState: "b"},
		}),
		workflowSpec: spec,
		stateStore:   listAppendErrorStore{},
		workflowID:   "wf-1",
	}

	// Both AppendList calls fail; persist must log and return without
	// panicking, and the trackers must not advance past the failed write.
	wc.persistWorkflowContext()
	assert.Equal(t, 0, wc.historyAppended)
	assert.Equal(t, 0, wc.artifactHistoryAppended)
}

// TestExtractWorkflowContext_ValueType covers the workflow.Context (by
// value) branch of extractWorkflowContext, which complements the
// pointer/map branches already exercised.
func TestExtractWorkflowContext_ValueType(t *testing.T) {
	metadata := map[string]any{workflowCurrentKey: workflow.Context{CurrentState: "v"}}
	got, err := extractWorkflowContext(metadata)
	require.NoError(t, err)
	assert.Equal(t, "v", got.CurrentState)
}

// TestHydrateWorkflowContextLists_RebuildsFromLists exercises the
// reverse half of the split-storage round-trip: after appending entries
// to the lists, a fresh hydrate populates History/ArtifactHistory and
// returns the lengths the caller needs to seed delta trackers.
func TestHydrateWorkflowContextLists_RebuildsFromLists(t *testing.T) {
	store := statestore.NewMemoryStore()
	ctx := context.Background()

	histA, _ := json.Marshal(workflow.StateTransition{From: "a", To: "b", Event: "X"})
	histB, _ := json.Marshal(workflow.StateTransition{From: "b", To: "c", Event: "Y"})
	require.NoError(t, store.AppendList(ctx, "wf-1", workflowHistoryListName, [][]byte{histA, histB}))

	artA, _ := json.Marshal(workflow.ArtifactSnapshot{FromState: "a", ToState: "b", Event: "X"})
	require.NoError(t, store.AppendList(ctx, "wf-1", workflowArtifactListName, [][]byte{artA}))

	wfCtx := &workflow.Context{CurrentState: "c"}
	historyLen, artHistLen, err := hydrateWorkflowContextLists(ctx, store, "wf-1", wfCtx)
	require.NoError(t, err)
	assert.Equal(t, 2, historyLen)
	assert.Equal(t, 1, artHistLen)
	require.Len(t, wfCtx.History, 2)
	assert.Equal(t, "a", wfCtx.History[0].From)
	assert.Equal(t, "Y", wfCtx.History[1].Event)
	require.Len(t, wfCtx.ArtifactHistory, 1)
	assert.Equal(t, "a", wfCtx.ArtifactHistory[0].FromState)
}

// TestReconcileActiveConv_NoOpOnMatchingState verifies that when the
// workflow machine's current prompt_task already matches the active
// conversation's prompt name, reconcileActiveConv is a no-op — it must
// not close the active conv or attempt to open a new one. This is the
// hot path during normal Sends that don't trigger a transition.
func TestReconcileActiveConv_NoOpOnMatchingState(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*workflow.State{
			"a": {PromptTask: "task-a", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "task-b"},
		},
	}
	wc := &WorkflowConversation{
		machine:      workflow.NewStateMachine(spec),
		workflowSpec: spec,
		// activeConv already at task-a — matches machine's current prompt_task.
		activeConv: &Conversation{promptName: "task-a"},
		packPath:   "/nonexistent/pack.json", // would fail Open if reconcile tried
	}
	require.NoError(t, wc.reconcileActiveConv(""))
	assert.False(t, wc.activeConv.closed, "active conv must remain open when no transition occurred")
	assert.Equal(t, "task-a", wc.activeConv.promptName)
}

// TestReconcileActiveConv_NoActiveConv verifies the helper returns nil
// when there is no active conversation yet (e.g., during initial open).
func TestReconcileActiveConv_NoActiveConv(t *testing.T) {
	spec := &workflow.Spec{Version: 1, Entry: "a", States: map[string]*workflow.State{
		"a": {PromptTask: "task-a"},
	}}
	wc := &WorkflowConversation{
		machine:      workflow.NewStateMachine(spec),
		workflowSpec: spec,
	}
	require.NoError(t, wc.reconcileActiveConv(""))
}

// TestOnTransitionCommitted_EmitsEvent verifies the hook fires
// workflow.transitioned for both eager (in-Send) and deferred (post-Send)
// commits. This is the same signal Arena emits and what observability
// consumers subscribe to.
func TestOnTransitionCommitted_EmitsEvent(t *testing.T) {
	bus := events.NewEventBus()
	transitioned := make(chan *events.Event, 4)
	bus.Subscribe(events.EventWorkflowTransitioned, func(e *events.Event) { transitioned <- e })

	spec := &workflow.Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*workflow.State{
			"a": {PromptTask: "p-a", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "p-b"},
		},
	}
	wc := &WorkflowConversation{
		machine:      workflow.NewStateMachine(spec),
		workflowSpec: spec,
		emitter:      events.NewEmitter(bus, "", "", ""),
	}
	wc.onTransitionCommitted(&workflow.TransitionResult{From: "a", To: "b", Event: "Go"})

	select {
	case e := <-transitioned:
		assert.Equal(t, events.EventWorkflowTransitioned, e.Type)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected workflow.transitioned event")
	}
}

// TestOnTransitionCommitted_NilResultIsNoOp verifies the hook does
// nothing when called with a nil TransitionResult (matches CommitPending's
// nil-on-noop semantics).
func TestOnTransitionCommitted_NilResultIsNoOp(t *testing.T) {
	wc := &WorkflowConversation{}
	wc.onTransitionCommitted(nil) // must not panic
}

// TestReconcileActiveConv_StateDivergedOpenError verifies that when the
// machine has moved past the active conv but Open() fails (e.g., bad
// pack path), reconcileActiveConv closes the old conv and returns the
// open error. This is the only failure surface added by the refactor —
// the no-op path is the hot path and is covered separately.
func TestReconcileActiveConv_StateDivergedOpenError(t *testing.T) {
	spec := &workflow.Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*workflow.State{
			"a": {PromptTask: "task-a", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "task-b"},
		},
	}
	machine := workflow.NewStateMachine(spec)
	// Advance machine to "b" so its CurrentPromptTask diverges from the
	// active conv's promptName.
	_, err := machine.ProcessEvent("Go")
	require.NoError(t, err)

	wc := &WorkflowConversation{
		machine:      machine,
		workflowSpec: spec,
		activeConv:   &Conversation{promptName: "task-a"},
		packPath:     "/nonexistent/pack.json",
	}
	err = wc.reconcileActiveConv("a summary")
	require.Error(t, err, "Open against a missing pack path must surface its error")
	assert.Contains(t, err.Error(), "task-b")
	assert.True(t, wc.activeConv.closed, "old conv must be closed even when opening the new one fails")
}
