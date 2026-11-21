package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A state the client does not know — one a later protocol version adds, say —
// is decoded as TaskStateUnknown instead of failing the whole task (A2A 1.0
// §5.7: implementations SHOULD ignore what they do not recognize, so the
// protocol can evolve).

func TestClient_GetTaskWithAnUnknownStateKeepsTheTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(mustJSON(req.ID)) + `,"result":{"id":"t","contextId":"c",` +
			`"status":{"state":"TASK_STATE_FROM_THE_FUTURE"},"artifacts":[{"artifactId":"a","parts":[{"text":"kept"}]}]}}`))
	}))
	defer srv.Close()

	task, err := NewClient(srv.URL).GetTask(context.Background(), "t")
	require.NoError(t, err)
	assert.Equal(t, TaskStateUnknown, task.Status.State)
	assert.Equal(t, "t", task.ID)
	require.Len(t, task.Artifacts, 1)
	assert.Equal(t, "kept", *task.Artifacts[0].Parts[0].Text)
}

func TestParseStreamEvent_UnknownStateIsNotDropped(t *testing.T) {
	evt, ok := parseStreamEvent(`{"jsonrpc":"2.0","id":1,"result":{"statusUpdate":{"taskId":"t","contextId":"c",` +
		`"status":{"state":"TASK_STATE_PAUSED"}}}}`)
	require.True(t, ok, "the event must not be dropped")
	require.NotNil(t, evt.StatusUpdate)
	assert.Equal(t, "t", evt.StatusUpdate.TaskID)
	assert.Equal(t, TaskStateUnknown, evt.StatusUpdate.Status.State)
}

// ParseTaskState stays strict: it is what a server uses to validate a state a
// caller asks for.
func TestParseTaskState_RefusesAnUnknownSpelling(t *testing.T) {
	_, err := ParseTaskState("TASK_STATE_PAUSED")
	require.Error(t, err)
	assert.Equal(t, `a2a: invalid task state: "TASK_STATE_PAUSED"`, err.Error())
}

// A state that is not a JSON string is still malformed.
func TestTaskState_NonStringIsAnError(t *testing.T) {
	var s TaskState
	assert.Error(t, json.Unmarshal([]byte(`3`), &s))
}

// --- Inline push notification configs (A2A 1.0 SendMessageConfiguration) ---

// A 0.3 pushNotificationConfig decodes into the same field as 1.0's
// taskPushNotificationConfig, its first authentication scheme as the scheme.
func TestSendMessageConfiguration_DecodesEitherPushConfig(t *testing.T) {
	for wire, want := range map[string]TaskPushNotificationConfig{
		`{"taskPushNotificationConfig":{"id":"c","url":"https://h/x","token":"t",` +
			`"authentication":{"scheme":"Bearer","credentials":"k"}}}`: {
			ID: "c", URL: "https://h/x", Token: "t", Authentication: &AuthenticationInfo{Scheme: "Bearer", Credentials: "k"},
		},
		`{"pushNotificationConfig":{"id":"c","url":"https://h/x","token":"t",` +
			`"authentication":{"schemes":["Bearer","Basic"],"credentials":"k"}}}`: {
			ID: "c", URL: "https://h/x", Token: "t", Authentication: &AuthenticationInfo{Scheme: "Bearer", Credentials: "k"},
		},
		`{"pushNotificationConfig":{"url":"https://h/x"}}`: {URL: "https://h/x"},
		`{"pushNotificationConfig":{"url":"https://h/x","authentication":{"schemes":[],"credentials":"k"}}}`: {
			URL: "https://h/x", Authentication: &AuthenticationInfo{Credentials: "k"},
		},
	} {
		var cfg SendMessageConfiguration
		require.NoError(t, json.Unmarshal([]byte(wire), &cfg), wire)
		require.NotNil(t, cfg.TaskPushNotificationConfig, wire)
		assert.Equal(t, want, *cfg.TaskPushNotificationConfig, wire)
	}

	var none SendMessageConfiguration
	require.NoError(t, json.Unmarshal([]byte(`{"historyLength":2,"returnImmediately":true}`), &none))
	assert.Nil(t, none.TaskPushNotificationConfig)
	assert.Equal(t, 2, *none.HistoryLength)
	assert.True(t, none.ReturnImmediately)
}

