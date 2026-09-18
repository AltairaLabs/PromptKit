package a2aserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerValueKey stands in for whatever a caller's HTTP middleware puts on the
// request context: an authenticated identity, a tenant, request-scoped config.
type callerValueKey struct{}

// withCallerValue returns middleware that puts value on every request context,
// the way an authenticating wrapper around the A2A handler would.
func withCallerValue(next http.Handler, value string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerValueKey{}, value)))
	})
}

func newTestServerWithCallerValue(opener ConversationOpener, value string) *httptest.Server {
	srv := NewServer(opener)
	return httptest.NewServer(withCallerValue(srv.Handler(), value))
}

// TestServer_SendMessage_PreservesCallerContextValues pins the contract that
// detaching the conversation goroutine from the request drops its cancellation
// and nothing else. Issue #2018: context.Background() also dropped every value,
// so message/send saw no caller identity while message/stream did.
func TestServer_SendMessage_PreservesCallerContextValues(t *testing.T) {
	gotValue := make(chan any, 1)
	mock := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			gotValue <- ctx.Value(callerValueKey{})
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}

	ts := newTestServerWithCallerValue(func(string) (Conversation, error) { return mock, nil }, "caller-identity")
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-values-send", "hello")

	select {
	case v := <-gotValue:
		if v != "caller-identity" {
			t.Fatalf("Send saw caller value %v, want %q", v, "caller-identity")
		}
	case <-time.After(time.Second):
		t.Fatal("Send was never called")
	}
}

// TestServer_Stream_PreservesCallerContextValues is the parity half: the
// streaming path derives from the request context and always carried values.
func TestServer_Stream_PreservesCallerContextValues(t *testing.T) {
	gotValue := make(chan any, 1)
	mock := &mockStreamConv{
		streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
			gotValue <- ctx.Value(callerValueKey{})
			ch := make(chan StreamEvent, 2)
			ch <- StreamEvent{Kind: EventText, Text: "ok"}
			ch <- StreamEvent{Kind: EventDone}
			close(ch)
			return ch
		},
	}

	ts := newTestServerWithCallerValue(func(string) (Conversation, error) { return mock, nil }, "caller-identity")
	defer ts.Close()

	readSSEEvents(t, ts, a2a.MethodSendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-values-stream",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hello")}},
		},
	})

	select {
	case v := <-gotValue:
		if v != "caller-identity" {
			t.Fatalf("Stream saw caller value %v, want %q", v, "caller-identity")
		}
	case <-time.After(time.Second):
		t.Fatal("Stream was never called")
	}
}

// TestServer_ToolResult_PreservesCallerContextValues covers the second detached
// site: resuming after a client-side tool result.
func TestServer_ToolResult_PreservesCallerContextValues(t *testing.T) {
	gotValue := make(chan any, 1)
	mock := &mockResumableConv{
		mockConv: mockConv{
			sendFunc: func(_ context.Context, _ any) (SendResult, error) {
				return &mockSendResult{
					hasPending:       true,
					hasPendingClient: true,
					pendingClientTools: []PendingClientToolInfo{
						{CallID: "call-1", ToolName: "get_location"},
					},
				}, nil
			},
		},
		resumeFunc: func(ctx context.Context) (SendResult, error) {
			gotValue <- ctx.Value(callerValueKey{})
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("You are in NYC")},
				text:  "You are in NYC",
			}, nil
		},
	}

	ts := newTestServerWithCallerValue(func(string) (Conversation, error) { return mock, nil }, "caller-identity")
	defer ts.Close()

	a2aSendMessage(t, ts, "ctx-values-resume", "Where am I?")

	a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-values-resume",
			Role:      a2a.RoleUser,
			Parts: []a2a.Part{
				{
					Metadata: map[string]any{
						"tool_call_id": "call-1",
						"tool_result":  map[string]any{"lat": 40.7},
					},
				},
			},
		},
		Configuration: &a2a.SendMessageConfiguration{Blocking: true},
	})

	select {
	case v := <-gotValue:
		if v != "caller-identity" {
			t.Fatalf("Resume saw caller value %v, want %q", v, "caller-identity")
		}
	case <-time.After(time.Second):
		t.Fatal("Resume was never called")
	}
}

// TestServer_SendMessage_SurvivesRequestCancellation is the other half of the
// contract: the conversation goroutine outlives the HTTP handler on the
// non-blocking path, so the request's cancellation must not reach it.
func TestServer_SendMessage_SurvivesRequestCancellation(t *testing.T) {
	gotCtx := make(chan context.Context, 1)
	release := make(chan struct{})
	mock := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			gotCtx <- ctx
			<-release
			return &mockSendResult{
				parts: []types.ContentPart{types.NewTextPart("ok")},
				text:  "ok",
			}, nil
		},
	}

	ts := newTestServerWithCallerValue(func(string) (Conversation, error) { return mock, nil }, "caller-identity")
	defer ts.Close()
	defer close(release)

	// Non-blocking: the handler returns after the settle time while Send is
	// still parked on release, which cancels the request context.
	a2aRPCRequestTask(t, ts, a2a.MethodSendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-values-cancel",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hello")}},
		},
	})

	var convCtx context.Context
	select {
	case convCtx = <-gotCtx:
	case <-time.After(time.Second):
		t.Fatal("Send was never called")
	}

	// Give the server time to tear the request down before sampling.
	time.Sleep(50 * time.Millisecond)
	if err := convCtx.Err(); err != nil {
		t.Fatalf("conversation context was canceled with the request: %v", err)
	}
}
