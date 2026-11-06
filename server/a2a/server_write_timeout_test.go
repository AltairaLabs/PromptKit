package a2aserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// serveOn starts srv on a loopback listener and returns its base URL; the
// server is shut down when the test ends.
func serveOn(t *testing.T, srv *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "http://" + ln.Addr().String()
}

// tryPostRPC posts one JSON-RPC request to base/a2a.
func tryPostRPC(t *testing.T, base, method string, params any) (*http.Response, error) {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: paramsJSON})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return http.Post(base+"/a2a", "application/json", bytes.NewReader(body))
}

// postRPC is tryPostRPC that fails the test on a transport error.
func postRPC(t *testing.T, base, method string, params any) *http.Response {
	t.Helper()
	resp, err := tryPostRPC(t, base, method, params)
	if err != nil {
		t.Fatalf("POST /a2a: %v", err)
	}
	return resp
}

func TestServe_AppliesWriteTimeout(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want time.Duration
	}{
		{name: "default is no write timeout", want: 0},
		{name: "option sets it", opts: []Option{WithWriteTimeout(7 * time.Second)}, want: 7 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(func(string) (Conversation, error) { return completingMock(), nil }, tt.opts...)
			base := serveOn(t, srv)

			resp, err := http.Get(base + "/healthz")
			if err != nil {
				t.Fatalf("GET /healthz: %v", err)
			}
			resp.Body.Close()

			srv.httpSrvMu.Lock()
			got := srv.httpSrv.WriteTimeout
			srv.httpSrvMu.Unlock()
			if got != tt.want {
				t.Errorf("http.Server.WriteTimeout = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListenAndServe_AppliesWriteTimeout(t *testing.T) {
	srv := NewServer(
		func(string) (Conversation, error) { return completingMock(), nil },
		WithPort(0),
		WithWriteTimeout(9*time.Second),
	)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("ListenAndServe: %v", err)
		}
	}()

	var got time.Duration
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.httpSrvMu.Lock()
		set := srv.httpSrv != nil
		if set {
			got = srv.httpSrv.WriteTimeout
		}
		srv.httpSrvMu.Unlock()
		if set {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got != 9*time.Second {
		t.Errorf("http.Server.WriteTimeout = %v, want 9s", got)
	}
}

// An SSE stream must outlive the write timeout: the timeout is for ordinary
// responses, and the stream lifts it for its own connection.
func TestServe_WriteTimeoutDoesNotCutSSEStream(t *testing.T) {
	const writeTimeout = 50 * time.Millisecond
	mock := &mockStreamConv{
		mockConv: mockConv{
			sendFunc: func(context.Context, any) (SendResult, error) {
				return nil, errors.New("should not call Send")
			},
		},
		streamFunc: func(ctx context.Context, _ any) <-chan StreamEvent {
			ch := make(chan StreamEvent)
			go func() {
				defer close(ch)
				for _, text := range []string{"one ", "two ", "three"} {
					select {
					case <-time.After(3 * writeTimeout):
					case <-ctx.Done():
						return
					}
					ch <- StreamEvent{Kind: EventText, Text: text}
				}
				ch <- StreamEvent{Kind: EventDone}
			}()
			return ch
		},
	}
	srv := NewServer(func(string) (Conversation, error) { return mock, nil }, WithWriteTimeout(writeTimeout))
	base := serveOn(t, srv)

	resp := postRPC(t, base, a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-write-timeout",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})
	defer resp.Body.Close()

	ch := make(chan a2a.StreamEvent)
	go func() {
		defer close(ch)
		a2a.ReadSSE(context.Background(), resp.Body, ch)
	}()
	var last a2a.StreamEvent
	for evt := range ch {
		last = evt
	}
	if last.StatusUpdate == nil || last.StatusUpdate.Status.State != a2a.TaskStateCompleted {
		t.Fatalf("stream ended before the turn completed (last event %+v): the write timeout cut it", last)
	}
}

// An ordinary response slower than the write timeout is cut off.
func TestServe_WriteTimeoutCutsSlowResponse(t *testing.T) {
	const writeTimeout = 50 * time.Millisecond
	conv := &mockConv{
		sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
			select {
			case <-time.After(6 * writeTimeout):
			case <-ctx.Done():
			}
			return &mockSendResult{text: "late"}, nil
		},
	}
	srv := NewServer(func(string) (Conversation, error) { return conv, nil }, WithWriteTimeout(writeTimeout))
	base := serveOn(t, srv)

	resp, err := tryPostRPC(t, base, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message: a2a.Message{
			ContextID: "ctx-slow",
			Role:      a2a.RoleUser,
			Parts:     []a2a.Part{{Text: serverTextPtr("hi")}},
		},
	})
	if err != nil {
		return // the connection was cut before a response: the timeout applied
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err == nil && len(body) > 0 {
		t.Fatalf("expected the write timeout to cut the response, got %s", body)
	}
}
