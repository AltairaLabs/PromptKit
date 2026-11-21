package a2aserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// Optional A2A 1.0 features (#2134), checked through the JSON-RPC endpoint.

// --- A state filter is validated (A2A 1.0 §3.3.2) ---

// The runtime decodes a state it does not know as TaskStateUnknown, so that a
// client survives a newer server. A server must still refuse a filter that
// names no state, rather than listing everything.
func TestListTasks_UnknownStatusFilterIsInvalidParams(t *testing.T) {
	_, ts := newTestServer(nopOpener)
	defer ts.Close()

	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		map[string]any{"contextId": "ctx", "status": "TASK_STATE_PAUSED"}))
	assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code)
	assert.Equal(t, `Invalid params: status: a2a: invalid task state: "TASK_STATE_PAUSED"`, e.Message)
}

// --- Tasks left waiting for the caller are evicted ---

// interruptedTask sends one message to a conversation that asks for a client
// tool, leaving its task input-required, and backdates the task's status by
// age. It returns the task's id.
func interruptedTask(t *testing.T, srv *Server, ts *httptest.Server, contextID string, age time.Duration) string {
	t.Helper()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage(contextID)})))
	require.Equal(t, a2a.TaskStateInputRequired, task.Status.State)
	setTaskTimestamp(srv.taskStore.(*InMemoryTaskStore), task.ID, time.Now().Add(-age))
	return task.ID
}

// getTaskCode is GetTask's error code for taskID, or 0 when it is found.
func getTaskCode(t *testing.T, ts *httptest.Server, taskID string) int {
	t.Helper()
	resp := rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: taskID})
	defer resp.Body.Close()
	var out a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	if out.Error == nil {
		return 0
	}
	return out.Error.Code
}

// A task that waits on the caller longer than WithInterruptedTaskTTL is
// evicted: a legacy client that resumes without a taskId starts a new task
// and leaves the old one input-required for good, so without this the store
// grows without bound. A task that has waited less is kept.
func TestInterruptedTaskTTL_EvictsTasksLeftWaiting(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil },
		WithInterruptedTaskTTL(time.Hour), WithConversationTTL(0))
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	stale := interruptedTask(t, srv, ts, "ctx-stale", 2*time.Hour)
	fresh := interruptedTask(t, srv, ts, "ctx-fresh", 10*time.Minute)
	srv.registerCancel(stale, func() {})

	srv.evictOnce()

	assert.Equal(t, a2a.ErrCodeTaskNotFound, getTaskCode(t, ts, stale), "a task left waiting past the TTL is evicted")
	assert.Equal(t, 0, getTaskCode(t, ts, fresh), "a task waiting within the TTL is kept")
	srv.cancelsMu.Lock()
	_, hasCancel := srv.cancels[stale]
	srv.cancelsMu.Unlock()
	assert.False(t, hasCancel, "an evicted task's cancel func is dropped")
}

// Without the option a waiting task is kept for a day, then evicted.
func TestInterruptedTaskTTL_DefaultsToADay(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil })
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	kept := interruptedTask(t, srv, ts, "ctx-kept", 23*time.Hour)
	evicted := interruptedTask(t, srv, ts, "ctx-evicted", 25*time.Hour)
	srv.evictOnce()

	assert.Equal(t, 0, getTaskCode(t, ts, kept))
	assert.Equal(t, a2a.ErrCodeTaskNotFound, getTaskCode(t, ts, evicted))
}

// A TTL of zero keeps waiting tasks however long they wait, and the
// finished-task TTL does not reach them.
func TestInterruptedTaskTTL_ZeroDisables(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil },
		WithInterruptedTaskTTL(0), WithTaskTTL(time.Minute))
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	waiting := interruptedTask(t, srv, ts, "ctx-forever", 1000*time.Hour)
	srv.evictOnce()
	assert.Equal(t, 0, getTaskCode(t, ts, waiting))
}

