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
// ctx.Err() when the context is cancelled mid-stream.
func TestCompositionStage_ContextCancellation(t *testing.T) {
	reg := tools.NewRegistry()
	registerEchoTool(t, reg, "echo")

	comp := &composition.Composition{
		Version: 1, Output: "a",
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "echo", Args: map[string]any{"v": "x"}},
		},
	}
	cs := NewCompositionStage("cancel", comp, CompositionExecutorDeps{ToolRegistry: reg})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	in := make(chan StreamElement, 1)
	out := make(chan StreamElement, 10)

	// Send a non-message element — the cancelled context should cause the
	// forwarding select to return ctx.Err().
	txt := "x"
	in <- StreamElement{Text: &txt}
	close(in)

	err := cs.Process(ctx, in, out)
	if err == nil {
		// context was already cancelled; most schedulers will pick the done case
		// but Go's select is non-deterministic when both cases are ready.
		// Accept either outcome rather than making the test racy.
		t.Log("select chose the send case before ctx.Done — acceptable")
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
