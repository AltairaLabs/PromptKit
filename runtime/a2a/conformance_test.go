package a2a

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// Conformance gaps from #2101: each test pins one behavior the A2A 1.0 / 0.3
// specs require and the runtime did not have.

// --- Data parts carry any JSON value (1.0: google.protobuf.Value) ---

func TestPart_DataAcceptsAnyJSONValue(t *testing.T) {
	for _, tc := range []struct {
		wire string
		want any
	}{
		{`{"data":[1,2]}`, []any{1.0, 2.0}},
		{`{"data":"plain"}`, "plain"},
		{`{"data":42}`, 42.0},
		{`{"data":true}`, true},
		{`{"kind":"data","data":["v03"]}`, []any{"v03"}},
	} {
		var p Part
		require.NoError(t, json.Unmarshal([]byte(tc.wire), &p), tc.wire)
		assert.Nil(t, p.Data, tc.wire)
		assert.Equal(t, tc.want, p.DataValue, tc.wire)

		out, err := json.Marshal(p)
		require.NoError(t, err)
		var round map[string]any
		require.NoError(t, json.Unmarshal(out, &round))
		assert.Equal(t, tc.want, round["data"], "re-encoding keeps the value: %s", out)
	}

	// An object still lands in Data, as it always has.
	var obj Part
	require.NoError(t, json.Unmarshal([]byte(`{"data":{"k":"v"}}`), &obj))
	assert.Equal(t, map[string]any{"k": "v"}, obj.Data)
	assert.Nil(t, obj.DataValue)
}

func TestPart_EmptyDataObjectIsKept(t *testing.T) {
	out, err := json.Marshal(Part{Data: map[string]any{}})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":{}}`, string(out), "an empty object is still a data part")
}

func TestPart_V03DataValue(t *testing.T) {
	v := toV03Part(&Part{DataValue: []any{1}})
	assert.Equal(t, kindData, v.Kind)
	assert.Equal(t, []any{1}, v.Data)
}

func TestClient_GetTaskWithNonObjectDataPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(mustJSON(req.ID)) + `,"result":{"id":"t","contextId":"c",` +
			`"status":{"state":"TASK_STATE_COMPLETED"},"artifacts":[{"artifactId":"a","parts":[{"data":[1]}]}]}}`))
	}))
	defer srv.Close()

	task, err := NewClient(srv.URL).GetTask(context.Background(), "t")
	require.NoError(t, err)
	assert.Equal(t, []any{1.0}, task.Artifacts[0].Parts[0].DataValue)
}

func TestParseStreamEvent_NonObjectDataPartIsNotDropped(t *testing.T) {
	evt, ok := parseStreamEvent(`{"jsonrpc":"2.0","id":1,"result":{"artifactUpdate":{"taskId":"t","contextId":"c",` +
		`"artifact":{"artifactId":"a","parts":[{"data":"x"}]}}}}`)
	require.True(t, ok, "the event must not be dropped")
	require.NotNil(t, evt.ArtifactUpdate)
	assert.Equal(t, "x", evt.ArtifactUpdate.Artifact.Parts[0].DataValue)
}

// --- The client talks to the interface the card declares (1.0 §8.3.2) ---

// cardAgent serves card at the well-known path and records the JSON-RPC
// requests posted anywhere else, answering each with a completed task.
type cardAgent struct {
	card    func(base string) AgentCard
	mu      sync.Mutex
	paths   []string
	params  []map[string]any
	headers []http.Header
}

func (a *cardAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.mu.Lock()
		a.headers = append(a.headers, r.Header.Clone())
		a.mu.Unlock()
		if r.URL.Path != AgentCardPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(a.card("http://" + r.Host))
		return
	}
	req := decodeRPC(r)
	var params map[string]any
	_ = json.Unmarshal(req.Params, &params)
	a.mu.Lock()
	a.paths = append(a.paths, r.URL.Path)
	a.params = append(a.params, params)
	a.mu.Unlock()
	rpcResult(w, req.ID, SendMessageResponse{Task: &Task{ID: "t", Status: TaskStatus{State: TaskStateCompleted}}})
}

func sendHi(t *testing.T, c *Client) error {
	t.Helper()
	_, err := c.SendMessage(context.Background(), &SendMessageRequest{
		Message: Message{MessageID: "m", Role: RoleUser, Parts: []Part{{Text: strPtr("hi")}}},
	})
	return err
}

func TestClient_UsesTheCardInterfaceURLAndTenant(t *testing.T) {
	agent := &cardAgent{card: func(base string) AgentCard {
		return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
			{URL: base + "/grpc", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"},
			{URL: base + "/", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0", Tenant: "acme"},
		}}
	}}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	require.NoError(t, sendHi(t, c))
	_, err = c.GetTask(context.Background(), "t")
	require.NoError(t, err)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	assert.Equal(t, []string{"/", "/"}, agent.paths, "calls go to the JSON-RPC interface's URL, not {base}/a2a")
	for _, p := range agent.params {
		assert.Equal(t, "acme", p["tenant"], "every request carries the interface's tenant")
	}
}

