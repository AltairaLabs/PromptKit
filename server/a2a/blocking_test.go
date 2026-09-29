package a2aserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// A turn that never ends, for the question every blocking SendMessage raises:
// what if it hangs?
func hungConv(started chan<- struct{}, turnCtx chan<- context.Context) *mockConv {
	return &mockConv{sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
		turnCtx <- ctx
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
}

func TestBlockingSend_CapAnswersWithTheWorkingTask(t *testing.T) {
	started := make(chan struct{})
	turnCtx := make(chan context.Context, 1)
	srv, ts := newTestServer(func(string) (Conversation, error) { return hungConv(started, turnCtx), nil },
		WithMaxBlockingWait(50*time.Millisecond))
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	begin := time.Now()
	result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodSendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-hung-cap")}))
	assert.Less(t, time.Since(begin), 2*time.Second, "the cap must bound the wait")
	task := result["task"].(map[string]any)
	assert.Equal(t, "TASK_STATE_WORKING", task["status"].(map[string]any)["state"])

	// The turn runs on, and can still be canceled by id.
	assert.NoError(t, (<-turnCtx).Err())
	canceled := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodCancelTask, a2a.CancelTaskRequest{ID: task["id"].(string)}))
	assert.Equal(t, "TASK_STATE_CANCELED", canceled["status"].(map[string]any)["state"])
}

func TestBlockingSend_CallerDisconnectReleasesTheHandler(t *testing.T) {
	started := make(chan struct{})
	turnCtx := make(chan context.Context, 1)
	srv := NewServer(func(string) (Conversation, error) { return hungConv(started, turnCtx), nil })
	defer func() { _ = srv.Shutdown(context.Background()) }()

	handlerDone := make(chan struct{})
	h := srv.Handler()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		h.ServeHTTP(w, r)
	}))
	defer ts.Close()

	params, _ := json.Marshal(a2a.SendMessageRequest{Message: userMessage("ctx-hung-leave")})
	body, _ := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodSendMessage, Params: params})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	req.Header.Set(a2a.HeaderVersion, "1.0")
	go func() {
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	<-started
	cancel()
	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("the handler stayed blocked on a hung turn after its caller left")
	}
	assert.NoError(t, (<-turnCtx).Err(), "the turn is detached from the request and runs on")
}
