package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- mock streaming conversation ---

type mockStreamConversation struct {
	mockConversation
	streamFunc func(ctx context.Context, msg *types.Message) (<-chan StreamChunk, error)
}

func (m *mockStreamConversation) Stream(ctx context.Context, msg *types.Message) (<-chan StreamChunk, error) {
	return m.streamFunc(ctx, msg)
}

// --- SSE test helpers ---

// readSSEEvents sends an RPC request and reads SSE events from the response.
func readSSEEvents(t *testing.T, ts *httptest.Server, method string, params any) []StreamEvent {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	body, err := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	defer resp.Body.Close()

	var events []StreamEvent
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		readSSE(context.Background(), resp.Body, ch)
	}()
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// --- tests ---

func TestStreamMessage_TextOnly(t *testing.T) {
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Type: StreamChunkText, Text: "Hello "}
			ch <- StreamChunk{Type: StreamChunkText, Text: "World"}
			ch <- StreamChunk{Type: StreamChunkDone}
			close(ch)
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-stream",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	// Expect: working status, 2 artifact updates, completed status
	if len(events) < 4 {
		t.Fatalf("got %d events, want at least 4", len(events))
	}

	// First event: working status.
	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != TaskStateWorking {
		t.Errorf("event 0: expected working status, got %+v", events[0])
	}

	// Middle events: artifact updates.
	if events[1].ArtifactUpdate == nil {
		t.Fatalf("event 1: expected artifact update, got %+v", events[1])
	}
	if events[1].ArtifactUpdate.Artifact.Parts[0].Text == nil || *events[1].ArtifactUpdate.Artifact.Parts[0].Text != "Hello " {
		t.Errorf("event 1: text = %v, want 'Hello '", events[1].ArtifactUpdate.Artifact.Parts[0].Text)
	}
	if !events[1].ArtifactUpdate.Append {
		t.Error("event 1: expected Append=true")
	}

	if events[2].ArtifactUpdate == nil {
		t.Fatalf("event 2: expected artifact update, got %+v", events[2])
	}
	if events[2].ArtifactUpdate.Artifact.Parts[0].Text == nil || *events[2].ArtifactUpdate.Artifact.Parts[0].Text != "World" {
		t.Errorf("event 2: text = %v, want 'World'", events[2].ArtifactUpdate.Artifact.Parts[0].Text)
	}

	// Last event: completed status.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("last event: expected completed status, got %+v", last)
	}
}

func TestStreamMessage_WithToolCalls(t *testing.T) {
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 4)
			ch <- StreamChunk{Type: StreamChunkText, Text: "thinking..."}
			ch <- StreamChunk{Type: StreamChunkToolCall}
			ch <- StreamChunk{Type: StreamChunkText, Text: "answer"}
			ch <- StreamChunk{Type: StreamChunkDone}
			close(ch)
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-tool",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	// Count artifact events — tool calls should be suppressed.
	var artifactCount int
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			artifactCount++
		}
	}

	if artifactCount != 2 {
		t.Errorf("got %d artifact events, want 2 (tool call should be suppressed)", artifactCount)
	}
}

func TestStreamMessage_Media(t *testing.T) {
	imgData := "iVBORw0KGgo="
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{
				Type: StreamChunkMedia,
				Media: &types.MediaContent{
					Data:     &imgData,
					MIMEType: "image/png",
				},
			}
			ch <- StreamChunk{Type: StreamChunkDone}
			close(ch)
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-media",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Show image")}},
		},
	})

	// Find artifact update with media.
	var found bool
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			parts := evt.ArtifactUpdate.Artifact.Parts
			if len(parts) > 0 && parts[0].MediaType != "" {
				found = true
			}
		}
	}

	if !found {
		t.Error("expected artifact update with media part")
	}
}

func TestStreamMessage_Error(t *testing.T) {
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: StreamChunkText, Text: "partial"}
			ch <- StreamChunk{Error: errors.New("stream broke")}
			close(ch)
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-err",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	// Last event should be a failed status.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != TaskStateFailed {
		t.Errorf("last event: expected failed status, got %+v", last)
	}
}

func TestStreamMessage_ClientDisconnect(t *testing.T) {
	streamStarted := make(chan struct{})
	ctxCanceled := make(chan struct{})

	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(ctx context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk)
			go func() {
				defer close(ch)
				close(streamStarted)
				<-ctx.Done()
				close(ctxCanceled)
			}()
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	paramsJSON, _ := json.Marshal(SendMessageRequest{
		Message: Message{
			ContextID: "ctx-disconnect",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})
	body, _ := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  MethodSendStreamingMessage,
		Params:  paramsJSON,
	})

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}

	// Wait for stream to start, then cancel.
	<-streamStarted
	cancel()
	resp.Body.Close()

	// The stream context should have been canceled.
	select {
	case <-ctxCanceled:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream context cancellation")
	}
}