// The interface's tenant fills in only a tenant the caller left empty; a
// caller-set tenant is never replaced or dropped.
func TestClient_CallerSetTenantIsKept(t *testing.T) {
	for _, tc := range []struct {
		name, ifaceTenant, callerTenant, want string
	}{
		{"interface has none", "", "caller-set", "caller-set"},
		{"interface has another", "acme", "caller-set", "caller-set"},
		{"caller left it empty", "acme", "", "acme"},
		{"neither has one", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := &cardAgent{card: func(base string) AgentCard {
				return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
					{URL: base + "/rpc", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0", Tenant: tc.ifaceTenant},
				}}
			}}
			srv := httptest.NewServer(agent)
			defer srv.Close()

			c := NewClient(srv.URL)
			_, err := c.Discover(context.Background())
			require.NoError(t, err)
			_, err = c.SendMessage(context.Background(), &SendMessageRequest{
				Tenant:  tc.callerTenant,
				Message: Message{MessageID: "m", Role: RoleUser, Parts: []Part{{Text: strPtr("hi")}}},
			})
			require.NoError(t, err)
			_, err = c.GetTask(context.Background(), "t") // carries no tenant of its own
			require.NoError(t, err)

			agent.mu.Lock()
			defer agent.mu.Unlock()
			require.Len(t, agent.params, 2)
			if tc.want == "" {
				assert.NotContains(t, agent.params[0], "tenant")
			} else {
				assert.Equal(t, tc.want, agent.params[0]["tenant"])
			}
			if tc.ifaceTenant == "" {
				assert.NotContains(t, agent.params[1], "tenant")
			} else {
				assert.Equal(t, tc.ifaceTenant, agent.params[1]["tenant"])
			}
		})
	}
}

func TestClient_PicksTheInterfaceForTheVersionItSpeaks(t *testing.T) {
	agent := &cardAgent{card: func(base string) AgentCard {
		return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
			{URL: base + "/v1", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0", Tenant: "t1"},
			{URL: base + "/v03", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "0.3", Tenant: "t03"},
		}}
	}}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion03))
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	require.NoError(t, sendHi(t, c))

	agent.mu.Lock()
	defer agent.mu.Unlock()
	assert.Equal(t, []string{"/v03"}, agent.paths)
	assert.NotContains(t, agent.params[0], "tenant", "0.3 has no tenant field")
}

func TestClient_RefusesACardWithNoJSONRPCInterface(t *testing.T) {
	agent := &cardAgent{card: func(base string) AgentCard {
		return AgentCard{Name: "a", SupportedInterfaces: []AgentInterface{
			{URL: base + "/grpc", ProtocolBinding: "GRPC", ProtocolVersion: "1.0"},
		}}
	}}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	err = sendHi(t, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON-RPC")
	agent.mu.Lock()
	defer agent.mu.Unlock()
	assert.Empty(t, agent.paths, "nothing is posted to a guessed endpoint")
}

func TestClient_DiscoverSendsTheVersionHeader(t *testing.T) {
	agent := &cardAgent{card: func(string) AgentCard { return AgentCard{Name: "a"} }}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	_, err := NewClient(srv.URL).Discover(context.Background())
	require.NoError(t, err)
	agent.mu.Lock()
	defer agent.mu.Unlock()
	require.NotEmpty(t, agent.headers)
	assert.Equal(t, "1.0", agent.headers[0].Get(HeaderVersion))
}

// --- JSON-RPC errors inside an SSE stream reach the caller (1.0 §9.5) ---

func TestSendMessageStream_DeliversAnInStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		w.Header().Set("Content-Type", "text/event-stream")
		task := &Task{ID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateWorking}}
		_, _ = w.Write([]byte(sseEvent(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: mustJSON(StreamResponse{Task: task})})))
		_, _ = w.Write([]byte(sseEvent(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{
			Code: ErrCodeInternal, Message: "agent crashed", Data: []any{map[string]any{"@type": "x"}},
		}})))
	}))
	defer srv.Close()

	ch, err := NewClient(srv.URL).SendMessageStream(context.Background(), &SendMessageRequest{
		Message: Message{MessageID: "m", Role: RoleUser, Parts: []Part{{Text: strPtr("hi")}}},
	})
	require.NoError(t, err)
	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	require.Len(t, events, 2)
	require.NotNil(t, events[0].Task)
	require.Error(t, events[1].Error, "the error must not be swallowed")
	var rpcErr *RPCError
	require.ErrorAs(t, events[1].Error, &rpcErr)
	assert.Equal(t, ErrCodeInternal, rpcErr.Code)
	assert.Equal(t, "agent crashed", rpcErr.Message)
	assert.Equal(t, []any{map[string]any{"@type": "x"}}, rpcErr.Data)
}

