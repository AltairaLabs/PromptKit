package mock

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

func TestMockStreamSession_SendToolResponse_RecordsAndContinues(t *testing.T) {
	session := NewMockStreamSession().WithAutoRespond("continued after tool")

	// Emit turn 1 first (simulates user speech triggering the initial
	// agent response that contained the tool_call).
	if err := session.SendText(context.Background(), "hi"); err != nil {
		t.Fatalf("SendText turn 1: %v", err)
	}
	drainOne(t, session, "turn 1")

	// Runtime executes the tool and sends the result back. Without
	// MockStreamSession implementing ToolResponseSupport, this call
	// would silently no-op via type-assertion failure in
	// DuplexProviderStage and the conversation would hang.
	if err := session.SendToolResponse(context.Background(), "call_0_lookup", `{"order":"ok"}`); err != nil {
		t.Fatalf("SendToolResponse: %v", err)
	}

	// The session should have emitted the next scripted turn in
	// response. Drain it so a follow-up SendText could trigger turn 3.
	chunk := drainOne(t, session, "turn 2 (continuation)")
	if chunk.Content == "" && chunk.Delta == "" {
		t.Errorf("turn 2 chunk had no content; got %+v", chunk)
	}

	got := session.ReceivedToolResponses()
	if len(got) != 1 {
		t.Fatalf("ReceivedToolResponses len = %d, want 1", len(got))
	}
	if got[0].ToolCallID != "call_0_lookup" {
		t.Errorf("ToolCallID = %q, want call_0_lookup", got[0].ToolCallID)
	}
	if got[0].Result != `{"order":"ok"}` {
		t.Errorf("Result = %q, want %q", got[0].Result, `{"order":"ok"}`)
	}
}

func TestMockStreamSession_SendToolResponses_BatchTriggersOneContinuation(t *testing.T) {
	session := NewMockStreamSession().WithAutoRespond("after parallel tools")

	// Prime the response counter so emitAutoResponse picks turn 2.
	if err := session.SendText(context.Background(), "hi"); err != nil {
		t.Fatalf("SendText prime: %v", err)
	}
	drainOne(t, session, "prime")

	batch := []providers.ToolResponse{
		{ToolCallID: "call_0_a", Result: "1"},
		{ToolCallID: "call_1_b", Result: "2"},
	}
	if err := session.SendToolResponses(context.Background(), batch); err != nil {
		t.Fatalf("SendToolResponses: %v", err)
	}

	// Two tool responses, one continuation — matches the real-provider
	// pattern (queue function_call_output items, then one response.create).
	drainOne(t, session, "continuation")
	assertNoMoreChunks(t, session)

	got := session.ReceivedToolResponses()
	if len(got) != 2 {
		t.Fatalf("ReceivedToolResponses len = %d, want 2", len(got))
	}
}

func TestMockStreamSession_SendToolResponse_ClosedSessionErrors(t *testing.T) {
	session := NewMockStreamSession()
	_ = session.Close()

	err := session.SendToolResponse(context.Background(), "x", "y")
	if err == nil {
		t.Fatal("expected error sending tool response on closed session")
	}
}

func TestMockStreamSession_InterfaceAssertion(t *testing.T) {
	// Compile-time check is in production code; this is the runtime
	// check that DuplexProviderStage performs. If the assertion ever
	// stopped passing, every duplex tool scenario would silently fail
	// the same way that motivated this file in the first place.
	var session providers.StreamInputSession = NewMockStreamSession()
	if _, ok := session.(providers.ToolResponseSupport); !ok {
		t.Fatal("MockStreamSession must satisfy providers.ToolResponseSupport")
	}
}

func drainOne(t *testing.T, session *MockStreamSession, label string) providers.StreamChunk {
	t.Helper()
	select {
	case chunk, ok := <-session.Response():
		if !ok {
			t.Fatalf("%s: response channel closed", label)
		}
		return chunk
	default:
		t.Fatalf("%s: no chunk on response channel", label)
	}
	return providers.StreamChunk{}
}

func assertNoMoreChunks(t *testing.T, session *MockStreamSession) {
	t.Helper()
	select {
	case chunk := <-session.Response():
		t.Errorf("unexpected extra chunk on response channel: %+v", chunk)
	default:
	}
}