// Eviction never touches a task that is running or finished within its own
// TTL, only interrupted ones.
func TestInMemoryTaskStore_EvictInterruptedOnlyTakesInterruptedTasks(t *testing.T) {
	store := NewInMemoryTaskStore()
	old := time.Now().Add(-time.Hour)
	for id, path := range map[string][]a2a.TaskState{
		"input":     {a2a.TaskStateWorking, a2a.TaskStateInputRequired},
		"auth":      {a2a.TaskStateWorking, a2a.TaskStateAuthRequired},
		"working":   {a2a.TaskStateWorking},
		"submitted": {},
		"completed": {a2a.TaskStateWorking, a2a.TaskStateCompleted},
	} {
		_, err := store.Create(id, "ctx")
		require.NoError(t, err)
		for _, state := range path {
			require.NoError(t, store.SetState(id, state, nil))
		}
		setTaskTimestamp(store, id, old)
	}

	evicted := store.EvictInterrupted(time.Now().Add(-time.Minute))
	sort.Strings(evicted)
	assert.Equal(t, []string{"auth", "input"}, evicted)
	for _, id := range []string{"working", "submitted", "completed"} {
		_, err := store.Get(id)
		assert.NoError(t, err, id)
	}
	for _, id := range evicted {
		_, err := store.Get(id)
		assert.ErrorIs(t, err, ErrTaskNotFound, id)
	}
}

// --- Streaming can be turned off (A2A 1.0 §3.3.4) ---

// servedCapabilities fetches the card in version v and returns its
// capabilities object.
func servedCapabilities(t *testing.T, ts *httptest.Server, v string) map[string]any {
	t.Helper()
	resp, body := fetchCardRaw(t, ts, a2a.AgentCardPath, map[string]string{a2a.HeaderVersion: v})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var card map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &card))
	return card["capabilities"].(map[string]any)
}

// With WithoutStreaming the card does not declare streaming, in either
// version, and both streaming methods fail with UnsupportedOperationError,
// as §3.3.4 requires of an agent that does not declare it. SendMessage still
// works, and the task it starts can be read.
func TestWithoutStreaming_CardAndMethods(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithCard(&a2a.AgentCard{Name: "a", Capabilities: a2a.AgentCapabilities{Streaming: true}}),
		WithoutStreaming())
	defer ts.Close()

	for _, v := range []string{"1.0", "0.3"} {
		caps := servedCapabilities(t, ts, v)
		assert.NotContains(t, caps, "streaming", "A2A %s card", v)
	}

	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-nostream")})))
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)

	for _, tc := range []struct{ version, method string }{
		{"1.0", a2a.MethodV1SendStreamingMessage},
		{"0.3", a2a.MethodV03SendStreamingMessage},
	} {
		e := rawError(t, rawRPC(t, ts, tc.version, tc.method, a2a.SendMessageRequest{Message: userMessage("ctx-nostream")}))
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, tc.method)
		assert.Equal(t, "Streaming is not supported by this agent", e.Message, tc.method)
	}
	for _, tc := range []struct{ version, method string }{
		{"1.0", a2a.MethodV1SubscribeToTask},
		{"0.3", a2a.MethodV03Resubscribe},
	} {
		e := rawError(t, rawRPC(t, ts, tc.version, tc.method, a2a.SubscribeTaskRequest{ID: task.ID}))
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, tc.method)
		assert.Equal(t, "Streaming is not supported by this agent", e.Message, tc.method)
	}

	page, err := queryTasks(srv.taskStore, TaskQuery{ContextID: "ctx-nostream"})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total, "a refused stream starts no task")
}

// Without the option the card declares streaming, whatever the configured
// card says.
func TestStreaming_DeclaredByDefault(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithCard(&a2a.AgentCard{Name: "a"}))
	defer ts.Close()
	for _, v := range []string{"1.0", "0.3"} {
		assert.Equal(t, true, servedCapabilities(t, ts, v)["streaming"], "A2A %s card", v)
	}
}

// --- Task history (A2A 1.0 Task.history, §3.2.4) ---

// historyOf summarizes a task's history as role:messageId, with the task
// each message names.
func historyOf(task *a2a.Task) []string {
	out := make([]string, len(task.History))
	for i, m := range task.History {
		out[i] = string(m.Role) + ":" + m.MessageID + "@" + m.TaskID
	}
	return out
}

// getTaskWithHistory is GetTask with historyLength n (nil for unset).
func getTaskWithHistory(t *testing.T, ts *httptest.Server, taskID string, n *int) *a2a.Task {
	t.Helper()
	return decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
		a2a.GetTaskRequest{ID: taskID, HistoryLength: n})))
}

func intPtr(n int) *int { return &n }