func TestReadSSE_StopsAfterAnError(t *testing.T) {
	body := `data: {"jsonrpc":"2.0","id":1,"error":{"code":-32001,"message":"Task not found"}}` + "\n\n" +
		`data: {"jsonrpc":"2.0","id":1,"result":{"task":{"id":"t","contextId":"c","status":{"state":"TASK_STATE_WORKING"}}}}` + "\n\n"
	ch := make(chan StreamEvent, 4)
	ReadSSE(context.Background(), bufio.NewReader(strings.NewReader(body)), ch)
	close(ch)
	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	require.Len(t, events, 1, "an error ends the stream")
	var rpcErr *RPCError
	require.ErrorAs(t, events[0].Error, &rpcErr)
	assert.Equal(t, ErrCodeTaskNotFound, rpcErr.Code)
}

func TestRPCCall_ErrorCarriesData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{
			Code: ErrCodeTaskNotFound, Message: "Task not found", Data: []any{"detail"},
		}})
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).GetTask(context.Background(), "missing")
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, []any{"detail"}, rpcErr.Data)
}

// --- The executor sends media once-encoded and reports unfinished tasks ---

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func TestBuildRequest_DecodesBase64Media(t *testing.T) {
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: "http://x"}}
	wav := append([]byte("RIFF\x24\x00\x00\x00WAVEfmt "), make([]byte, 16)...)
	args, err := json.Marshal(map[string]string{
		"query":      "look",
		"image_data": base64.StdEncoding.EncodeToString(pngHeader),
		"audio_data": "data:audio/ogg;base64," + base64.StdEncoding.EncodeToString(wav),
	})
	require.NoError(t, err)

	_, req, err := buildRequest(desc, args)
	require.NoError(t, err)
	parts := req.Message.Parts
	require.Len(t, parts, 3)
	assert.Equal(t, pngHeader, parts[1].Raw, "raw holds the bytes, not their base64 text")
	assert.Equal(t, "image/png", parts[1].MediaType)
	assert.Equal(t, wav, parts[2].Raw)
	assert.Equal(t, "audio/ogg", parts[2].MediaType, "a data URL's declared type wins")

	// On the wire the bytes are base64-encoded exactly once.
	wire, err := json.Marshal(parts[1])
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(wire, &decoded))
	assert.Equal(t, base64.StdEncoding.EncodeToString(pngHeader), decoded["raw"])
}

func TestBuildRequest_RejectsMediaThatIsNotBase64(t *testing.T) {
	desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: "http://x"}}
	_, _, err := buildRequest(desc, json.RawMessage(`{"query":"q","image_data":"not base64!"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_data")
}

// stateAgent answers SendMessage with a task in state, and records CancelTask.
func stateAgent(t *testing.T, state TaskState, text string) (srv *httptest.Server, cancels func() int) {
	t.Helper()
	var mu sync.Mutex
	var canceled []string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		if req.Method == MethodV1CancelTask {
			var p CancelTaskRequest
			_ = json.Unmarshal(req.Params, &p)
			mu.Lock()
			canceled = append(canceled, p.ID)
			mu.Unlock()
			rpcResult(w, req.ID, &Task{ID: p.ID, Status: TaskStatus{State: TaskStateCanceled}})
			return
		}
		rpcResult(w, req.ID, SendMessageResponse{Task: &Task{ID: "task-x", Status: TaskStatus{
			State:   state,
			Message: &Message{Role: RoleAgent, Parts: []Part{{Text: &text}}},
		}}})
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(canceled)
	}
}

func TestExecutor_UnsuccessfulTaskIsAnError(t *testing.T) {
	for _, state := range []TaskState{
		TaskStateFailed, TaskStateRejected, TaskStateCanceled, TaskStateInputRequired, TaskStateAuthRequired,
	} {
		srv, cancels := stateAgent(t, state, "reason-"+string(state))
		e := NewExecutor(WithNoRetry())
		desc := &tools.ToolDescriptor{Name: "t", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

		_, err := e.Execute(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
		require.Error(t, err, state)
		assert.Contains(t, err.Error(), state.V03Name(), state)
		assert.Contains(t, err.Error(), "reason-"+string(state), "the agent's explanation is kept")

		_, _, err = e.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{"query":"q"}`))
		require.Error(t, err, state)

		if state.IsInterrupted() {
			assert.Eventually(t, func() bool { return cancels() > 0 }, time.Second, time.Millisecond,
				"an interrupted task nobody will answer is canceled")
		}
		_ = e.Close()
	}
}
