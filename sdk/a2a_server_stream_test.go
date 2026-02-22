package sdk

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// --- mock streaming conversation ---

type mockA2AStreamConv struct {
	mockA2AConv
	streamFunc func(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
}

func (m *mockA2AStreamConv) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	return m.streamFunc(ctx, message, opts...)
}

// --- SSE test helpers ---

// readA2ASSEEvents sends an RPC request and reads SSE events from the response.
func readA2ASSEEvents(t *testing.T, ts *httptest.Server, method string, params any) []a2a.StreamEvent {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	body, err := json.Marshal(a2a.JSONRPCRequest{
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

	var events []a2a.StreamEvent
	ch := make(chan a2a.StreamEvent)
	go func() {
		defer close(ch)
		a2a.ReadSSE(context.Background(), resp.Body, ch)
	}()
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// --- tests ---

func TestA2AServer_StreamMessage_TextOnly(t *testing.T) {
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Type: ChunkText, Text: "Hello "}
			ch <- StreamChunk{Type: ChunkText, Text: "World"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Expect: working status, 2 artifact updates, completed status
	if len(events) < 4 {
		t.Fatalf("got %d events, want at least 4", len(events))
	}

	// First event: working status.
	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != a2a.TaskStateWorking {
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
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateCompleted {
		t.Errorf("last event: expected completed status, got %+v", last)
	}
}

func TestA2AServer_StreamMessage_WithToolCalls(t *testing.T) {
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 4)
			ch <- StreamChunk{Type: ChunkText, Text: "thinking..."}
			ch <- StreamChunk{Type: ChunkToolCall}
			ch <- StreamChunk{Type: ChunkText, Text: "answer"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-tool",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
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

func TestA2AServer_StreamMessage_Media(t *testing.T) {
	imgData := "iVBORw0KGgo="
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{
				Type: ChunkMedia,
				Media: &types.MediaContent{
					Data:     &imgData,
					MIMEType: "image/png",
				},
			}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-media",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Show image")}},
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

func TestA2AServer_StreamMessage_Error(t *testing.T) {
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkText, Text: "partial"}
			ch <- StreamChunk{Error: errors.New("stream broke")}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-err",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Last event should be a failed status.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateFailed {
		t.Errorf("last event: expected failed status, got %+v", last)
	}
}

func TestA2AServer_StreamMessage_ClientDisconnect(t *testing.T) {
	streamStarted := make(chan struct{})
	ctxCanceled := make(chan struct{})

	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(ctx context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk)
			go func() {
				defer close(ch)
				close(streamStarted)
				<-ctx.Done()
				close(ctxCanceled)
			}()
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	paramsJSON, _ := json.Marshal(a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-disconnect",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})
	body, _ := json.Marshal(a2a.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  a2a.MethodSendStreamingMessage,
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

func TestA2AServer_StreamMessage_NotStreamable(t *testing.T) {
	// Use a regular (non-streaming) mockA2AConv.
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-nostream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	if resp.Error == nil {
		t.Fatal("expected error for non-streaming conversation")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601", resp.Error.Code)
	}
}

func TestA2AServer_StreamMessage_StreamInitError(t *testing.T) {
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			// Return a channel that immediately sends an error.
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Error: errors.New("stream init failed")}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-init-err",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Should get working status then failed status.
	if len(events) < 2 {
		t.Fatalf("got %d events, want at least 2", len(events))
	}

	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateFailed {
		t.Errorf("last event: expected failed status, got %+v", last)
	}
}

func TestA2AServer_StreamMessage_ChannelCloseWithoutDone(t *testing.T) {
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkText, Text: "partial"}
			close(ch) // Close without sending ChunkDone.
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-nodone",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Should still complete successfully.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateCompleted {
		t.Errorf("last event: expected completed status, got %+v", last)
	}
}

func TestA2AServer_TaskSubscribe(t *testing.T) {
	streamReady := make(chan struct{})
	continueStream := make(chan struct{})

	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(ctx context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk)
			go func() {
				defer close(ch)
				close(streamReady)
				<-continueStream
				ch <- StreamChunk{Type: ChunkText, Text: "result"}
				ch <- StreamChunk{Type: ChunkDone}
			}()
			return ch
		},
	}

	srv, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	// Start a streaming message in the background.
	var streamEvents []a2a.StreamEvent
	var streamWg sync.WaitGroup
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		streamEvents = readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
			Message: a2a.Message{
				ContextID: "ctx-sub",
				Role:      a2a.RoleUser,
				Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
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
	var subEvents []a2a.StreamEvent
	var subWg sync.WaitGroup
	subWg.Add(1)
	go func() {
		defer subWg.Done()
		subEvents = readA2ASSEEvents(t, ts, a2a.MethodTaskSubscribe, a2a.SubscribeTaskRequest{
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
		if evt.StatusUpdate != nil && evt.StatusUpdate.Status.State == a2a.TaskStateCompleted {
			gotCompleted = true
		}
	}
	if !gotCompleted {
		t.Error("subscriber did not receive completed status event")
	}
}

func TestA2AServer_TaskSubscribe_CompletedTask(t *testing.T) {
	mock := completingA2AMock()
	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	// Create a completed task.
	task := a2aSendMessage(t, ts, "ctx-completed", "Hello")

	// Subscribe to the completed task.
	events := readA2ASSEEvents(t, ts, a2a.MethodTaskSubscribe, a2a.SubscribeTaskRequest{
		ID: task.ID,
	})

	if len(events) == 0 {
		t.Fatal("expected at least one event for completed task")
	}

	// Should get a status event with completed state.
	if events[0].StatusUpdate == nil || events[0].StatusUpdate.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed status event, got %+v", events[0])
	}
}

func TestA2AServer_TaskSubscribe_NotFound(t *testing.T) {
	_, ts := newA2ATestServer(nopA2AOpener)
	defer ts.Close()

	resp := a2aRPCRequest(t, ts, a2a.MethodTaskSubscribe, a2a.SubscribeTaskRequest{
		ID: "nonexistent-task",
	})

	if resp.Error == nil {
		t.Fatal("expected error for nonexistent task")
	}
	if resp.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001", resp.Error.Code)
	}
}