// A task records the messages of each turn: what the caller sent, and the
// status message the agent ended a turn with (here, its request for a client
// tool). historyLength trims it to the most recent messages.
func TestHistory_RecordsEachTurnsMessages(t *testing.T) {
	for _, method := range []string{a2a.MethodV1SendMessage, a2a.MethodV1SendStreamingMessage} {
		t.Run(method, func(t *testing.T) {
			_, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil })
			defer ts.Close()

			var taskID string
			first := rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: userMessage("ctx-history")})
			second := func(id string) *http.Response {
				return rawRPC(t, ts, "1.0", method, a2a.SendMessageRequest{Message: toolResultMessage("", id)})
			}
			if method == a2a.MethodV1SendMessage {
				taskID = decodeTaskResult(t, rawRPCResult(t, first)).ID
				_ = rawRPCResult(t, second(taskID))
			} else {
				_, events := rawStream(t, first)
				taskID = events[0]["task"].(map[string]any)["id"].(string)
				_, _ = rawStream(t, second(taskID))
			}

			task := getTaskWithHistory(t, ts, taskID, nil)
			require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
			require.Len(t, task.History, 3)
			assert.Equal(t, "user:m1@"+taskID, historyOf(task)[0])
			agent := task.History[1]
			assert.Equal(t, a2a.RoleAgent, agent.Role)
			assert.Equal(t, taskID, agent.TaskID)
			assert.Equal(t, "ctx-history", agent.ContextID)
			assert.Equal(t, "call-1", agent.Parts[0].Metadata["tool_call_id"], "the agent's request for the client tool")
			assert.Equal(t, "user:m2@"+taskID, historyOf(task)[2])
			assert.Equal(t, "ctx-history", task.History[2].ContextID, "the context is filled in from the task")

			assert.Equal(t, []string{"user:m2@" + taskID}, historyOf(getTaskWithHistory(t, ts, taskID, intPtr(1))))
		})
	}
}

// historyLength 0 omits history altogether (§3.2.4: the field SHOULD be
// omitted), and the field is absent too on a task with none recorded.
func TestHistory_ZeroLengthOmitsTheField(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-zero")})))
	require.Len(t, task.History, 1, "the caller's message")

	result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID, HistoryLength: intPtr(0)}))
	assert.NotContains(t, result, "history")
}

// historyLength is honored on SendMessage's answer and on ListTasks.
func TestHistory_LengthOnSendAndList(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil })
	defer ts.Close()
	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-list-history")})))
	assert.Equal(t, []string{"user:m1@" + first.ID, "agent:" + first.History[1].MessageID + "@" + first.ID},
		historyOf(first), "unset historyLength returns it all")

	second := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{
		Message:       toolResultMessage("", first.ID),
		Configuration: &a2a.SendMessageConfiguration{HistoryLength: intPtr(1)},
	})))
	assert.Equal(t, []string{"user:m2@" + first.ID}, historyOf(second))

	var page a2a.ListTasksResponse
	require.NoError(t, json.Unmarshal(rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		a2a.ListTasksRequest{ContextID: "ctx-list-history", HistoryLength: intPtr(2)})), &page))
	require.Len(t, page.Tasks, 1)
	require.Len(t, page.Tasks[0].History, 2)
	assert.Equal(t, a2a.RoleAgent, page.Tasks[0].History[0].Role)
	assert.Equal(t, "m2", page.Tasks[0].History[1].MessageID)
}

// A failed turn records the agent's failure message after the caller's.
func TestHistory_RecordsTheFailure(t *testing.T) {
	failing := &mockConv{sendFunc: func(context.Context, any) (SendResult, error) {
		return nil, errors.New("model unavailable")
	}}
	_, ts := newTestServer(func(string) (Conversation, error) { return failing, nil })
	defer ts.Close()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-fail-history")})))
	require.Equal(t, a2a.TaskStateFailed, task.Status.State)
	require.Len(t, task.History, 2)
	assert.Equal(t, "m1", task.History[0].MessageID)
	assert.Equal(t, a2a.RoleAgent, task.History[1].Role)
	assert.Equal(t, "model unavailable", *task.History[1].Parts[0].Text)
}

// A store that cannot record history serves tasks without it.
func TestHistory_StoreWithoutHistory(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(storeWithoutHistory{NewInMemoryTaskStore()}))
	defer ts.Close()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-no-history")})))
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)
	assert.Empty(t, task.History)
}

// storeWithoutHistory hides InMemoryTaskStore's history support.
type storeWithoutHistory struct{ TaskStore }

