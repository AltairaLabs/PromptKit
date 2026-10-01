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

// asMap round-trips v through JSON so tests can assert on the wire shape.
func asMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func strPtr(s string) *string { return &s }

func sampleTask() *Task {
	return &Task{
		ID:        "t1",
		ContextID: "c1",
		Status: TaskStatus{
			State:   TaskStateInputRequired,
			Message: &Message{MessageID: "m1", Role: RoleAgent, Parts: []Part{{Text: strPtr("need input")}}},
		},
		Artifacts: []Artifact{{
			ArtifactID: "a1",
			Parts: []Part{
				{Text: strPtr("hi")},
				{Raw: []byte("PNG"), MediaType: "image/png", Filename: "x.png"},
				{URL: strPtr("https://example.com/y"), MediaType: "text/plain"},
				{Data: map[string]any{"k": "v"}},
			},
		}},
	}
}

func TestWire_V1TaskShape(t *testing.T) {
	m := asMap(t, ProtocolVersion10.WireSendResult(sampleTask()))
	task, ok := m["task"].(map[string]any)
	require.True(t, ok, "1.0 SendMessage result is wrapped as {task}")
	assert.NotContains(t, task, "kind")
	status := task["status"].(map[string]any)
	assert.Equal(t, "TASK_STATE_INPUT_REQUIRED", status["state"])
	assert.Equal(t, "ROLE_AGENT", status["message"].(map[string]any)["role"])
	parts := task["artifacts"].([]any)[0].(map[string]any)["parts"].([]any)
	assert.Equal(t, "hi", parts[0].(map[string]any)["text"])
	assert.Contains(t, parts[1], "raw")
	assert.Contains(t, parts[2], "url")
}

func TestWire_V03TaskShape(t *testing.T) {
	m := asMap(t, ProtocolVersion03.WireSendResult(sampleTask()))
	assert.Equal(t, "task", m["kind"], "0.3 result is the bare kind-tagged task")
	status := m["status"].(map[string]any)
	assert.Equal(t, "input-required", status["state"])
	msg := status["message"].(map[string]any)
	assert.Equal(t, "message", msg["kind"])
	assert.Equal(t, "agent", msg["role"])

	parts := m["artifacts"].([]any)[0].(map[string]any)["parts"].([]any)
	assert.Equal(t, map[string]any{"kind": "text", "text": "hi"}, parts[0])
	file := parts[1].(map[string]any)
	assert.Equal(t, "file", file["kind"])
	assert.Equal(t, map[string]any{"bytes": "UE5H", "mimeType": "image/png", "name": "x.png"}, file["file"])
	assert.Equal(t, map[string]any{"uri": "https://example.com/y", "mimeType": "text/plain"},
		parts[2].(map[string]any)["file"])
	assert.Equal(t, map[string]any{"kind": "data", "data": map[string]any{"k": "v"}}, parts[3])
}

func TestWire_V03RoundTripsThroughLenientDecode(t *testing.T) {
	data, err := json.Marshal(ProtocolVersion03.WireTask(sampleTask()))
	require.NoError(t, err)
	var got Task
	require.NoError(t, json.Unmarshal(data, &got))
	want := sampleTask()
	assert.Equal(t, want.Status.State, got.Status.State)
	assert.Equal(t, want.Status.Message.Role, got.Status.Message.Role)
	assert.Equal(t, want.Artifacts[0].Parts, got.Artifacts[0].Parts)
}

