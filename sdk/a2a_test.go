package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

func TestA2AOpener(t *testing.T) {
	// A2AOpener returns an A2AConversationOpener function. We can verify the
	// function signature is correct by assigning it to the interface type.
	// Actually calling it would require a valid pack file, so we just
	// verify the type contract.
	var opener A2AConversationOpener = A2AOpener("nonexistent.pack.json", "prompt")
	_, err := opener("ctx-1")
	if err == nil {
		t.Fatal("expected error for nonexistent pack file")
	}
}

// --- responseAdapter tests ---

func TestResponseAdapter_NoPendingTools(t *testing.T) {
	resp := &Response{
		message: &types.Message{
			Role:  "assistant",
			Parts: []types.ContentPart{types.NewTextPart("hello")},
		},
	}
	adapter := &responseAdapter{r: resp}

	if adapter.HasPendingTools() {
		t.Error("expected HasPendingTools() = false")
	}
	if adapter.Text() != "hello" {
		t.Errorf("expected Text() = %q, got %q", "hello", adapter.Text())
	}
	parts := adapter.Parts()
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
}

func TestResponseAdapter_WithPendingTools(t *testing.T) {
	resp := &Response{
		message: &types.Message{
			Role:  "assistant",
			Parts: []types.ContentPart{types.NewTextPart("thinking...")},
		},
		pendingTools: []PendingTool{{ID: "call_1", Name: "search", Arguments: map[string]any{}}},
	}
	adapter := &responseAdapter{r: resp}

	if !adapter.HasPendingTools() {
		t.Error("expected HasPendingTools() = true")
	}
}

// --- chunkToEvent tests ---

func TestChunkToEvent_Text(t *testing.T) {
	evt := chunkToEvent(StreamChunk{Type: ChunkText, Text: "hello"})
	if evt.Kind != a2aserver.EventText {
		t.Errorf("expected EventText, got %v", evt.Kind)
	}
	if evt.Text != "hello" {
		t.Errorf("expected text %q, got %q", "hello", evt.Text)
	}
}

func TestChunkToEvent_Media(t *testing.T) {
	fakeData := "ZmFrZQ=="
	media := &types.MediaContent{MIMEType: "image/png", Data: &fakeData}
	evt := chunkToEvent(StreamChunk{Type: ChunkMedia, Media: media})
	if evt.Kind != a2aserver.EventMedia {
		t.Errorf("expected EventMedia, got %v", evt.Kind)
	}
	if evt.Media != media {
		t.Error("expected media to be passed through")
	}
}

func TestChunkToEvent_ToolCall(t *testing.T) {
	evt := chunkToEvent(StreamChunk{Type: ChunkToolCall})
	if evt.Kind != a2aserver.EventToolCall {
		t.Errorf("expected EventToolCall, got %v", evt.Kind)
	}
}

func TestChunkToEvent_Done(t *testing.T) {
	evt := chunkToEvent(StreamChunk{Type: ChunkDone})
	if evt.Kind != a2aserver.EventDone {
		t.Errorf("expected EventDone, got %v", evt.Kind)
	}
}

func TestChunkToEvent_Error(t *testing.T) {
	testErr := errors.New("boom")
	evt := chunkToEvent(StreamChunk{Error: testErr})
	if evt.Error != testErr {
		t.Errorf("expected error %v, got %v", testErr, evt.Error)
	}
}

func TestChunkToEvent_Unknown(t *testing.T) {
	evt := chunkToEvent(StreamChunk{Type: ChunkType(999)})
	if evt.Kind != 0 {
		t.Errorf("expected zero-value event kind for unknown chunk type, got %v", evt.Kind)
	}
}

// --- Re-exported option functions tests ---

func TestNewA2AServer_WithOptions(t *testing.T) {
	opener := func(string) (a2aserver.Conversation, error) {
		return nil, errors.New("not implemented")
	}
	card := &a2a.AgentCard{Name: "test-agent"}

	srv := NewA2AServer(
		opener,
		WithA2ACard(card),
		WithA2APort(9090),
		WithA2ATaskStore(NewInMemoryA2ATaskStore()),
		WithA2AReadTimeout(5*time.Second),
		WithA2AWriteTimeout(10*time.Second),
		WithA2AIdleTimeout(30*time.Second),
		WithA2AMaxBodySize(1<<20),
		WithA2ATaskTTL(2*time.Hour),
		WithA2AConversationTTL(3*time.Hour),
	)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}

	// Verify the handler works by fetching the agent card.
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent.json")
	if err != nil {
		t.Fatalf("GET agent card: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var gotCard a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&gotCard); err != nil {
		t.Fatalf("decode agent card: %v", err)
	}
	if gotCard.Name != "test-agent" {
		t.Errorf("expected agent name %q, got %q", "test-agent", gotCard.Name)
	}
}

