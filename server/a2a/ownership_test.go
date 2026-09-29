package a2aserver

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerHeader carries the test caller's identity; a real host reads it from
// its authentication middleware.
const callerHeader = "X-Test-Caller"

func ownerFromHeader(r *http.Request) string { return r.Header.Get(callerHeader) }

// as sends a JSON-RPC request as caller and returns the decoded response.
func as(t *testing.T, ts *httptest.Server, caller, method string, params any) *a2a.JSONRPCResponse {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	require.NoError(t, err)
	body, err := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: paramsJSON})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/a2a", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set(a2a.HeaderVersion, "1.0")
	if caller != "" {
		req.Header.Set(callerHeader, caller)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return &out
}

func requireCode(t *testing.T, resp *a2a.JSONRPCResponse, code int, what string) {
	t.Helper()
	require.NotNil(t, resp.Error, "%s: expected error %d, got result %s", what, code, resp.Result)
	assert.Equal(t, code, resp.Error.Code, what)
}

func TestOwnership_CallersSeeOnlyTheirOwnTasks(t *testing.T) {
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("secret")}, text: "secret"}, nil
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil }, WithTaskOwner(ownerFromHeader))
	defer ts.Close()
	defer close(release)

	sent := as(t, ts, "alice", a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-alice"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	})
	require.Nil(t, sent.Error)
	task := decodeTaskResult(t, sent.Result)

	// Bob cannot read, cancel or subscribe to it: to him it does not exist.
	requireCode(t, as(t, ts, "bob", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID}),
		a2a.ErrCodeTaskNotFound, "get")
	requireCode(t, as(t, ts, "bob", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID}),
		a2a.ErrCodeTaskNotFound, "cancel")
	requireCode(t, as(t, ts, "bob", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: task.ID}),
		a2a.ErrCodeTaskNotFound, "subscribe")
	stored, err := srv.taskStore.Get(task.ID)
	require.NoError(t, err)
	assert.NotEqual(t, a2a.TaskStateCanceled, stored.Status.State, "bob's cancel must not have reached the task")

	// Nor list it, with or without its context.
	for _, req := range []a2a.ListTasksRequest{{}, {ContextID: "ctx-alice"}} {
		var list a2a.ListTasksResponse
		resp := as(t, ts, "bob", a2a.MethodV1ListTasks, req)
		require.Nil(t, resp.Error)
		require.NoError(t, json.Unmarshal(resp.Result, &list))
		assert.Empty(t, list.Tasks, "bob listed alice's tasks with %+v", req)
		assert.Zero(t, list.TotalSize)
	}

	// Nor send into her context, which would hand him her conversation.
	requireCode(t, as(t, ts, "bob", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-alice")}),
		a2a.ErrCodeInvalidParams, "send into another caller's context")
	requireCode(t, as(t, ts, "bob", a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{Message: userMessage("ctx-alice")}),
		a2a.ErrCodeInvalidParams, "stream into another caller's context")

	// Alice sees her task everywhere, including a list without a context.
	got := as(t, ts, "alice", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID})
	require.Nil(t, got.Error)
	var list a2a.ListTasksResponse
	resp := as(t, ts, "alice", a2a.MethodV1ListTasks, a2a.ListTasksRequest{})
	require.Nil(t, resp.Error)
	require.NoError(t, json.Unmarshal(resp.Result, &list))
	require.Len(t, list.Tasks, 1)
	assert.Equal(t, task.ID, list.Tasks[0].ID)

	canceled := as(t, ts, "alice", a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID})
	require.Nil(t, canceled.Error)
}

