package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a"
)

// --- mock SDK response for adapter tests ---

type mockSDKResponse struct {
	text              string
	parts             []types.ContentPart
	pendingTools      []PendingTool
	pendingClientBool bool
	clientTools       []PendingClientTool
}

func (r *mockSDKResponse) Text() string                     { return r.text }
func (r *mockSDKResponse) Parts() []types.ContentPart       { return r.parts }
func (r *mockSDKResponse) PendingTools() []PendingTool      { return r.pendingTools }
func (r *mockSDKResponse) HasPendingClientTools() bool      { return r.pendingClientBool }
func (r *mockSDKResponse) ClientTools() []PendingClientTool { return r.clientTools }

// --- mock conversationBackend ---

type mockConvBackend struct {
	sendFunc           func(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	closeFunc          func() error
	sendToolResultFunc func(ctx context.Context, callID string, result any) error
	rejectClientFunc   func(ctx context.Context, callID, reason string)
	resumeFunc         func(ctx context.Context) (*Response, error)
	resumeStreamFunc   func(ctx context.Context) <-chan StreamChunk
	streamFunc         func(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
}

func (m *mockConvBackend) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	return m.sendFunc(ctx, message, opts...)
}

func (m *mockConvBackend) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func (m *mockConvBackend) SendToolResult(ctx context.Context, callID string, result any) error {
	return m.sendToolResultFunc(ctx, callID, result)
}

func (m *mockConvBackend) RejectClientTool(ctx context.Context, callID, reason string) {
	m.rejectClientFunc(ctx, callID, reason)
}

func (m *mockConvBackend) Resume(ctx context.Context) (*Response, error) {
	return m.resumeFunc(ctx)
}

func (m *mockConvBackend) ResumeStream(ctx context.Context) <-chan StreamChunk {
	return m.resumeStreamFunc(ctx)
}

func (m *mockConvBackend) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	return m.streamFunc(ctx, message, opts...)
}

// --- responseAdapter tests ---

func TestResponseAdapter_HasPendingTools_IncludesClientTools(t *testing.T) {
	resp := &Response{}
	adapter := &responseAdapter{r: resp}
	if adapter.HasPendingTools() {
		t.Error("expected false when no tools pending")
	}

	resp = &Response{
		clientTools: []PendingClientTool{
			{CallID: "c1", ToolName: "gps"},
		},
	}
	adapter = &responseAdapter{r: resp}
	if !adapter.HasPendingTools() {
		t.Error("expected true when client tools pending")
	}
}

func TestResponseAdapter_HasPendingClientTools(t *testing.T) {
	resp := &Response{}
	adapter := &responseAdapter{r: resp}
	if adapter.HasPendingClientTools() {
		t.Error("expected false when no client tools")
	}

	resp = &Response{
		clientTools: []PendingClientTool{
			{CallID: "c1", ToolName: "camera"},
		},
	}
	adapter = &responseAdapter{r: resp}
	if !adapter.HasPendingClientTools() {
		t.Error("expected true when client tools present")
	}
}

func TestResponseAdapter_PendingClientTools(t *testing.T) {
	resp := &Response{
		clientTools: []PendingClientTool{
			{CallID: "c1", ToolName: "gps", Args: map[string]any{"accuracy": "high"}, ConsentMsg: "Allow GPS?"},
			{CallID: "c2", ToolName: "camera"},
		},
	}
	adapter := &responseAdapter{r: resp}
	tools := adapter.PendingClientTools()

	if len(tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(tools))
	}
	if tools[0].CallID != "c1" || tools[0].ToolName != "gps" {
		t.Errorf("tool[0] = %+v, want {c1 gps ...}", tools[0])
	}
	if tools[0].ConsentMsg != "Allow GPS?" {
		t.Errorf("consent = %q, want 'Allow GPS?'", tools[0].ConsentMsg)
	}
	if tools[1].CallID != "c2" || tools[1].ToolName != "camera" {
		t.Errorf("tool[1] = %+v, want {c2 camera}", tools[1])
	}
}

func TestResponseAdapter_PendingClientTools_Empty(t *testing.T) {
	resp := &Response{}
	adapter := &responseAdapter{r: resp}
	tools := adapter.PendingClientTools()
	if tools != nil {
		t.Errorf("expected nil, got %+v", tools)
	}
}

// --- chunkToEvent tests ---

func TestChunkToEvent_ClientTool(t *testing.T) {
	chunk := StreamChunk{
		Type: ChunkClientTool,
		ClientTool: &PendingClientTool{
			CallID:     "call-1",
			ToolName:   "get_location",
			Args:       map[string]any{"accuracy": "high"},
			ConsentMsg: "Allow location?",
		},
	}

	evt := chunkToEvent(chunk)
	if evt.Kind != a2aserver.EventClientTool {
		t.Fatalf("kind = %d, want EventClientTool", evt.Kind)
	}
	if evt.ClientTool == nil {
		t.Fatal("expected non-nil ClientTool")
	}
	if evt.ClientTool.CallID != "call-1" {
		t.Errorf("call ID = %q, want call-1", evt.ClientTool.CallID)
	}
	if evt.ClientTool.ToolName != "get_location" {
		t.Errorf("tool name = %q, want get_location", evt.ClientTool.ToolName)
	}
	if evt.ClientTool.ConsentMsg != "Allow location?" {
		t.Errorf("consent = %q, want 'Allow location?'", evt.ClientTool.ConsentMsg)
	}
}