// --- tenant (A2A 1.0 request field, AgentInterface.tenant) ---

// errorCode is the JSON-RPC error code of resp, or 0 for a result.
func errorCode(t *testing.T, resp *http.Response) int {
	t.Helper()
	defer resp.Body.Close()
	var out a2a.JSONRPCResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	if out.Error == nil {
		return 0
	}
	return out.Error.Code
}

// listedIDs is the ids of the tasks ListTasks returns for a request in tenant.
func listedIDs(t *testing.T, ts *httptest.Server, tenant, contextID string) []string {
	t.Helper()
	var page a2a.ListTasksResponse
	require.NoError(t, json.Unmarshal(rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1ListTasks,
		a2a.ListTasksRequest{Tenant: tenant, ContextID: contextID})), &page))
	ids := make([]string, len(page.Tasks))
	for i := range page.Tasks {
		ids[i] = page.Tasks[i].ID
	}
	return ids
}

// A task belongs to the tenant whose request created it. A request in another
// tenant, or in none, is told it does not exist (A2A 1.0 §3.3.2: do not reveal
// what the caller may not access), whatever it asks: get, cancel, subscribe,
// continue or list.
func TestTenant_TasksAreScopedToTheirTenant(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil })
	defer ts.Close()

	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-acme")})))
	require.Equal(t, a2a.TaskStateInputRequired, task.Status.State)

	assert.Equal(t, 0, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
		a2a.GetTaskRequest{Tenant: "acme", ID: task.ID})), "its own tenant sees it")
	for _, other := range []string{"globex", ""} {
		assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
			a2a.GetTaskRequest{Tenant: other, ID: task.ID})), "GetTask in tenant %q", other)
		assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1CancelTask,
			a2a.CancelTaskRequest{Tenant: other, ID: task.ID})), "CancelTask in tenant %q", other)
		assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1SubscribeToTask,
			a2a.SubscribeTaskRequest{Tenant: other, ID: task.ID})), "SubscribeToTask in tenant %q", other)
		assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
			a2a.SendMessageRequest{Tenant: other, Message: toolResultMessage("", task.ID)})), "continuing in tenant %q", other)
		assert.Empty(t, listedIDs(t, ts, other, "ctx-acme"), "ListTasks in tenant %q", other)
	}
	assert.Equal(t, []string{task.ID}, listedIDs(t, ts, "acme", "ctx-acme"))

	resumed := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: toolResultMessage("", task.ID)})))
	assert.Equal(t, a2a.TaskStateCompleted, resumed.Status.State, "its own tenant continues it")
}

// A context belongs to the tenant that first used it: a message from another
// tenant naming it is refused, while its conversation is open and after it has
// been evicted.
func TestTenant_ContextBelongsToItsTenant(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithConversationTTL(time.Hour))
	defer ts.Close()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	require.Equal(t, 0, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-shared")})))

	refused := func(when string) {
		e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
			a2a.SendMessageRequest{Tenant: "globex", Message: userMessage("ctx-shared")}))
		assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code, when)
		assert.Equal(t, "Invalid params: contextId is not available to this caller", e.Message, when)
	}
	refused("while the conversation is open")

	srv.convsMu.Lock()
	srv.convLastUse["ctx-shared"] = time.Now().Add(-2 * time.Hour)
	srv.convsMu.Unlock()
	srv.evictOnce()
	refused("after the conversation was evicted")

	assert.Equal(t, 0, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-shared")})), "its own tenant carries on")
}

// A stateless handler is told the tenant of each request.
func TestTenant_ReachesTheStatelessHandler(t *testing.T) {
	h := &resumableHandler{}
	h.events = []StreamEvent{{Kind: EventClientTool, ClientTool: &PendingClientToolInfo{CallID: "call-1"}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-handler-tenant")})))
	_ = rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: toolResultMessage("", task.ID)}))

	_, requests, _ := h.snapshot()
	require.Len(t, requests, 1)
	assert.Equal(t, "acme", requests[0].Tenant)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.toolResults, 1)
	assert.Equal(t, "acme", h.toolResults[0].Tenant)
}

// --- Push notifications are refused, inline configs included (§3.3.4) ---