func TestOwnership_AnonymousCallerIsRefused(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithTaskOwner(ownerFromHeader))
	defer ts.Close()

	paramsJSON, _ := json.Marshal(a2a.GetTaskRequest{ID: "x"})
	body, _ := json.Marshal(a2a.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: a2a.MethodV1GetTask, Params: paramsJSON})
	resp, err := http.Post(ts.URL+"/a2a", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// storeWithoutOwners hides InMemoryTaskStore's owner support.
type storeWithoutOwners struct{ TaskStore }

func TestOwnership_RequiresAnOwnedStore(t *testing.T) {
	assert.PanicsWithError(t,
		"a2a: WithTaskOwner needs a TaskStore that implements OwnedTaskStore (got a2aserver.storeWithoutOwners)",
		func() {
			NewServer(nopOpener, WithTaskOwner(ownerFromHeader),
				WithTaskStore(storeWithoutOwners{NewInMemoryTaskStore()}))
		})
}

// ownedListOnly exposes owners but not Query, so the server filters the List
// fallback by owner itself.
type ownedListOnly struct{ OwnedTaskStore }

func TestQueryTasks_FallbackFiltersByOwner(t *testing.T) {
	inner := NewInMemoryTaskStore()
	for id, owner := range map[string]string{"a1": "alice", "a2": "alice", "b1": "bob"} {
		_, err := inner.CreateOwned(id, "ctx", owner)
		require.NoError(t, err)
	}
	page, err := queryTasks(ownedListOnly{inner}, TaskQuery{Owner: "alice"})
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total)

	page, err = queryTasks(storeWithoutOwners{inner}, TaskQuery{Owner: "alice"})
	require.NoError(t, err)
	assert.Zero(t, page.Total, "a store that cannot name owners yields nothing for an owner query")

	owner, err := inner.Owner("b1")
	require.NoError(t, err)
	assert.Equal(t, "bob", owner)
	_, err = inner.Owner("missing")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

// broadcastCanceler stands in for a shared cancel channel (Redis pub/sub,
// say): every instance that listens hears every cancel.
type broadcastCanceler struct {
	mu        sync.Mutex
	listeners []func(string)
	fail      bool
}

func (b *broadcastCanceler) Cancel(_ context.Context, taskID string) error {
	if b.fail {
		return errors.New("bus down")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range b.listeners {
		l(taskID)
	}
	return nil
}

func (b *broadcastCanceler) Listen(cancelLocal func(string)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, cancelLocal)
	return func() {}
}

// Two instances behind one task store, as replicas behind a load balancer:
// with a shared bus and canceler, a subscribe or cancel that lands on the
// instance not running the task still reaches it.
func TestReplicas_SharedBusAndCanceler(t *testing.T) {
	store := NewInMemoryTaskStore()
	bus := newLocalTaskEvents()
	canceler := &broadcastCanceler{}

	started := make(chan struct{}, 1)
	turnCanceled := make(chan struct{})
	release := make(chan struct{})
	mock := &mockConv{sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
		started <- struct{}{}
		select {
		case <-release:
			return &mockSendResult{parts: []types.ContentPart{types.NewTextPart("done")}, text: "done"}, nil
		case <-ctx.Done():
			close(turnCanceled)
			return nil, ctx.Err()
		}
	}}
	opener := func(string) (Conversation, error) { return mock, nil }
	opts := []Option{WithTaskStore(store), WithTaskEventBus(bus), WithTaskCanceler(canceler)}
	_, replicaA := newTestServer(opener, opts...)
	defer replicaA.Close()
	_, replicaB := newTestServer(opener, opts...)
	defer replicaB.Close()

	send := func() *a2a.Task {
		return decodeTaskResult(t, a2aRPCRequest(t, replicaA, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
			Message:       userMessage(""),
			Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
		}).Result)
	}

	// Subscribe through B to a turn running on A.
	first := send()
	<-started
	got := make(chan []map[string]any)
	go func() {
		_, events := rawStream(t, rawRPC(t, replicaB, "1.0", a2a.MethodV1SubscribeToTask, a2a.SubscribeTaskRequest{ID: first.ID}))
		got <- events
	}()
	require.Eventually(t, func() bool {
		bus.mu.Lock()
		defer bus.mu.Unlock()
		return len(bus.subs[first.ID]) == 1
	}, time.Second, 5*time.Millisecond)
	release <- struct{}{}
	select {
	case events := <-got:
		last := events[len(events)-1]["statusUpdate"].(map[string]any)
		assert.Equal(t, "TASK_STATE_COMPLETED", last["status"].(map[string]any)["state"])
	case <-time.After(3 * time.Second):
		t.Fatal("the subscriber on the other instance never saw the task finish")
	}

	// Cancel through B a turn running on A.
	second := send()
	<-started
	resp := a2aRPCRequest(t, replicaB, a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: second.ID})
	require.Nil(t, resp.Error)
	select {
	case <-turnCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel on one instance did not stop the turn on the other")
	}
}

func TestCancel_CancelerFailureStillCancelsTheTask(t *testing.T) {
	started := make(chan struct{})
	mock := &mockConv{sendFunc: func(ctx context.Context, _ any) (SendResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	srv, ts := newTestServer(func(string) (Conversation, error) { return mock, nil },
		WithTaskCanceler(&broadcastCanceler{fail: true}))
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	task := decodeTaskResult(t, a2aRPCRequest(t, ts, a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-cancel-fail"),
		Configuration: &a2a.SendMessageConfiguration{ReturnImmediately: true},
	}).Result)
	<-started

	resp := a2aRPCRequest(t, ts, a2a.MethodV1CancelTask, a2a.CancelTaskRequest{ID: task.ID})
	require.Nil(t, resp.Error)
	assert.Equal(t, a2a.TaskStateCanceled, decodeTaskResult(t, resp.Result).Status.State)
}

// Two callers racing to open the same context: whichever opens the
// conversation owns it, and the other is refused — the check is made under
// the lock that creates the conversation, not from tasks that may not exist
// yet.
func TestOwnership_ConversationBelongsToWhoeverOpenedIt(t *testing.T) {
	srv := NewServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskOwner(ownerFromHeader))
	defer func() { _ = srv.Shutdown(context.Background()) }()

	alice, bob := &rpcCall{owner: "alice"}, &rpcCall{owner: "bob"}
	_, err := srv.getOrCreateConversation(alice, "shared")
	require.NoError(t, err, "no task exists yet, and alice opens the conversation")
	_, err = srv.getOrCreateConversation(bob, "shared")
	assert.ErrorIs(t, err, errContextTaken)
	_, err = srv.getOrCreateConversation(alice, "shared")
	assert.NoError(t, err)
}

// After alice's conversation is evicted her tasks remain, and they still keep
// bob out of the context.
func TestOwnership_EvictedConversationStaysScoped(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskOwner(ownerFromHeader))
	defer ts.Close()

	sent := as(t, ts, "alice", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-evict")})
	require.Nil(t, sent.Error)

	srv.convsMu.Lock()
	srv.convLastUse["ctx-evict"] = time.Now().Add(-48 * time.Hour)
	srv.convsMu.Unlock()
	srv.convTTL = time.Hour
	srv.evictIdleConversations(time.Now())
	srv.convsMu.RLock()
	_, cached := srv.convs["ctx-evict"]
	srv.convsMu.RUnlock()
	require.False(t, cached)

	requireCode(t, as(t, ts, "bob", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-evict")}),
		a2a.ErrCodeInvalidParams, "send into an evicted conversation's context")
	resp := as(t, ts, "alice", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-evict")})
	assert.Nil(t, resp.Error, "alice may reopen her own context")
}