func TestWire_StreamEvents(t *testing.T) {
	status := &TaskStatusUpdateEvent{TaskID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateCompleted}}
	working := &TaskStatusUpdateEvent{TaskID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateWorking}}
	art := &TaskArtifactUpdateEvent{TaskID: "t", ContextID: "c",
		Artifact: Artifact{ArtifactID: "a", Parts: []Part{{Text: strPtr("x")}}}, LastChunk: true}

	v1, err := ProtocolVersion10.WireStreamEvent(status)
	require.NoError(t, err)
	m := asMap(t, v1)
	require.Contains(t, m, "statusUpdate")
	assert.NotContains(t, m["statusUpdate"], "final", "1.0 has no final flag")

	v1, err = ProtocolVersion10.WireStreamEvent(art)
	require.NoError(t, err)
	assert.Contains(t, asMap(t, v1), "artifactUpdate")

	v03, err := ProtocolVersion03.WireStreamEvent(status)
	require.NoError(t, err)
	m = asMap(t, v03)
	assert.Equal(t, "status-update", m["kind"])
	assert.Equal(t, true, m["final"], "a terminal status ends a 0.3 stream")

	v03, err = ProtocolVersion03.WireStreamEvent(working)
	require.NoError(t, err)
	assert.Equal(t, false, asMap(t, v03)["final"], "final is required, and false mid-stream")

	v03, err = ProtocolVersion03.WireStreamEvent(art)
	require.NoError(t, err)
	m = asMap(t, v03)
	assert.Equal(t, "artifact-update", m["kind"])
	assert.Equal(t, true, m["lastChunk"])

	v03, err = ProtocolVersion03.WireStreamEvent(sampleTask())
	require.NoError(t, err)
	assert.Equal(t, "task", asMap(t, v03)["kind"])

	_, err = ProtocolVersion10.WireStreamEvent("nope")
	assert.Error(t, err)
}

func TestPart_UnmarshalV03(t *testing.T) {
	var parts []Part
	require.NoError(t, json.Unmarshal([]byte(`[
		{"kind":"text","text":"hello"},
		{"kind":"file","file":{"bytes":"UE5H","mimeType":"image/png","name":"a.png"}},
		{"kind":"file","file":{"uri":"https://e.com/f","mimeType":"application/pdf"}},
		{"kind":"data","data":{"a":1}}
	]`), &parts))
	require.Len(t, parts, 4)
	assert.Equal(t, "hello", *parts[0].Text)
	assert.Equal(t, []byte("PNG"), parts[1].Raw)
	assert.Equal(t, "image/png", parts[1].MediaType)
	assert.Equal(t, "a.png", parts[1].Filename)
	assert.Equal(t, "https://e.com/f", *parts[2].URL)
	assert.Equal(t, map[string]any{"a": float64(1)}, parts[3].Data)

	var bad Part
	assert.Error(t, json.Unmarshal([]byte(`{"kind":"file","file":{"bytes":"!!"}}`), &bad))
}

func TestSendMessageConfiguration_WaitsForCompletion(t *testing.T) {
	var nilCfg *SendMessageConfiguration
	assert.True(t, nilCfg.WaitsForCompletion(ProtocolVersion10), "1.0 blocks by default")
	assert.False(t, nilCfg.WaitsForCompletion(ProtocolVersion03), "0.3 does not")
	assert.False(t, (&SendMessageConfiguration{ReturnImmediately: true}).WaitsForCompletion(ProtocolVersion10))
	assert.True(t, (&SendMessageConfiguration{Blocking: true}).WaitsForCompletion(ProtocolVersion03))
}

func TestWireSendParams(t *testing.T) {
	req := &SendMessageRequest{
		Message:       Message{MessageID: "m", Role: RoleUser, Parts: []Part{{Text: strPtr("q")}}},
		Configuration: &SendMessageConfiguration{Blocking: true, ReturnImmediately: true},
	}
	assert.Same(t, req, ProtocolVersion10.WireSendParams(req))

	m := asMap(t, ProtocolVersion03.WireSendParams(req))
	msg := m["message"].(map[string]any)
	assert.Equal(t, "message", msg["kind"])
	assert.Equal(t, "user", msg["role"])
	assert.Equal(t, "text", msg["parts"].([]any)[0].(map[string]any)["kind"])
	assert.Equal(t, map[string]any{"blocking": true}, m["configuration"])
}