// The server does not deliver push notifications, so a message that asks for
// them inline is refused with PushNotificationNotSupportedError rather than
// run with the request silently dropped; nothing is started. 1.0 names the
// config taskPushNotificationConfig, 0.3 pushNotificationConfig.
func TestPush_InlineConfigIsRefused(t *testing.T) {
	srv, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	for _, tc := range []struct{ version, method, key string }{
		{"1.0", a2a.MethodV1SendMessage, "taskPushNotificationConfig"},
		{"1.0", a2a.MethodV1SendStreamingMessage, "taskPushNotificationConfig"},
		{"0.3", a2a.MethodV03SendMessage, "pushNotificationConfig"},
		{"0.3", a2a.MethodV03SendStreamingMessage, "pushNotificationConfig"},
	} {
		params := map[string]any{
			"message": map[string]any{"messageId": "m1", "contextId": "ctx-push", "role": "ROLE_USER",
				"parts": []any{map[string]any{"text": "hi"}}},
			"configuration": map[string]any{tc.key: map[string]any{"url": "https://hooks.example/a2a", "token": "t"}},
		}
		e := rawError(t, rawRPC(t, ts, tc.version, tc.method, params))
		assert.Equal(t, a2a.ErrCodePushNotificationNotSupported, e.Code, tc.method)
		assert.Equal(t, "Push notifications are not supported", e.Message, tc.method)
	}

	page, err := queryTasks(srv.taskStore, TaskQuery{ContextID: "ctx-push"})
	require.NoError(t, err)
	assert.Equal(t, 0, page.Total, "a refused message starts no task")
}

// The card says so, and every push config method is refused.
func TestPush_MethodsAreRefusedAndTheCardSaysSo(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithCard(&a2a.AgentCard{
		Name: "a", Capabilities: a2a.AgentCapabilities{PushNotifications: true},
	}))
	defer ts.Close()
	assert.NotContains(t, servedCapabilities(t, ts, "1.0"), "pushNotifications")
	for _, method := range []string{
		a2a.MethodV1CreateTaskPushNotificationConfig, a2a.MethodV1GetTaskPushNotificationConfig,
		a2a.MethodV1ListTaskPushNotificationConfigs, a2a.MethodV1DeleteTaskPushNotificationConfig,
	} {
		e := rawError(t, rawRPC(t, ts, "1.0", method, map[string]any{"taskId": "t", "id": "c"}))
		assert.Equal(t, a2a.ErrCodePushNotificationNotSupported, e.Code, method)
	}
}

// tenantListOnly is a TenantTaskStore that cannot query, so the server
// filters ListTasks by tenant itself.
type tenantListOnly struct{ TenantTaskStore }

// Without TaskQuerier the server filters by tenant in memory, to the same
// effect.
func TestTenant_ListWithoutAQuerier(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(tenantListOnly{NewInMemoryTaskStore()}))
	defer ts.Close()

	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-fallback")})))
	assert.Equal(t, []string{task.ID}, listedIDs(t, ts, "acme", "ctx-fallback"))
	assert.Empty(t, listedIDs(t, ts, "globex", "ctx-fallback"))
	assert.Empty(t, listedIDs(t, ts, "", "ctx-fallback"))
}

// A 0.3 request has no tenant: one sent anyway is ignored, so a 0.3 caller
// works with the tasks created in no tenant.
func TestTenant_V03RequestsHaveNone(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()

	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "0.3", a2a.MethodV03SendMessage, map[string]any{
		"tenant":  "acme",
		"message": map[string]any{"kind": "message", "messageId": "m1", "role": "user", "parts": []any{map[string]any{"kind": "text", "text": "hi"}}},
	})))
	assert.Equal(t, 0, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: task.ID})),
		"the task is in no tenant")
	assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
		a2a.GetTaskRequest{Tenant: "acme", ID: task.ID})))
}

// failingExtendedCard cannot produce its card.
type failingExtendedCard struct{}

func (failingExtendedCard) AgentCard(*http.Request) (*a2a.AgentCard, error) {
	return nil, errors.New("card store down")
}

// An extended card provider that fails is an internal error, its cause kept
// from the caller.
func TestExtendedCard_ProviderFailureIsAnInternalError(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithExtendedCardProvider(failingExtendedCard{}),
		WithAuthenticator(&mockAuthenticator{}))
	defer ts.Close()
	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
	assert.Equal(t, a2a.ErrCodeInternal, e.Code)
	assert.Equal(t, "Internal error", e.Message)
}

// historyFailingStore cannot record history.
type historyFailingStore struct{ *InMemoryTaskStore }

