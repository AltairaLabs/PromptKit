package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockSDKConv implements sdkConv for testing.
type mockSDKConv struct {
	sendFn   func(ctx context.Context, message any, opts ...SendOption) (*Response, error)
	streamFn func(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk
	closeFn  func() error
}

func (m *mockSDKConv) Send(ctx context.Context, message any, opts ...SendOption) (*Response, error) {
	return m.sendFn(ctx, message, opts...)
}

func (m *mockSDKConv) Stream(ctx context.Context, message any, opts ...SendOption) <-chan StreamChunk {
	return m.streamFn(ctx, message, opts...)
}

func (m *mockSDKConv) Close() error {
	return m.closeFn()
}

func TestA2AAdapter_Send(t *testing.T) {
	mock := &mockSDKConv{
		sendFn: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{
					Role: "assistant",
					Parts: []types.ContentPart{
						{Type: "text", Text: a2aStrPtr("hello")},
					},
				},
			}, nil
		},
	}

	adapter := &A2AAdapter{conv: mock}
	result, err := adapter.Send(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result.Parts))
	}
	if *result.Parts[0].Text != "hello" {
		t.Errorf("expected text 'hello', got %q", *result.Parts[0].Text)
	}
	if result.PendingTools {
		t.Error("expected PendingTools=false")
	}
}

func TestA2AAdapter_Send_PendingTools(t *testing.T) {
	mock := &mockSDKConv{
		sendFn: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return &Response{
				message: &types.Message{Role: "assistant"},
				pendingTools: []PendingTool{
					{ID: "tool-1", Name: "approve"},
				},
			}, nil
		},
	}

	adapter := &A2AAdapter{conv: mock}
	result, err := adapter.Send(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.PendingTools {
		t.Error("expected PendingTools=true")
	}
}

func TestA2AAdapter_Send_Error(t *testing.T) {
	wantErr := errors.New("send failed")
	mock := &mockSDKConv{
		sendFn: func(_ context.Context, _ any, _ ...SendOption) (*Response, error) {
			return nil, wantErr
		},
	}

	adapter := &A2AAdapter{conv: mock}
	_, err := adapter.Send(context.Background(), &types.Message{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

func TestA2AAdapter_Stream_Text(t *testing.T) {
	mock := &mockSDKConv{
		streamFn: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkText, Text: "hello"}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	adapter := &A2AAdapter{conv: mock}
	ch, err := adapter.Stream(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if chunk.Type != a2a.StreamChunkText {
		t.Errorf("expected StreamChunkText, got %d", chunk.Type)
	}
	if chunk.Text != "hello" {
		t.Errorf("expected text 'hello', got %q", chunk.Text)
	}

	chunk = <-ch
	if chunk.Type != a2a.StreamChunkDone {
		t.Errorf("expected StreamChunkDone, got %d", chunk.Type)
	}

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestA2AAdapter_Stream_Media(t *testing.T) {
	data := "base64data"
	media := &types.MediaContent{MIMEType: "image/png", Data: &data}
	mock := &mockSDKConv{
		streamFn: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkMedia, Media: media}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	adapter := &A2AAdapter{conv: mock}
	ch, err := adapter.Stream(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if chunk.Type != a2a.StreamChunkMedia {
		t.Errorf("expected StreamChunkMedia, got %d", chunk.Type)
	}
	if chunk.Media != media {
		t.Error("expected media pointer to match")
	}
}

func TestA2AAdapter_Stream_ToolCall(t *testing.T) {
	mock := &mockSDKConv{
		streamFn: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Type: ChunkToolCall, ToolCall: &types.MessageToolCall{Name: "search"}}
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	adapter := &A2AAdapter{conv: mock}
	ch, err := adapter.Stream(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if chunk.Type != a2a.StreamChunkToolCall {
		t.Errorf("expected StreamChunkToolCall, got %d", chunk.Type)
	}
}

func TestA2AAdapter_Stream_Done(t *testing.T) {
	mock := &mockSDKConv{
		streamFn: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Type: ChunkDone}
			close(ch)
			return ch
		},
	}

	adapter := &A2AAdapter{conv: mock}
	ch, err := adapter.Stream(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if chunk.Type != a2a.StreamChunkDone {
		t.Errorf("expected StreamChunkDone, got %d", chunk.Type)
	}
}

func TestA2AAdapter_Stream_Error(t *testing.T) {
	wantErr := errors.New("stream error")
	mock := &mockSDKConv{
		streamFn: func(_ context.Context, _ any, _ ...SendOption) <-chan StreamChunk {
			ch := make(chan StreamChunk, 1)
			ch <- StreamChunk{Error: wantErr}
			close(ch)
			return ch
		},
	}

	adapter := &A2AAdapter{conv: mock}
	ch, err := adapter.Stream(context.Background(), &types.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk := <-ch
	if !errors.Is(chunk.Error, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, chunk.Error)
	}
}

func TestA2AAdapter_Close(t *testing.T) {
	called := false
	mock := &mockSDKConv{
		closeFn: func() error {
			called = true
			return nil
		},
	}

	adapter := &A2AAdapter{conv: mock}
	if err := adapter.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected Close to be called on underlying conv")
	}
}

func TestA2AAdapter_Close_Error(t *testing.T) {
	wantErr := errors.New("close failed")
	mock := &mockSDKConv{
		closeFn: func() error {
			return wantErr
		},
	}

	adapter := &A2AAdapter{conv: mock}
	err := adapter.Close()
	if !errors.Is(err, wantErr) {
		t.Errorf("expected error %v, got %v", wantErr, err)
	}
}

func TestA2AOpener(t *testing.T) {
	// A2AOpener returns a ConversationOpener function. We can verify the
	// function signature is correct by assigning it to the interface type.
	// Actually calling it would require a valid pack file, so we just
	// verify the type contract.
	var opener a2a.ConversationOpener = A2AOpener("nonexistent.pack.json", "prompt")
	_, err := opener("ctx-1")
	if err == nil {
		t.Fatal("expected error for nonexistent pack file")
	}
}

// a2aStrPtr is a test helper that returns a pointer to a string.
func a2aStrPtr(s string) *string { return &s }
