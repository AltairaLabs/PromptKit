package stage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestCompositionStage_RunsToCompletion(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo") // reuse helper from composition_executor_test.go

	comp := &composition.Composition{
		Version: 1, Output: "b",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "${input.x}"}},
			{ID: "b", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"prev": "${a.output.v}"}},
		},
	}
	cs := NewCompositionStage("comp", comp, CompositionExecutorDeps{ToolRegistry: reg})

	pipe, err := NewPipelineBuilder().Chain(cs).Build()
	if err != nil {
		t.Fatal(err)
	}
	msg := types.Message{Role: "user", Content: `{"x":"hi"}`}
	res, err := pipe.ExecuteSync(context.Background(), NewMessageElement(&msg))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Response == nil {
		t.Fatal("expected a response")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(res.Response.Content), &got); err != nil {
		t.Fatalf("response not JSON: %q (%v)", res.Response.Content, err)
	}
	if got["prev"] != "hi" {
		t.Errorf("composition output = %#v, want prev=hi", got)
	}
}

// TestCompositionStage_PlainTextInput verifies that a plain-text (non-JSON)
// message content is JSON-encoded before being passed to the engine.
func TestCompositionStage_PlainTextInput(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	// Single-step composition that echoes its input back directly.
	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"msg": "${input}"}},
		},
	}
	cs := NewCompositionStage("plain", comp, CompositionExecutorDeps{ToolRegistry: reg})

	pipe, err := NewPipelineBuilder().Chain(cs).Build()
	if err != nil {
		t.Fatal(err)
	}
	// "hello world" is not valid JSON — compositionInput must JSON-encode it.
	msg := types.Message{Role: "user", Content: "hello world"}
	res, err := pipe.ExecuteSync(context.Background(), NewMessageElement(&msg))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Response == nil {
		t.Fatal("expected a response")
	}
}

// TestCompositionStage_ForwardsNonMessageElements verifies that elements which
// are not message elements (e.g. EndOfStream or elements without a Message) are
// forwarded unchanged and do not trigger composition execution.
func TestCompositionStage_ForwardsNonMessageElements(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "x"}},
		},
	}
	cs := NewCompositionStage("fwd", comp, CompositionExecutorDeps{ToolRegistry: reg})

	// Drive the stage directly: send a text element (no .Message) then a message element.
	in := make(chan StreamElement, 2)
	out := make(chan StreamElement, 10)

	txt := "some text"
	in <- StreamElement{Text: &txt} // non-message element — must be forwarded
	msg := &types.Message{Role: "user", Content: `{}`}
	in <- NewMessageElement(msg) // message element — triggers execution
	close(in)

	if err := cs.Process(context.Background(), in, out); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	elems := drainChannel(out)
	if len(elems) < 2 {
		t.Fatalf("expected at least 2 elements, got %d", len(elems))
	}
	// First element should be the forwarded text element.
	if elems[0].Text == nil || *elems[0].Text != "some text" {
		t.Errorf("first element: want text='some text', got %+v", elems[0])
	}
	// Second element should be the assistant result from the composition.
	if elems[1].Message == nil || elems[1].Message.Role != roleAssistant {
		t.Errorf("second element: want assistant message, got %+v", elems[1])
	}
}

// TestCompositionStage_ContextCancellation verifies that Process returns
// ctx.Err() when the context is cancelled before a blocking composition runs.
// A blocking tool is registered so the engine cannot complete before the context
// is cancelled; we cancel before calling Process, ensuring the cancellation is
// detected on the first forward or execute attempt.
func TestCompositionStage_ContextCancellation(t *testing.T) {
	reg := tools.NewRegistry()

	// Register a tool that blocks until the context is cancelled.
	blockExec := &blockingExecutor{}
	reg.RegisterExecutor(blockExec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        "block",
		Description: "blocking tool",
		Mode:        blockExec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("register block tool: %v", err)
	}

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "block", Args: map[string]any{}},
		},
	}
	cs := NewCompositionStage("cancel", comp, CompositionExecutorDeps{ToolRegistry: reg})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Process is called

	in := make(chan StreamElement, 1)
	out := make(chan StreamElement, 10)

	msg := &types.Message{Role: "user", Content: "{}"}
	in <- NewMessageElement(msg)
	close(in)

	err := cs.Process(ctx, in, out)
	if err == nil {
		t.Error("expected a cancellation error, got nil")
	}
}