func sampleCard() *AgentCard {
	return &AgentCard{
		Name:        "agent",
		Description: "d",
		Version:     "1",
		SupportedInterfaces: []AgentInterface{
			{URL: "https://a.example/a2a", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "1.0"},
			{URL: "https://a.example/a2a", ProtocolBinding: ProtocolBindingJSONRPC, ProtocolVersion: "0.3"},
		},
		Capabilities: AgentCapabilities{Streaming: true},
		SecuritySchemes: map[string]SecurityScheme{
			"bearer": {HTTPAuth: &HTTPAuthSecurityScheme{Scheme: "Bearer", BearerFormat: "JWT"}},
			"key":    {APIKey: &APIKeySecurityScheme{Location: "header", Name: "X-Key"}},
		},
		SecurityRequirements: []SecurityRequirement{RequireScheme("bearer")},
		Skills:               []AgentSkill{{ID: "s", Name: "S"}},
	}
}

func TestWire_Card(t *testing.T) {
	v1 := asMap(t, ProtocolVersion10.WireAgentCard(sampleCard()))
	assert.Equal(t, map[string]any{"httpAuthSecurityScheme": map[string]any{"scheme": "Bearer", "bearerFormat": "JWT"}},
		v1["securitySchemes"].(map[string]any)["bearer"])
	assert.Equal(t, []any{map[string]any{"schemes": map[string]any{"bearer": map[string]any{"list": []any{}}}}},
		v1["securityRequirements"])
	assert.NotContains(t, v1, "url")

	v03 := asMap(t, ProtocolVersion03.WireAgentCard(sampleCard()))
	assert.Equal(t, "0.3.0", v03["protocolVersion"])
	assert.Equal(t, "https://a.example/a2a", v03["url"])
	assert.Equal(t, "JSONRPC", v03["preferredTransport"])
	assert.Contains(t, v03, "supportedInterfaces", "kept for 1.0 clients reading an unversioned card")
	assert.Equal(t, map[string]any{"type": "http", "scheme": "Bearer", "bearerFormat": "JWT"},
		v03["securitySchemes"].(map[string]any)["bearer"])
	assert.Equal(t, map[string]any{"type": "apiKey", "in": "header", "name": "X-Key"},
		v03["securitySchemes"].(map[string]any)["key"])
	assert.Equal(t, []any{map[string]any{"bearer": []any{}}}, v03["security"])
	skill := v03["skills"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{}, skill["tags"], "0.3 requires tags")
}

func TestAgentCard_UnmarshalBothShapes(t *testing.T) {
	for _, v := range []ProtocolVersion{ProtocolVersion10, ProtocolVersion03} {
		data, err := json.Marshal(v.WireAgentCard(sampleCard()))
		require.NoError(t, err)
		var got AgentCard
		require.NoError(t, json.Unmarshal(data, &got), v)
		assert.Equal(t, sampleCard().SecuritySchemes, got.SecuritySchemes, v)
		assert.Equal(t, sampleCard().SecurityRequirements, got.SecurityRequirements, v)
		assert.Equal(t, ProtocolVersion10, got.PreferredVersion(), v)
	}

	// A pure 0.3 card: the endpoint comes from url/preferredTransport.
	var old AgentCard
	require.NoError(t, json.Unmarshal([]byte(`{
		"name":"old","url":"https://o.example/rpc","preferredTransport":"JSONRPC","protocolVersion":"0.3.0",
		"additionalInterfaces":[{"url":"https://o.example/grpc","transport":"GRPC"}],
		"security":[{"oauth":["read"]}],
		"securitySchemes":{"oauth":{"type":"oauth2","flows":{"clientCredentials":{"tokenUrl":"https://t","scopes":{}}}},
			"oidc":{"type":"openIdConnect","openIdConnectUrl":"https://i"},"mtls":{"type":"mutualTLS"}},
		"supportsAuthenticatedExtendedCard":true
	}`), &old))
	assert.Equal(t, []AgentInterface{
		{URL: "https://o.example/rpc", ProtocolBinding: "JSONRPC", ProtocolVersion: "0.3"},
		{URL: "https://o.example/grpc", ProtocolBinding: "GRPC", ProtocolVersion: "0.3"},
	}, old.SupportedInterfaces)
	assert.Equal(t, []SecurityRequirement{{Schemes: map[string][]string{"oauth": {"read"}}}}, old.SecurityRequirements)
	assert.Equal(t, "https://t", old.SecuritySchemes["oauth"].OAuth2.Flows.ClientCredentials.TokenURL)
	assert.Equal(t, "https://i", old.SecuritySchemes["oidc"].OpenIDConnect.OpenIDConnectURL)
	assert.NotNil(t, old.SecuritySchemes["mtls"].MutualTLS)
	assert.True(t, old.Capabilities.ExtendedAgentCard)
	assert.Equal(t, ProtocolVersion03, old.PreferredVersion())
}