// --- Group B: Streaming Data Integrity ---

func TestA2AServer_StreamMessage_MediaDataIntegrity(t *testing.T) {
	// Verify actual Raw bytes in artifact event match original base64 data.
	originalBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	b64 := base64.StdEncoding.EncodeToString(originalBytes)

	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{
				Type: ChunkMedia,
				Media: &types.MediaContent{
					Data:     &b64,
					MIMEType: "image/png",
				},
			}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-media-integrity",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Show image")}},
		},
	})

	var found bool
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			parts := evt.ArtifactUpdate.Artifact.Parts
			if len(parts) > 0 && parts[0].MediaType == "image/png" {
				if !bytes.Equal(parts[0].Raw, originalBytes) {
					t.Errorf("Raw bytes mismatch: got %x, want %x", parts[0].Raw, originalBytes)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("expected artifact update with image/png data")
	}
}

func TestA2AServer_StreamMessage_URLMedia(t *testing.T) {
	// Streams ChunkMedia with URL-based media (not base64).
	imgURL := "https://example.com/streamed.png"

	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{
				Type: ChunkMedia,
				Media: &types.MediaContent{
					URL:      &imgURL,
					MIMEType: "image/png",
				},
			}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-stream-url",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Show URL image")}},
		},
	})

	var found bool
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			parts := evt.ArtifactUpdate.Artifact.Parts
			if len(parts) > 0 && parts[0].URL != nil && *parts[0].URL == imgURL {
				if parts[0].MediaType != "image/png" {
					t.Errorf("mediaType = %q, want image/png", parts[0].MediaType)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("expected artifact update with URL media")
	}
}

func TestA2AServer_StreamMessage_MixedTextAndMedia(t *testing.T) {
	// Interleaved ChunkText + ChunkMedia chunks. Each produces separate
	// artifact event with correct content. Artifact IDs increment.
	imgData := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8})

	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 4)
			ch <- StreamChunk{Type: ChunkText, Text: "Here is an image:"}
			ch <- StreamChunk{
				Type: ChunkMedia,
				Media: &types.MediaContent{
					Data:     &imgData,
					MIMEType: "image/jpeg",
				},
			}
			ch <- StreamChunk{Type: ChunkText, Text: "End of response"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-mixed-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	var artifacts []a2a.TaskArtifactUpdateEvent
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			artifacts = append(artifacts, *evt.ArtifactUpdate)
		}
	}
	if len(artifacts) != 3 {
		t.Fatalf("expected 3 artifact events, got %d", len(artifacts))
	}

	// Check artifact IDs increment.
	for i, art := range artifacts {
		wantID := fmt.Sprintf("artifact-%d", i)
		if art.Artifact.ArtifactID != wantID {
			t.Errorf("artifact[%d] ID = %q, want %q", i, art.Artifact.ArtifactID, wantID)
		}
	}

	// Check content types.
	if artifacts[0].Artifact.Parts[0].Text == nil {
		t.Error("artifact[0] should be text")
	}
	if artifacts[1].Artifact.Parts[0].MediaType != "image/jpeg" {
		t.Errorf("artifact[1] mediaType = %q, want image/jpeg", artifacts[1].Artifact.Parts[0].MediaType)
	}
	if artifacts[2].Artifact.Parts[0].Text == nil {
		t.Error("artifact[2] should be text")
	}
}