// blockingExecutor is a tool executor that blocks until its context is cancelled.
type blockingExecutor struct{}

func (b *blockingExecutor) Name() string { return "blocking" }
func (b *blockingExecutor) Execute(ctx context.Context, _ *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestCompositionStage_SkipsHistoryMessages verifies that history-flagged
// message elements (Meta.FromHistory == true) are forwarded without triggering
// composition execution, while the subsequent live user message IS executed.
func TestCompositionStage_SkipsHistoryMessages(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "${input}"}},
		},
	}
	cs := NewCompositionStage("hist", comp, CompositionExecutorDeps{ToolRegistry: reg})

	in := make(chan StreamElement, 3)
	out := make(chan StreamElement, 10)

	// First element: history message — must NOT trigger execution.
	histMsg := &types.Message{Role: "user", Content: `{"x":"history"}`}
	histElem := NewMessageElement(histMsg)
	histElem.Meta.FromHistory = true
	in <- histElem

	// Second element: live user message — MUST trigger execution.
	liveMsg := &types.Message{Role: "user", Content: `{"x":"live"}`}
	in <- NewMessageElement(liveMsg)
	close(in)

	if err := cs.Process(context.Background(), in, out); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	elems := drainChannel(out)
	// Expect: forwarded history element + assistant result from live message.
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements (history + result), got %d: %+v", len(elems), elems)
	}
	// First element should be the forwarded history message (not an assistant).
	if elems[0].Message == nil || elems[0].Message.Role != "user" {
		t.Errorf("first element: want forwarded user/history message, got %+v", elems[0])
	}
	if !elems[0].Meta.FromHistory {
		t.Errorf("first element: want FromHistory=true, got false")
	}
	// Second element should be the assistant result from the live message.
	if elems[1].Message == nil || elems[1].Message.Role != roleAssistant {
		t.Errorf("second element: want assistant message, got %+v", elems[1])
	}
}

// TestCompositionInput_ObjectBiased verifies that compositionInput treats only
// explicit JSON objects/arrays as verbatim composition input; bare scalars and
// plain text are always encoded as JSON strings.
func TestCompositionInput_ObjectBiased(t *testing.T) {
	cases := []struct {
		content string
		want    string // expected JSON encoding of result
	}{
		{`{"x":1}`, `{"x":1}`},           // object → verbatim
		{`[1,2,3]`, `[1,2,3]`},           // array → verbatim
		{`true`, `"true"`},               // bare boolean → string
		{`42`, `"42"`},                   // bare number → string
		{`"hello"`, `"\"hello\""`},       // bare string literal → string
		{`hello world`, `"hello world"`}, // plain text → string
	}
	for _, tc := range cases {
		msg := &types.Message{Content: tc.content}
		got := compositionInput(msg)
		if string(got) != tc.want {
			t.Errorf("compositionInput(%q) = %s, want %s", tc.content, got, tc.want)
		}
	}
}

// drainChannel collects all remaining elements from a closed channel.
func drainChannel(ch <-chan StreamElement) []StreamElement {
	var elems []StreamElement
	for e := range ch {
		elems = append(elems, e)
	}
	return elems
}

