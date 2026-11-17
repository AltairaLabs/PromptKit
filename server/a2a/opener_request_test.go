package a2aserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func okConv() *mockConv {
	return &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("ok")}, text: "ok"}, nil
	}}
}

func newServerForTest(t *testing.T, opener RequestConversationOpener) *httptest.Server {
	t.Helper()
	srv := NewServerFor(ownerFromHeader, opener)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})
	return ts
}

func sendAs(t *testing.T, ts *httptest.Server, caller, contextID string) *a2a.JSONRPCResponse {
	t.Helper()
	return as(t, ts, caller, a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage(contextID)})
}

// NewServerFor gives the opener the request that opens each context, once per
// context: the caller the host identified on it opens that conversation.
func TestNewServerFor_OpenerSeesTheRequestOpeningEachContext(t *testing.T) {
	var mu sync.Mutex
	opened := map[string]string{} // context ID -> caller header of the opening request
	opens := 0
	ts := newServerForTest(t, func(r *http.Request, contextID string) (Conversation, error) {
		mu.Lock()
		defer mu.Unlock()
		opens++
		opened[contextID] = r.Header.Get(callerHeader)
		return okConv(), nil
	})

	for _, msg := range []struct{ caller, contextID string }{
		{"alice", "ctx-alice"}, {"bob", "ctx-bob"}, {"alice", "ctx-alice"},
	} {
		resp := sendAs(t, ts, msg.caller, msg.contextID)
		require.Nil(t, resp.Error, "%s into %s", msg.caller, msg.contextID)
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, map[string]string{"ctx-alice": "alice", "ctx-bob": "bob"}, opened)
	assert.Equal(t, 2, opens, "a context's conversation is opened once")
}

// Another caller naming a context never reaches the conversation opened for
// the caller who opened it, without WithTaskOwner.
func TestNewServerFor_RefusesAnotherCallerIntoAContext(t *testing.T) {
	var sends atomic.Int32
	ts := newServerForTest(t, func(*http.Request, string) (Conversation, error) {
		return &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
			sends.Add(1)
			return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("ok")}, text: "ok"}, nil
		}}, nil
	})

	require.Nil(t, sendAs(t, ts, "alice", "ctx-1").Error)
	resp := sendAs(t, ts, "bob", "ctx-1")

	requireCode(t, resp, a2a.ErrCodeInvalidParams, "bob into alice's context")
	assert.Equal(t, "Invalid params: contextId is not available to this caller", resp.Error.Message)
	assert.Equal(t, int32(1), sends.Load(), "bob's message never reaches alice's conversation")
}

// A request with no caller identity is refused before any conversation opens.
func TestNewServerFor_RefusesARequestWithoutACaller(t *testing.T) {
	var opens atomic.Int32
	ts := newServerForTest(t, func(*http.Request, string) (Conversation, error) {
		opens.Add(1)
		return okConv(), nil
	})

	resp := sendAs(t, ts, "", "ctx-1")

	require.NotNil(t, resp.Error)
	assert.Equal(t, -32000, resp.Error.Code)
	assert.Equal(t, "Caller identity required", resp.Error.Message)
	assert.Equal(t, int32(0), opens.Load())
}

// A slow open of one context does not hold up another context, and
// concurrent messages into one new context open it once.
func TestNewServerFor_OpensContextsConcurrentlyAndEachOnce(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	slowStarted := make(chan struct{})
	var slowOpens atomic.Int32
	ts := newServerForTest(t, func(_ *http.Request, contextID string) (Conversation, error) {
		if contextID == "ctx-slow" {
			if slowOpens.Add(1) == 1 {
				close(slowStarted)
			}
			<-release
		}
		return okConv(), nil
	})

	var wg sync.WaitGroup
	slowErrs := make([]*a2a.JSONRPCError, 2)
	for i := range slowErrs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slowErrs[i] = sendAs(t, ts, "alice", "ctx-slow").Error
		}()
	}
	<-slowStarted

	fast := make(chan *a2a.JSONRPCError, 1)
	go func() { fast <- sendAs(t, ts, "bob", "ctx-fast").Error }()
	select {
	case err := <-fast:
		assert.Nil(t, err)
	case <-time.After(5 * time.Second):
		releaseAll()
		t.Fatal("ctx-fast waited on ctx-slow's open")
	}

	releaseAll()
	wg.Wait()
	assert.Equal(t, []*a2a.JSONRPCError{nil, nil}, slowErrs)
	assert.Equal(t, int32(1), slowOpens.Load(), "concurrent messages open a context once")
}

// NewServerFor needs a store that records task owners, as WithTaskOwner does.
func TestNewServerFor_RequiresAnOwnedStore(t *testing.T) {
	assert.PanicsWithError(t,
		"a2a: WithTaskOwner needs a TaskStore that implements OwnedTaskStore (got a2aserver.storeWithoutOwners)",
		func() {
			NewServerFor(ownerFromHeader, func(*http.Request, string) (Conversation, error) { return okConv(), nil },
				WithTaskStore(storeWithoutOwners{NewInMemoryTaskStore()}))
		})
}

// A WithTaskOwner passed to NewServerFor scopes tasks in place of its caller
// identity: here every caller shares one task owner and sees the task.
func TestNewServerFor_WithTaskOwnerOverridesTaskScope(t *testing.T) {
	srv := NewServerFor(ownerFromHeader, func(*http.Request, string) (Conversation, error) { return okConv(), nil },
		WithTaskOwner(func(*http.Request) string { return "team" }))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		ts.Close()
		_ = srv.Shutdown(context.Background())
	})

	sent := sendAs(t, ts, "alice", "ctx-alice")
	require.Nil(t, sent.Error)
	task := decodeTaskResult(t, sent.Result)

	got := as(t, ts, "bob", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID})
	require.Nil(t, got.Error)
	assert.Equal(t, task.ID, decodeTaskResult(t, got.Result).ID)
}