func TestChunkToEvent_ClientTool_Nil(t *testing.T) {
	chunk := StreamChunk{
		Type:       ChunkClientTool,
		ClientTool: nil,
	}
	evt := chunkToEvent(chunk)
	if evt.Kind != a2aserver.EventClientTool {
		t.Fatalf("kind = %d, want EventClientTool", evt.Kind)
	}
	if evt.ClientTool != nil {
		t.Error("expected nil ClientTool for nil input")
	}
}

func TestChunkToEvent_ExistingTypes(t *testing.T) {
	tests := []struct {
		name string
		in   StreamChunk
		want a2aserver.EventKind
	}{
		{"text", StreamChunk{Type: ChunkText, Text: "hi"}, a2aserver.EventText},
		{"tool_call", StreamChunk{Type: ChunkToolCall}, a2aserver.EventToolCall},
		{"done", StreamChunk{Type: ChunkDone}, a2aserver.EventDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := chunkToEvent(tt.in)
			if evt.Kind != tt.want {
				t.Errorf("kind = %d, want %d", evt.Kind, tt.want)
			}
		})
	}
}

// --- convAdapter tests ---

func TestConvAdapter_SendToolResult(t *testing.T) {
	var capturedCallID string
	var capturedResult any
	mock := &mockConvBackend{
		sendToolResultFunc: func(_ context.Context, callID string, result any) error {
			capturedCallID = callID
			capturedResult = result
			return nil
		},
	}
	adapter := &convAdapter{c: mock}

	err := adapter.SendToolResult("call-1", map[string]any{"lat": 40.7})
	if err != nil {
		t.Fatalf("SendToolResult error: %v", err)
	}
	if capturedCallID != "call-1" {
		t.Errorf("callID = %q, want call-1", capturedCallID)
	}
	if capturedResult == nil {
		t.Error("expected non-nil result")
	}
}

func TestConvAdapter_SendToolResult_Error(t *testing.T) {
	wantErr := errors.New("serialize error")
	mock := &mockConvBackend{
		sendToolResultFunc: func(_ context.Context, _ string, _ any) error {
			return wantErr
		},
	}
	adapter := &convAdapter{c: mock}

	err := adapter.SendToolResult("call-1", "data")
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestConvAdapter_RejectClientTool(t *testing.T) {
	var capturedCallID, capturedReason string
	mock := &mockConvBackend{
		rejectClientFunc: func(_ context.Context, callID, reason string) {
			capturedCallID = callID
			capturedReason = reason
		},
	}
	adapter := &convAdapter{c: mock}

	adapter.RejectClientTool("call-2", "denied by user")
	if capturedCallID != "call-2" {
		t.Errorf("callID = %q, want call-2", capturedCallID)
	}
	if capturedReason != "denied by user" {
		t.Errorf("reason = %q, want 'denied by user'", capturedReason)
	}
}

func TestConvAdapter_Resume(t *testing.T) {
	mock := &mockConvBackend{
		resumeFunc: func(_ context.Context) (*Response, error) {
			return &Response{
				message: &types.Message{Parts: []types.ContentPart{types.NewTextPart("resumed")}},
			}, nil
		},
	}
	adapter := &convAdapter{c: mock}

	result, err := adapter.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume error: %v", err)
	}
	if result.Text() != "resumed" {
		t.Errorf("text = %q, want 'resumed'", result.Text())
	}
}

func TestConvAdapter_Resume_Error(t *testing.T) {
	wantErr := errors.New("resume failed")
	mock := &mockConvBackend{
		resumeFunc: func(_ context.Context) (*Response, error) {
			return nil, wantErr
		},
	}
	adapter := &convAdapter{c: mock}

	_, err := adapter.Resume(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestConvAdapter_ResumeStream(t *testing.T) {
	mock := &mockConvBackend{
		resumeStreamFunc: func(_ context.Context) <-chan StreamChunk {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Type: ChunkText, Text: "resumed text"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}
	adapter := &convAdapter{c: mock}

	var events []a2aserver.StreamEvent
	for evt := range adapter.ResumeStream(context.Background()) {
		events = append(events, evt)
	}

	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Kind != a2aserver.EventText || events[0].Text != "resumed text" {
		t.Errorf("event[0] = %+v, want text='resumed text'", events[0])
	}
	if events[1].Kind != a2aserver.EventDone {
		t.Errorf("event[1] kind = %d, want EventDone", events[1].Kind)
	}
}

// Verify convAdapter implements ResumableConversation at compile time.
var _ a2aserver.ResumableConversation = (*convAdapter)(nil)

// Verify streamConvAdapter also implements ResumableConversation.
var _ a2aserver.ResumableConversation = (*streamConvAdapter)(nil)