func TestSecurityScheme_V03RoundTrip(t *testing.T) {
	schemes := []SecurityScheme{
		{APIKey: &APIKeySecurityScheme{Location: "query", Name: "k"}},
		{HTTPAuth: &HTTPAuthSecurityScheme{Scheme: "Basic"}},
		{OAuth2: &OAuth2SecurityScheme{Flows: OAuthFlows{Password: &OAuthFlow{TokenURL: "https://t"}}}},
		{OpenIDConnect: &OpenIDConnectSecurityScheme{OpenIDConnectURL: "https://o"}},
		{MutualTLS: &MutualTLSSecurityScheme{Description: "m"}},
	}
	for _, s := range schemes {
		data, err := json.Marshal(s.v03())
		require.NoError(t, err)
		var got SecurityScheme
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, s, got)
	}
	assert.Equal(t, v03SecurityScheme{}, (&SecurityScheme{}).v03())
}

func TestPreferredVersion(t *testing.T) {
	var nilCard *AgentCard
	assert.Equal(t, ProtocolVersion10, nilCard.PreferredVersion())
	assert.Equal(t, ProtocolVersion10, (&AgentCard{}).PreferredVersion())
	grpcOnly := &AgentCard{SupportedInterfaces: []AgentInterface{{ProtocolBinding: "GRPC", ProtocolVersion: "0.3"}}}
	assert.Equal(t, ProtocolVersion10, grpcOnly.PreferredVersion())
}

func TestParseProtocolVersion(t *testing.T) {
	for in, want := range map[string]ProtocolVersion{
		"": "", "1.0": ProtocolVersion10, " 1.0.2 ": ProtocolVersion10, "0.3": ProtocolVersion03, "0.3.0": ProtocolVersion03,
	} {
		got, err := ParseProtocolVersion(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"2.0", "0.2", "one"} {
		_, err := ParseProtocolVersion(bad)
		assert.Error(t, err, bad)
	}
}

func TestLookupMethodAndMethod(t *testing.T) {
	ops := []Operation{OpSendMessage, OpSendStreamingMessage, OpGetTask, OpCancelTask,
		OpListTasks, OpSubscribeToTask, OpGetExtendedAgentCard}
	for _, v := range []ProtocolVersion{ProtocolVersion10, ProtocolVersion03} {
		for _, op := range ops {
			name := v.Method(op)
			require.NotEmpty(t, name, "%s op %d", v, op)
			gotOp, gotV, ok := LookupMethod(name)
			require.True(t, ok, name)
			assert.Equal(t, op, gotOp, name)
			assert.Equal(t, v, gotV, name)
		}
		assert.Empty(t, v.Method(OpPushNotificationConfig))
	}
	op, _, ok := LookupMethod(MethodLegacySubscribe)
	assert.True(t, ok)
	assert.Equal(t, OpSubscribeToTask, op)
	_, _, ok = LookupMethod("nope")
	assert.False(t, ok)
}

// versionedAgent is a test agent that answers only one protocol version's
// method names and records what it received.
type versionedAgent struct {
	speaks ProtocolVersion

	mu       sync.Mutex
	methods  []string
	versions []string
}

func (a *versionedAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := decodeRPC(r)
	a.mu.Lock()
	a.methods = append(a.methods, req.Method)
	a.versions = append(a.versions, r.Header.Get(HeaderVersion))
	a.mu.Unlock()

	_, v, ok := LookupMethod(req.Method)
	if !ok || v != a.speaks {
		rpcErrorResp(w, req.ID, ErrCodeMethodNotFound, "Method not found")
		return
	}
	task := &Task{ID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateCompleted}}
	if strings := req.Method; strings == MethodV1SendStreamingMessage || strings == MethodV03SendStreamingMessage {
		w.Header().Set("Content-Type", "text/event-stream")
		evt, _ := a.speaks.WireStreamEvent(task)
		_, _ = w.Write([]byte(sseEvent(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: mustJSON(evt)})))
		return
	}
	rpcResult(w, req.ID, a.speaks.WireSendResult(task))
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func TestClient_SpeaksV1WithHeader(t *testing.T) {
	agent := &versionedAgent{speaks: ProtocolVersion10}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.SendMessage(context.Background(), &SendMessageRequest{})
	require.NoError(t, err)
	assert.Equal(t, "t", task.ID, "the {task} wrapper is unwrapped")
	assert.Equal(t, []string{MethodV1SendMessage}, agent.methods)
	assert.Equal(t, []string{"1.0"}, agent.versions)
	assert.Equal(t, ProtocolVersion10, c.ProtocolVersion())
}