func (historyFailingStore) AppendHistory(string, int, ...a2a.Message) error {
	return errors.New("history full")
}

// A history the store cannot record is a warning naming the task; the turn
// goes ahead.
func TestHistory_StoreFailureIsAWarning(t *testing.T) {
	records := &logRecords{}
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(historyFailingStore{NewInMemoryTaskStore()}), WithLogger(slog.New(records)))
	defer ts.Close()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-history-fails")})))
	assert.Equal(t, a2a.TaskStateCompleted, task.Status.State)

	level, attrs, ok := records.find("a2a: failed to record task history")
	require.True(t, ok)
	assert.Equal(t, slog.LevelWarn, level)
	assert.Equal(t, task.ID, attrs["task_id"])
	assert.Equal(t, "history full", attrs["error"])
}

// Restoring a waiting task after its results could not be submitted is not a
// failure, and is not logged as one.
func TestContinuation_RestoreIsNotAWarning(t *testing.T) {
	records := &logRecords{}
	_, ts := newTestServer(func(string) (Conversation, error) { return failingResumeConv{clientToolConv()}, nil },
		WithLogger(slog.New(records)))
	defer ts.Close()
	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-restore-log")})))
	e := rawError(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)}))
	require.Equal(t, a2a.ErrCodeInternal, e.Code)

	_, _, logged := records.find("a2a: failed to set task state")
	assert.False(t, logged)
	assert.Equal(t, 0, getTaskCode(t, ts, first.ID))
}

// --- Review follow-ups ---

// The extended card is for authenticated callers only (§3.1.11, §13.3). A
// server with no way to authenticate refuses it as not configured, rather
// than hand it to anyone.
func TestExtendedCard_RefusedWithoutAuthentication(t *testing.T) {
	provider := &StaticCard{Card: a2a.AgentCard{Name: "extended"}}
	_, open := newTestServer(nopOpener, WithExtendedCardProvider(provider))
	defer open.Close()
	e := rawError(t, rawRPC(t, open, "1.0", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
	assert.Equal(t, a2a.ErrCodeExtendedAgentCardNotConfigured, e.Code)
	assert.Equal(t, "The extended agent card requires an authenticated caller; "+
		"this agent has no authenticator configured", e.Message)

	for name, opt := range map[string]Option{
		"WithAuthenticator": WithAuthenticator(&mockAuthenticator{}),
		"WithTaskOwner":     WithTaskOwner(func(*http.Request) string { return "alice" }),
	} {
		_, ts := newTestServer(nopOpener, WithExtendedCardProvider(provider), opt)
		result := rawResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetExtendedAgentCard, map[string]any{}))
		assert.Equal(t, "extended", result["name"], name)
		ts.Close()
	}
}

// seededStore serves one task, "outside", that its embedded in-memory store
// does not hold, as a read-through wrapper over another store would.
type seededStore struct{ *InMemoryTaskStore }

func (s seededStore) Get(taskID string) (*a2a.Task, error) {
	if taskID == "outside" {
		return &a2a.Task{ID: "outside", ContextID: "c", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}}, nil
	}
	return s.InMemoryTaskStore.Get(taskID)
}

// A task the store records no tenant for is visible to a request that names
// none: a wrapper serving tasks its embedded store does not hold keeps
// working. A request in a tenant still does not see it.
func TestTenant_TaskWithNoRecordIsInNoTenant(t *testing.T) {
	_, ts := newTestServer(nopOpener, WithTaskStore(seededStore{NewInMemoryTaskStore()}))
	defer ts.Close()
	assert.Equal(t, 0, getTaskCode(t, ts, "outside"))
	assert.Equal(t, a2a.ErrCodeTaskNotFound, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1GetTask,
		a2a.GetTaskRequest{Tenant: "acme", ID: "outside"})))
}

// plainStore is a TaskStore with none of the optional interfaces.
type plainStore struct{ TaskStore }