// The client sends the config in the version it speaks.
func TestWireSendParams_PushConfigPerVersion(t *testing.T) {
	req := &SendMessageRequest{
		Message: Message{MessageID: "m", Role: RoleUser, Parts: []Part{{Text: strPtr("hi")}}},
		Configuration: &SendMessageConfiguration{TaskPushNotificationConfig: &TaskPushNotificationConfig{
			URL: "https://h/x", Token: "t", Authentication: &AuthenticationInfo{Scheme: "Bearer", Credentials: "k"},
		}},
	}
	v1, err := json.Marshal(ProtocolVersion10.WireSendParams(req))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(v1, &got))
	assert.Equal(t, map[string]any{"taskPushNotificationConfig": map[string]any{
		"url": "https://h/x", "token": "t", "authentication": map[string]any{"scheme": "Bearer", "credentials": "k"},
	}}, got["configuration"])

	v03, err := json.Marshal(ProtocolVersion03.WireSendParams(req))
	require.NoError(t, err)
	got = nil
	require.NoError(t, json.Unmarshal(v03, &got))
	assert.Equal(t, map[string]any{"pushNotificationConfig": map[string]any{
		"url": "https://h/x", "token": "t", "authentication": map[string]any{"schemes": []any{"Bearer"}, "credentials": "k"},
	}}, got["configuration"])
}

// --- SubscribeToTask and GetExtendedAgentCard on the wire ---

// recordingAgent records each JSON-RPC request and answers with answer.
type recordingAgent struct {
	mu     sync.Mutex
	method []string
	params []string
	paths  []string
	answer func(w http.ResponseWriter, req JSONRPCRequest)
}

func (a *recordingAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := decodeRPC(r)
	a.mu.Lock()
	a.method = append(a.method, req.Method)
	a.params = append(a.params, string(req.Params))
	a.paths = append(a.paths, r.URL.Path)
	a.mu.Unlock()
	a.answer(w, req)
}

// SubscribeToTask posts the version's method with the task id and delivers
// the stream's events; 0.3 calls it tasks/resubscribe.
func TestClient_SubscribeToTaskOnTheWire(t *testing.T) {
	for v, method := range map[ProtocolVersion]string{
		ProtocolVersion10: MethodV1SubscribeToTask,
		ProtocolVersion03: MethodV03Resubscribe,
	} {
		agent := &recordingAgent{answer: func(w http.ResponseWriter, req JSONRPCRequest) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(sseEvent(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID,
				Result: mustJSON(StreamResponse{Task: &Task{ID: "t", Status: TaskStatus{State: TaskStateWorking}}})})))
			_, _ = w.Write([]byte(sseEvent(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID,
				Result: mustJSON(StreamResponse{StatusUpdate: &TaskStatusUpdateEvent{TaskID: "t",
					Status: TaskStatus{State: TaskStateCompleted}}})})))
		}}
		srv := httptest.NewServer(agent)
		ch, err := NewClient(srv.URL, WithProtocolVersion(v)).SubscribeToTask(context.Background(), "t")
		require.NoError(t, err)
		var states []TaskState
		for evt := range ch {
			switch {
			case evt.Task != nil:
				states = append(states, evt.Task.Status.State)
			case evt.StatusUpdate != nil:
				states = append(states, evt.StatusUpdate.Status.State)
			}
		}
		srv.Close()
		assert.Equal(t, []TaskState{TaskStateWorking, TaskStateCompleted}, states, v)
		assert.Equal(t, []string{method}, agent.method, v)
		assert.JSONEq(t, `{"id":"t"}`, agent.params[0], v)
	}
}

// GetExtendedAgentCard posts the version's method, decodes the card in
// either version's shape, and routes later calls by it.
func TestClient_GetExtendedAgentCardOnTheWire(t *testing.T) {
	var base string
	agent := &recordingAgent{}
	agent.answer = func(w http.ResponseWriter, req JSONRPCRequest) {
		switch req.Method {
		case MethodV1GetExtendedAgentCard:
			rpcResult(w, req.ID, AgentCard{Name: "extended", SupportedInterfaces: []AgentInterface{
				{URL: base + "/rpc", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0"},
			}})
		default:
			rpcResult(w, req.ID, &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}})
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/", agent)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	client := NewClient(srv.URL)
	card, err := client.GetExtendedAgentCard(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "extended", card.Name)
	assert.JSONEq(t, `{}`, agent.params[0])

	_, err = client.GetTask(context.Background(), "t")
	require.NoError(t, err)
	assert.Equal(t, []string{MethodV1GetExtendedAgentCard, MethodV1GetTask}, agent.method)
	assert.Equal(t, []string{"/a2a", "/rpc"}, agent.paths, "later calls go where the extended card says")
}

// A result that is not a card is a decode error, and the cached card is kept.
func TestClient_GetExtendedAgentCardBadResult(t *testing.T) {
	srv := httptest.NewServer(&recordingAgent{answer: func(w http.ResponseWriter, req JSONRPCRequest) {
		rpcResult(w, req.ID, []int{1})
	}})
	defer srv.Close()
	client := NewClient(srv.URL)
	card, err := client.GetExtendedAgentCard(context.Background())
	assert.Nil(t, card)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a2a: GetExtendedAgentCard: decode result:")
	assert.Nil(t, client.cachedCard())
}