func TestStreamMessage_NotStreamable(t *testing.T) {
	// Use a regular (non-streaming) mockConversation.
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-nostream",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	if resp.Error == nil {
		t.Fatal("expected error for non-streaming conversation")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestStreamMessage_StreamInitError(t *testing.T) {
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			return nil, errors.New("stream init failed")
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-init-err",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	// Should get working status then failed status.
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least 2", len(events))
	}

	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != TaskStateFailed {
		t.Errorf("last event: expected failed status, got %+v", last)
	}
}

func TestStreamMessage_ChannelCloseWithoutDone(t *testing.T) {
	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: StreamChunkText, Text: "partial"}
			close(ch) // Close without sending StreamChunkDone.
			return ch, nil
		},
	}

	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	events := readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
		Message: Message{
			ContextID: "ctx-nodone",
			Role:      RoleUser,
			Parts:     []Part{{Text: textPtr("Hello")}},
		},
	})

	// Should still complete successfully.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("last event: expected completed status, got %+v", last)
	}
}

func TestTaskSubscribe(t *testing.T) {
	streamReady := make(chan struct{})
	continueStream := make(chan struct{})

	mock := &mockStreamConversation{
		mockConversation: mockConversation{
			sendFunc: func(context.Context, *types.Message) (*ConversationResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(ctx context.Context, _ *types.Message) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk)
			go func() {
				defer close(ch)
				close(streamReady)
				<-continueStream
				ch <- StreamChunk{Type: StreamChunkText, Text: "result"}
				ch <- StreamChunk{Type: StreamChunkDone}
			}()
			return ch, nil
		},
	}

	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	// Start a streaming message in the background.
	var streamEvents []StreamEvent
	var streamWg sync.WaitGroup
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		streamEvents = readSSEEvents(t, ts, MethodSendStreamingMessage, SendMessageRequest{
			Message: Message{
				ContextID: "ctx-sub",
				Role:      RoleUser,
				Parts:     []Part{{Text: textPtr("Hello")}},
			},
		})
	}()

	// Wait for stream to be ready.
	<-streamReady

	// Find the task ID from the server's subs map.
	var taskID string
	srv.subsMu.Lock()
	for id := range srv.subs {
		taskID = id
		break
	}
	srv.subsMu.Unlock()

	if taskID == "" {
		t.Fatal("no broadcaster found")
	}

	// Subscribe in background.
	var subEvents []StreamEvent
	var subWg sync.WaitGroup
	subWg.Add(1)
	go func() {
		defer subWg.Done()
		subEvents = readSSEEvents(t, ts, MethodTaskSubscribe, SubscribeTaskRequest{
			ID: taskID,
		})
	}()

	// Give the subscriber time to connect.
	time.Sleep(50 * time.Millisecond)

	// Continue the stream.
	close(continueStream)

	streamWg.Wait()
	subWg.Wait()

	// Verify the main stream got events too.
	if len(streamEvents) == 0 {
		t.Fatal("main stream received no events")
	}

	// The subscriber should have received at least the artifact and completion events.
	if len(subEvents) == 0 {
		t.Fatal("subscriber received no events")
	}

	// Check that the subscriber got a completed status event.
	var gotCompleted bool
	for _, evt := range subEvents {
		if evt.StatusUpdate != nil && evt.StatusUpdate.Status.State == TaskStateCompleted {
			gotCompleted = true
		}
	}
	if !gotCompleted {
		t.Error("subscriber did not receive completed status event")
	}
}

func TestTaskSubscribe_CompletedTask(t *testing.T) {
	mock := completingMock()
	_, ts := newTestServer(func(string) (Conversation, error) { return mock, nil })
	defer ts.Close()

	// Create a completed task.
	task := sendMessage(t, ts, "ctx-completed", "Hello")

	// Subscribe to the completed task.
	events := readSSEEvents(t, ts, MethodTaskSubscribe, SubscribeTaskRequest{
		ID: task.ID,
	})

	if len(events) == 0 {
		t.Fatal("expected at least one event for completed task")
	}

	// Should get a status event with completed state.
	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != TaskStateCompleted {
		t.Errorf("expected completed status event, got %+v", events[0])
	}
}

func TestTaskSubscribe_NotFound(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	resp := rpcRequest(t, ts, MethodTaskSubscribe, SubscribeTaskRequest{
		ID: "nonexistent-task",
	})

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001", resp.Error.Code)
	}
}