// A store that cannot record tenants fails closed: a request that names a
// tenant is refused, rather than served across tenants. One that names none
// works as before.
func TestTenant_StoreWithoutTenantsRefusesTenantRequests(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(plainStore{NewInMemoryTaskStore()}))
	defer ts.Close()

	for _, tc := range []struct {
		method string
		params any
	}{
		{a2a.MethodV1SendMessage, a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-plain")}},
		{a2a.MethodV1GetTask, a2a.GetTaskRequest{Tenant: "acme", ID: "x"}},
		{a2a.MethodV1ListTasks, a2a.ListTasksRequest{Tenant: "acme", ContextID: "ctx-plain"}},
	} {
		e := rawError(t, rawRPC(t, ts, "1.0", tc.method, tc.params))
		assert.Equal(t, a2a.ErrCodeUnsupportedOperation, e.Code, tc.method)
		assert.Equal(t, "This agent does not support tenants", e.Message, tc.method)
	}
	assert.Equal(t, 0, errorCode(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-plain")})))
}

// Tenant and owner combine: in the same tenant, another caller's task is
// still not found.
func TestTenant_CombinesWithTaskOwner(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskOwner(ownerFromHeader))
	defer ts.Close()
	send := as(t, ts, "alice", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-owned-tenant")})
	require.Nil(t, send.Error)
	task := decodeTaskResult(t, send.Result)

	requireCode(t, as(t, ts, "bob", a2a.MethodV1GetTask, a2a.GetTaskRequest{Tenant: "acme", ID: task.ID}),
		a2a.ErrCodeTaskNotFound, "another caller in the same tenant")
	requireCode(t, as(t, ts, "alice", a2a.MethodV1GetTask, a2a.GetTaskRequest{Tenant: "globex", ID: task.ID}),
		a2a.ErrCodeTaskNotFound, "the same caller in another tenant")
	assert.Nil(t, as(t, ts, "alice", a2a.MethodV1GetTask, a2a.GetTaskRequest{Tenant: "acme", ID: task.ID}).Error)
}

// In stateless mode the handler owns contexts: two tenants may use the same
// contextId, the handler is told which tenant each request is in (so it keys
// its sessions by both), and each tenant sees only its own tasks.
func TestTenant_StatelessContextsAreTheHandlers(t *testing.T) {
	h := &recordingHandler{events: []StreamEvent{{Kind: EventText, Text: "ok"}}}
	_, ts := statelessServer(t, h)
	defer ts.Close()

	ids := map[string]string{}
	for _, tenant := range []string{"acme", "globex"} {
		task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
			a2a.SendMessageRequest{Tenant: tenant, Message: userMessage("ctx-same")})))
		ids[tenant] = task.ID
	}
	_, requests, _ := h.snapshot()
	require.Len(t, requests, 2)
	assert.Equal(t, []string{"acme", "globex"}, []string{requests[0].Tenant, requests[1].Tenant})
	assert.Equal(t, requests[0].ContextID, requests[1].ContextID)
	assert.Equal(t, []string{ids["acme"]}, listedIDs(t, ts, "acme", "ctx-same"))
	assert.Equal(t, []string{ids["globex"]}, listedIDs(t, ts, "globex", "ctx-same"))
}

// tenantBlindQuerier answers queries ignoring TaskQuery.Tenant, as a custom
// store might.
type tenantBlindQuerier struct{ *InMemoryTaskStore }

func (q tenantBlindQuerier) Query(tq TaskQuery) (TaskPage, error) {
	all, err := q.List(tq.ContextID, 0, 0)
	return TaskPage{Tasks: all, Total: len(all)}, err
}

// The server drops what a querier returns from another tenant, so a store that
// ignores TaskQuery.Tenant still does not leak.
func TestTenant_QuerierIgnoringTenantDoesNotLeak(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil },
		WithTaskStore(tenantBlindQuerier{NewInMemoryTaskStore()}))
	defer ts.Close()
	task := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Tenant: "acme", Message: userMessage("ctx-blind")})))
	assert.Equal(t, []string{task.ID}, listedIDs(t, ts, "acme", "ctx-blind"))
	assert.Empty(t, listedIDs(t, ts, "globex", "ctx-blind"))
	assert.Empty(t, listedIDs(t, ts, "", "ctx-blind"))
}

// The eviction loop runs by default, for waiting tasks, even with the other
// TTLs off, and Shutdown stops it.
func TestEvictionLoop_StopsOnShutdown(t *testing.T) {
	srv := NewServer(nopOpener, WithTaskTTL(0), WithConversationTTL(0))
	require.NotNil(t, srv.evictionDone, "the default interrupted-task TTL runs the loop")
	select {
	case <-srv.evictionDone:
		t.Fatal("the loop ended before Shutdown")
	default:
	}
	require.NoError(t, srv.Shutdown(context.Background()))
	select {
	case <-srv.evictionDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not stop the eviction loop")
	}

	off := NewServer(nopOpener, WithTaskTTL(0), WithConversationTTL(0), WithInterruptedTaskTTL(0))
	defer func() { _ = off.Shutdown(context.Background()) }()
	assert.Nil(t, off.evictionDone, "with every TTL off no loop runs")
}