func TestClient_FallsBackToV03AndRemembers(t *testing.T) {
	agent := &versionedAgent{speaks: ProtocolVersion03}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.SendMessage(context.Background(), &SendMessageRequest{})
	require.NoError(t, err)
	assert.Equal(t, TaskStateCompleted, task.Status.State)
	assert.Equal(t, ProtocolVersion03, c.ProtocolVersion())

	_, err = c.GetTask(context.Background(), "t")
	require.NoError(t, err)
	assert.Equal(t, []string{MethodV1SendMessage, MethodV03SendMessage, MethodV03GetTask}, agent.methods,
		"after one fallback the client speaks 0.3 without probing again")
	assert.Equal(t, []string{"1.0", "0.3", "0.3"}, agent.versions)
}

func TestClient_StreamFallsBackToV03(t *testing.T) {
	agent := &versionedAgent{speaks: ProtocolVersion03}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL)
	ch, err := c.SendMessageStream(context.Background(), &SendMessageRequest{})
	require.NoError(t, err)
	var events []StreamEvent
	for e := range ch {
		events = append(events, e)
	}
	require.Len(t, events, 1)
	require.NotNil(t, events[0].Task, "the kind-tagged 0.3 task is decoded as a Task event")
	assert.Equal(t, []string{MethodV1SendStreamingMessage, MethodV03SendStreamingMessage}, agent.methods)
}

func TestClient_PinnedVersionDoesNotFallBack(t *testing.T) {
	agent := &versionedAgent{speaks: ProtocolVersion03}
	srv := httptest.NewServer(agent)
	defer srv.Close()

	c := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion10))
	_, err := c.SendMessage(context.Background(), &SendMessageRequest{})
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, ErrCodeMethodNotFound, rpcErr.Code)
	assert.Len(t, agent.methods, 1)
}

func TestClient_DiscoverPinsCardVersion(t *testing.T) {
	// The card's interface is on the agent's own host; one on another host
	// is not followed and settles nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"x","url":"http://` + r.Host + `/a2a","protocolVersion":"0.3.0"}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	_, err := c.Discover(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion03, c.ProtocolVersion())
}