// TestCompositionStage_AttachesSnapshotMeta verifies that when a
// CompositionStage is constructed with a recorder, the assistant message emitted
// after execution carries a non-nil *CompositionSnapshot under the
// "_composition_snapshot" Meta key with at least one step recorded.
func TestCompositionStage_AttachesSnapshotMeta(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "${input.x}"}},
		},
	}
	rec := NewCompositionRecorder()
	cs := NewCompositionStageWithRecorder("snap-comp", comp, CompositionExecutorDeps{ToolRegistry: reg}, rec)

	in := make(chan StreamElement, 1)
	out := make(chan StreamElement, 10)

	msg := &types.Message{Role: "user", Content: `{"x":"hello"}`}
	in <- NewMessageElement(msg)
	close(in)

	if err := cs.Process(context.Background(), in, out); err != nil {
		t.Fatalf("Process error: %v", err)
	}

	elems := drainChannel(out)
	if len(elems) == 0 {
		t.Fatal("expected at least one output element")
	}

	// Find the assistant message element.
	var assistantMsg *types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == roleAssistant {
			assistantMsg = e.Message
			break
		}
	}
	if assistantMsg == nil {
		t.Fatal("no assistant message element emitted")
	}
	if assistantMsg.Meta == nil {
		t.Fatal("assistant message Meta is nil; expected _composition_snapshot")
	}
	raw, ok := assistantMsg.Meta[compositionSnapshotMetaKey]
	if !ok {
		t.Fatalf("Meta missing key %q; got keys: %v", compositionSnapshotMetaKey, assistantMsg.Meta)
	}
	snap, ok := raw.(*CompositionSnapshot)
	if !ok {
		t.Fatalf("Meta[%q] has type %T, want *CompositionSnapshot", compositionSnapshotMetaKey, raw)
	}
	if len(snap.Steps) == 0 {
		t.Errorf("snapshot has no steps; want at least 1")
	}
}

// TestCompositionStage_EmitsStartedAndCompleted verifies that CompositionStage
// brackets a composition execution with composition.started (before engine.Execute)
// and composition.completed (after), and that the two events appear in that order.
// Per-step events (step.started, step.completed) must appear between them.
func TestCompositionStage_EmitsStartedAndCompleted(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo") // defined in composition_executor_test.go

	// Single-step composition: simplest possible case for bracketing verification.
	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "${input}"}},
		},
	}

	bus := events.NewEventBus()
	defer bus.Close()
	em := events.NewEmitter(bus, "test-exec", "test-sess", "test-conv")

	var mu sync.Mutex
	var received []*events.Event
	bus.SubscribeAll(func(e *events.Event) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, e)
	})

	rec := NewCompositionRecorder()
	cs := NewCompositionStageWithRecorder("bracket-comp", comp,
		CompositionExecutorDeps{ToolRegistry: reg, Emitter: em}, rec)

	pipe, err := NewPipelineBuilder().Chain(cs).Build()
	if err != nil {
		t.Fatal(err)
	}

	msg := types.Message{Role: "user", Content: `{"v":"hello"}`}
	res, err := pipe.ExecuteSync(context.Background(), NewMessageElement(&msg))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Response == nil {
		t.Fatal("expected a response")
	}

	// Wait for async bus to drain. Require started, completed, AND at least one
	// step event, since async SubscribeAll delivery can deliver step events after
	// composition.completed in arrival order.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		startSeen, completedSeen, stepSeen := false, false, false
		for _, e := range received {
			switch e.Type {
			case events.EventCompositionStarted:
				startSeen = true
			case events.EventCompositionCompleted:
				completedSeen = true
			case events.EventCompositionStepStarted, events.EventCompositionStepCompleted:
				stepSeen = true
			}
		}
		done := startSeen && completedSeen && stepSeen
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	// Sort by the bus-stamped Sequence: SubscribeAll delivery is async, so arrival
	// order is not emission order. Sequence (stamped at emit) reflects emission order.
	ordered := make([]*events.Event, len(received))
	copy(ordered, received)
	sortEventsBySequence(ordered)
	receivedTypes := make([]events.EventType, len(ordered))
	for i, e := range ordered {
		receivedTypes[i] = e.Type
	}

	// Find the indices of composition.started and composition.completed.
	startIdx := -1
	completedIdx := -1
	for i, et := range receivedTypes {
		switch et {
		case events.EventCompositionStarted:
			if startIdx == -1 {
				startIdx = i
			}
		case events.EventCompositionCompleted:
			if completedIdx == -1 {
				completedIdx = i
			}
		}
	}

	if startIdx == -1 {
		t.Fatal("composition.started event not received")
	}
	if completedIdx == -1 {
		t.Fatal("composition.completed event not received")
	}

	// started must come before completed.
	if startIdx >= completedIdx {
		t.Errorf("composition.started (idx %d) must precede composition.completed (idx %d); got events: %v",
			startIdx, completedIdx, receivedTypes)
	}

	// At least one per-step event must appear between started and completed.
	stepEventBetween := false
	for i := startIdx + 1; i < completedIdx; i++ {
		if receivedTypes[i] == events.EventCompositionStepStarted ||
			receivedTypes[i] == events.EventCompositionStepCompleted {
			stepEventBetween = true
			break
		}
	}
	if !stepEventBetween {
		t.Errorf("expected at least one step event between composition.started and composition.completed; got: %v", receivedTypes)
	}
}