// History is capped: WithMaxTaskHistory keeps the most recent messages.
func TestHistory_IsCapped(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil },
		WithMaxTaskHistory(2))
	defer ts.Close()
	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-capped")})))
	_ = rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: toolResultMessage("", first.ID)}))

	task := getTaskWithHistory(t, ts, first.ID, nil)
	require.Len(t, task.History, 2, "m1 is dropped, the agent's request and m2 kept")
	assert.Equal(t, a2a.RoleAgent, task.History[0].Role)
	assert.Equal(t, "m2", task.History[1].MessageID)
}

// Without the option a task keeps its last 100 messages.
func TestInMemoryTaskStore_AppendHistoryLimit(t *testing.T) {
	store := NewInMemoryTaskStore()
	_, err := store.Create("t", "c")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, store.AppendHistory("t", 3, a2a.Message{MessageID: strconv.Itoa(i)}))
	}
	task, err := store.Get("t")
	require.NoError(t, err)
	assert.Equal(t, []string{"2", "3", "4"}, []string{task.History[0].MessageID, task.History[1].MessageID, task.History[2].MessageID})
	assert.Len(t, task.History, 3)
	assert.Equal(t, 100, NewServer(nopOpener).maxHistory, "the default cap")
	assert.ErrorIs(t, store.AppendHistory("missing", 3, a2a.Message{}), ErrTaskNotFound)

	// A limit of zero keeps everything.
	require.NoError(t, store.AppendHistory("t", 0, a2a.Message{MessageID: "5"}))
	task, err = store.Get("t")
	require.NoError(t, err)
	assert.Len(t, task.History, 4)
}

// A negative historyLength is invalid, wherever it is sent.
func TestHistory_NegativeLengthIsInvalidParams(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return completingMock(), nil })
	defer ts.Close()
	cfg := &a2a.SendMessageConfiguration{HistoryLength: intPtr(-1)}
	for _, tc := range []struct {
		method string
		params any
	}{
		{a2a.MethodV1GetTask, a2a.GetTaskRequest{ID: "x", HistoryLength: intPtr(-1)}},
		{a2a.MethodV1ListTasks, a2a.ListTasksRequest{ContextID: "c", HistoryLength: intPtr(-1)}},
		{a2a.MethodV1SendMessage, a2a.SendMessageRequest{Message: userMessage("ctx-neg"), Configuration: cfg}},
		{a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{Message: userMessage("ctx-neg"), Configuration: cfg}},
	} {
		e := rawError(t, rawRPC(t, ts, "1.0", tc.method, tc.params))
		assert.Equal(t, a2a.ErrCodeInvalidParams, e.Code, tc.method)
		assert.Equal(t, "Invalid params: historyLength must not be negative", e.Message, tc.method)
	}
}

// The task a stream opens with is trimmed to the configuration's
// historyLength, as SendMessage's answer is.
func TestHistory_StreamOpensWithTheTrimmedTask(t *testing.T) {
	_, ts := newTestServer(func(string) (Conversation, error) { return clientToolConv(), nil })
	defer ts.Close()
	first := decodeTaskResult(t, rawRPCResult(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendMessage,
		a2a.SendMessageRequest{Message: userMessage("ctx-stream-trim")})))

	_, events := rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{
		Message:       toolResultMessage("", first.ID),
		Configuration: &a2a.SendMessageConfiguration{HistoryLength: intPtr(1)},
	}))
	opening := events[0]["task"].(map[string]any)
	history := opening["history"].([]any)
	require.Len(t, history, 1)
	assert.Equal(t, "m2", history[0].(map[string]any)["messageId"])

	_, events = rawStream(t, rawRPC(t, ts, "1.0", a2a.MethodV1SendStreamingMessage, a2a.SendMessageRequest{
		Message:       userMessage("ctx-stream-zero"),
		Configuration: &a2a.SendMessageConfiguration{HistoryLength: intPtr(0)},
	}))
	assert.NotContains(t, events[0]["task"].(map[string]any), "history")
}
