package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
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