func TestA2AServer_StreamMessage_NilMedia(t *testing.T) {
	// ChunkMedia with Media: nil. Silently skipped, no artifact event emitted.
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Type: ChunkMedia, Media: nil} // nil media — should be skipped
			ch <- StreamChunk{Type: ChunkText, Text: "after nil"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-nil-media",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Only the text chunk should produce an artifact event.
	var artifactCount int
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			artifactCount++
		}
	}
	if artifactCount != 1 {
		t.Errorf("got %d artifact events, want 1 (nil media should be skipped)", artifactCount)
	}
}

func TestA2AServer_StreamMessage_MediaConversionError(t *testing.T) {
	// ChunkMedia with media that has no Data and no URL. Silently skipped,
	// subsequent chunks still work.
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 4)
			// Media with no Data or URL — ContentPartToA2APart will fail.
			ch <- StreamChunk{
				Type: ChunkMedia,
				Media: &types.MediaContent{
					MIMEType: "image/png",
					// No Data, no URL — will error.
				},
			}
			ch <- StreamChunk{Type: ChunkText, Text: "still works"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-conv-err",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// The text chunk should still produce an artifact event.
	var artifactCount int
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			artifactCount++
		}
	}
	if artifactCount != 1 {
		t.Errorf("got %d artifact events, want 1 (conversion error should be skipped)", artifactCount)
	}

	// Stream should still complete.
	last := events[len(events)-1]
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateCompleted {
		t.Errorf("last event: expected completed, got %+v", last)
	}
}

func TestA2AServer_StreamMessage_ArtifactIDs(t *testing.T) {
	// 5 text chunks → artifact IDs are "artifact-0" through "artifact-4" in order.
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 6)
			for i := range 5 {
				ch <- StreamChunk{Type: ChunkText, Text: fmt.Sprintf("chunk-%d", i)}
			}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-artifact-ids",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	var artifactIDs []string
	for _, evt := range events {
		if evt.ArtifactUpdate != nil {
			artifactIDs = append(artifactIDs, evt.ArtifactUpdate.Artifact.ArtifactID)
		}
	}

	if len(artifactIDs) != 5 {
		t.Fatalf("got %d artifact events, want 5", len(artifactIDs))
	}
	for i, id := range artifactIDs {
		want := fmt.Sprintf("artifact-%d", i)
		if id != want {
			t.Errorf("artifact[%d] ID = %q, want %q", i, id, want)
		}
	}
}

func TestA2AServer_StreamMessage_TaskStoreAfterStream(t *testing.T) {
	// After streaming completes, tasks/get returns task in completed state.
	mock := &mockA2AStreamConv{
		mockA2AConv: mockA2AConv{
			sendFunc: func(context.Context, any, ...SendOption) (*Response, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkText, Text: "streamed result"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	_, ts := newA2ATestServer(func(string) (a2aConv, error) { return mock, nil })
	defer ts.Close()

	events := readA2ASSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-store-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("Hello")}},
		},
	})

	// Find the task ID from the events.
	var taskID string
	for _, evt := range events {
		if evt.StatusUpdate != nil {
			taskID = evt.StatusUpdate.TaskID
			break
		}
	}
	if taskID == "" {
		t.Fatal("no task ID found in stream events")
	}

	// Verify via tasks/get.
	got := a2aRPCRequestTask(t, ts, a2a.MethodGetTask, a2a.GetTaskRequest{ID: taskID})
	if got.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got.Status.State)
	}
	if got.ID != taskID {
		t.Fatalf("task ID = %q, want %q", got.ID, taskID)
	}
}