func TestNewA2AServer_SendMessage(t *testing.T) {
	// Create a mock conversation that implements a2aserver.Conversation.
	mock := &a2aTestConv{}
	opener := func(string) (a2aserver.Conversation, error) {
		return mock, nil
	}

	srv := NewA2AServer(opener)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Send a JSON-RPC message via the A2A client.
	client := a2a.NewClient(ts.URL)
	task, err := client.SendMessage(context.Background(), &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: a2aTextPtr("hi")}},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}

	// Verify we got back the assistant's response.
	found := false
	for _, art := range task.Artifacts {
		for _, p := range art.Parts {
			if p.Text != nil && strings.Contains(*p.Text, "mock response") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected artifact containing %q in task %+v", "mock response", task)
	}
}

// --- Re-exported sentinel errors ---

func TestA2ASentinelErrors(t *testing.T) {
	if ErrTaskNotFound == nil {
		t.Error("ErrTaskNotFound should not be nil")
	}
	if ErrTaskAlreadyExists == nil {
		t.Error("ErrTaskAlreadyExists should not be nil")
	}
	if ErrInvalidTransition == nil {
		t.Error("ErrInvalidTransition should not be nil")
	}
	if ErrTaskTerminal == nil {
		t.Error("ErrTaskTerminal should not be nil")
	}
}

// --- convAdapter tests ---

func TestConvAdapter_Send(t *testing.T) {
	conv := newTestConversation()
	adapter := &convAdapter{c: conv}

	result, err := adapter.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error from Send: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// The result should implement SendResult.
	if result.Text() == "" && len(result.Parts()) == 0 {
		// OK — the test conversation may return empty, that's fine.
	}
}

func TestConvAdapter_Close(t *testing.T) {
	conv := newTestConversation()
	adapter := &convAdapter{c: conv}

	if err := adapter.Close(); err != nil {
		t.Fatalf("unexpected error from Close: %v", err)
	}
}

func TestStreamConvAdapter_Send(t *testing.T) {
	conv := newTestConversation()
	adapter := &streamConvAdapter{convAdapter: convAdapter{c: conv}}

	result, err := adapter.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("unexpected error from Send: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestStreamConvAdapter_Close(t *testing.T) {
	conv := newTestConversation()
	adapter := &streamConvAdapter{convAdapter: convAdapter{c: conv}}

	if err := adapter.Close(); err != nil {
		t.Fatalf("unexpected error from Close: %v", err)
	}
}

func TestStreamConvAdapter_Stream(t *testing.T) {
	conv := newTestConversation()
	adapter := &streamConvAdapter{convAdapter: convAdapter{c: conv}}

	events := adapter.Stream(context.Background(), "hello")
	if events == nil {
		t.Fatal("expected non-nil channel")
	}

	// Drain the channel — it should close eventually (may contain error events
	// since no provider is wired, but the goroutine conversion path is exercised).
	for range events {
		// consume all events
	}
}

func TestConvAdapter_SendReturnsError(t *testing.T) {
	conv := newTestConversation()
	// Close the conversation first so Send will fail.
	_ = conv.Close()
	adapter := &convAdapter{c: conv}

	_, err := adapter.Send(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from Send on closed conversation")
	}
}

// --- test helpers ---

// a2aTestConv implements a2aserver.Conversation for testing.
type a2aTestConv struct{}

func (m *a2aTestConv) Send(_ context.Context, _ any) (a2aserver.SendResult, error) {
	return &a2aTestResult{}, nil
}

func (m *a2aTestConv) Close() error { return nil }

type a2aTestResult struct{}

func (r *a2aTestResult) HasPendingTools() bool       { return false }
func (r *a2aTestResult) Parts() []types.ContentPart { return []types.ContentPart{types.NewTextPart("mock response")} }
func (r *a2aTestResult) Text() string               { return "mock response" }

func a2aTextPtr(s string) *string { return &s }