func TestDecodeSendResult(t *testing.T) {
	for name, raw := range map[string]string{
		"v1 task":    `{"task":{"id":"t","contextId":"c","status":{"state":"TASK_STATE_COMPLETED"}}}`,
		"v03 task":   `{"kind":"task","id":"t","contextId":"c","status":{"state":"completed"}}`,
		"legacy":     `{"id":"t","contextId":"c","status":{"state":"completed"}}`,
		"v1 message": `{"message":{"messageId":"m","taskId":"t","role":"ROLE_AGENT","parts":[{"text":"hi"}]}}`,
		"v03 msg":    `{"kind":"message","messageId":"m","taskId":"t","role":"agent","parts":[{"kind":"text","text":"hi"}]}`,
	} {
		task, err := decodeSendResult(json.RawMessage(raw))
		require.NoError(t, err, name)
		assert.Equal(t, "t", task.ID, name)
		assert.Equal(t, TaskStateCompleted, task.Status.State, name)
	}
	_, err := decodeSendResult(json.RawMessage(`[]`))
	assert.Error(t, err)
	_, err = decodeSendResult(json.RawMessage(`{"message":{"role":7}}`))
	assert.Error(t, err)
}

func TestParseStreamEvent_AllShapes(t *testing.T) {
	cases := map[string]struct {
		data  string
		check func(StreamEvent) bool
	}{
		"v1 task":     {`{"result":{"task":{"id":"t","status":{"state":"TASK_STATE_WORKING"}}}}`, func(e StreamEvent) bool { return e.Task != nil }},
		"v1 status":   {`{"result":{"statusUpdate":{"taskId":"t","status":{"state":"TASK_STATE_COMPLETED"}}}}`, func(e StreamEvent) bool { return e.StatusUpdate != nil }},
		"v1 artifact": {`{"result":{"artifactUpdate":{"taskId":"t","artifact":{"artifactId":"a","parts":[]}}}}`, func(e StreamEvent) bool { return e.ArtifactUpdate != nil }},
		"v1 message":  {`{"result":{"message":{"messageId":"m","role":"ROLE_AGENT","parts":[]}}}`, func(e StreamEvent) bool { return e.Message != nil }},
		"v03 status": {`{"kind":"status-update","taskId":"t","status":{"state":"input-required"},"final":true}`, func(e StreamEvent) bool {
			return e.StatusUpdate != nil && e.StatusUpdate.Status.State == TaskStateInputRequired
		}},
		"v03 artifact": {`{"kind":"artifact-update","taskId":"t","artifact":{"artifactId":"a","parts":[{"kind":"text","text":"x"}]}}`, func(e StreamEvent) bool { return e.ArtifactUpdate != nil }},
		"v03 message":  {`{"kind":"message","messageId":"m","role":"agent","parts":[]}`, func(e StreamEvent) bool { return e.Message != nil }},
		"legacy task":  {`{"id":"t","status":{"state":"working"}}`, func(e StreamEvent) bool { return e.Task != nil }},
	}
	for name, tc := range cases {
		evt, ok := parseStreamEvent(tc.data)
		require.True(t, ok, name)
		assert.True(t, tc.check(evt), name)
	}
	for _, bad := range []string{`{"task":{"status":{"state":"bogus"}}}`, `{"kind":"status-update","status":{"state":"bogus"}}`, `{}`, `nope`} {
		_, ok := parseStreamEvent(bad)
		assert.False(t, ok, bad)
	}
}

func TestOpenStream_JSONErrorIsReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcErrorResp(w, req.ID, ErrCodeUnsupportedOperation, "Streaming is not supported")
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion10)).
		SendMessageStream(context.Background(), &SendMessageRequest{})
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, ErrCodeUnsupportedOperation, rpcErr.Code)
}

// An agent whose result does not decode fails the call with an error naming
// the method, rather than returning a zero value.
func TestClient_UndecodableResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcResult(w, decodeRPC(r).ID, "not an object")
	}))
	defer srv.Close()
	c := NewClient(srv.URL, WithProtocolVersion(ProtocolVersion10))
	ctx := context.Background()

	_, err := c.SendMessage(ctx, &SendMessageRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), MethodV1SendMessage+": decode result")

	_, err = c.GetTask(ctx, "t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), MethodV1GetTask+": decode result")

	_, err = c.ListTasks(ctx, &ListTasksRequest{ContextID: "c"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), MethodV1ListTasks+": decode result")
}