// sortEventsBySequence orders events by their bus-stamped Sequence (emission
// order), insulating assertions from async SubscribeAll delivery reordering.
func sortEventsBySequence(evs []*events.Event) {
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].Sequence < evs[j-1].Sequence; j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
}

// TestCompositionStage_EndToEnd_MixedKinds proves that a multi-kind composition
// runs through CompositionStage with a real mock-provider sub-pipeline:
//
//  1. classify (prompt step) — single LLM round
//  2. meta (parallel step) — two echo-tool branches, barrier-merged into "m"
//  3. synth (agent step) — depends on classify + meta, consumes ${meta.output.m},
//     produces the final composition output
func TestCompositionStage_EndToEnd_MixedKinds(t *testing.T) {
	// Mock provider whose every LLM call returns "synthesized".
	repo := mock.NewInMemoryMockRepository("synthesized")
	prov := mock.NewProviderWithRepository("mock", "mock", false, repo)

	// Prompt registry backed by the local in-package mock repository.
	preg := prompt.NewRegistryWithRepository(newMockRepo())
	registerSimplePrompt(t, preg, "classify")
	registerSimplePrompt(t, preg, "analyze")

	// Tool registry with a single echo tool used by the parallel branches.
	treg := tools.NewRegistry()
	registerEchoTool(t, treg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "synth",
		Steps: []*composition.Step{
			// Step 1: prompt step.
			{
				ID:         "classify",
				Kind:       composition.KindPrompt,
				PromptTask: "classify",
				Input:      "${input.text}",
			},
			// Step 2: parallel step — two echo-tool branches, barrier-merged into "m".
			{
				ID:   "meta",
				Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "s1", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"c": "${input.text}"}},
					{ID: "s2", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"c": "${input.text}"}},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceBarrier, Into: "m"},
			},
			// Step 3: agent step — depends on classify + meta, consumes barrier output.
			{
				ID:          "synth",
				Kind:        composition.KindAgent,
				PromptTask:  "analyze",
				DependsOn:   []string{"classify", "meta"},
				Input:       "${meta.output.m}",
				Termination: &composition.Termination{MaxSteps: 3},
			},
		},
	}

	cs := NewCompositionStage("doc", comp, CompositionExecutorDeps{
		PromptRegistry: preg,
		Provider:       prov,
		ToolRegistry:   treg,
	})

	pipe, err := NewPipelineBuilder().Chain(cs).Build()
	if err != nil {
		t.Fatal(err)
	}

	msg := types.Message{Role: "user", Content: `{"text":"a document"}`}
	res, err := pipe.ExecuteSync(context.Background(), NewMessageElement(&msg))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Response == nil || res.Response.Content == "" {
		t.Fatal("expected a non-empty composition output")
	}
	// The final agent step (analyze) drives the output; the mock provider returns
	// "synthesized". responseToJSON encodes plain text as a JSON string, so
	// Content should be the JSON-encoded form: `"synthesized"`.
	var text string
	if err := json.Unmarshal([]byte(res.Response.Content), &text); err != nil {
		t.Fatalf("expected JSON-string output from agent step, got %q: %v", res.Response.Content, err)
	}
	if text != "synthesized" {
		t.Errorf("expected final output = %q, got %q", "synthesized", text)
	}
}
