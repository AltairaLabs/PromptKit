package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the stateless revision (2026-07-28) and dual-era detection.

func TestEncodeHeaderValue_SpecExamples(t *testing.T) {
	// The table in 2026-07-28 basic/transports/streamable-http, "Value Encoding".
	for _, tc := range []struct{ in, want string }{
		{"us-west1", "us-west1"},
		{"Hello, 世界", "=?base64?SGVsbG8sIOS4lueVjA==?="},
		{" padded ", "=?base64?IHBhZGRlZCA=?="},
		{"line1\nline2", "=?base64?bGluZTEKbGluZTI=?="},
		{"=?base64?literal?=", "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?="},
		{"us west 1", "us west 1"},
		{"\tindented", "=?base64?CWluZGVudGVk?="},
		{"", ""},
	} {
		assert.Equal(t, tc.want, encodeHeaderValue(tc.in), "encoding %q", tc.in)
	}
}

func TestSetStandardHeaders(t *testing.T) {
	h := http.Header{}
	setStandardHeaders(h, methodToolsCall, json.RawMessage(`{"name":"get_weather","arguments":{}}`))
	assert.Equal(t, "tools/call", h.Get(headerMcpMethod))
	assert.Equal(t, "get_weather", h.Get(headerMcpName))

	h = http.Header{}
	setStandardHeaders(h, "resources/read", json.RawMessage(`{"uri":"file:///a b"}`))
	assert.Equal(t, "file:///a b", h.Get(headerMcpName), "Mcp-Name mirrors params.uri for resources/read")

	h = http.Header{}
	setStandardHeaders(h, methodToolsList, nil)
	assert.Equal(t, "tools/list", h.Get(headerMcpMethod))
	assert.Empty(t, h.Get(headerMcpName), "tools/list has no Mcp-Name")
}

func TestToolParamHeaders_Validation(t *testing.T) {
	valid, err := toolParamHeaders(json.RawMessage(`{"type":"object","properties":{
		"region":{"type":"string","x-mcp-header":"Region"},
		"n":{"type":"integer","x-mcp-header":"N"},
		"nested":{"type":"object","properties":{"flag":{"type":"boolean","x-mcp-header":"Flag"}}},
		"query":{"type":"string"}}}`))
	require.NoError(t, err)
	assert.Equal(t, []paramHeader{
		{name: "Flag", path: []string{"nested", "flag"}},
		{name: "N", path: []string{"n"}},
		{name: "Region", path: []string{"region"}},
	}, valid)

	for name, schema := range map[string]string{
		"empty name":        `{"properties":{"a":{"type":"string","x-mcp-header":""}}}`,
		"not a token":       `{"properties":{"a":{"type":"string","x-mcp-header":"Bad Name"}}}`,
		"control character": `{"properties":{"a":{"type":"string","x-mcp-header":"Bad\r\nName"}}}`,
		"duplicate":         `{"properties":{"a":{"type":"string","x-mcp-header":"X"},"b":{"type":"string","x-mcp-header":"x"}}}`,
		"number type":       `{"properties":{"a":{"type":"number","x-mcp-header":"A"}}}`,
		"object type":       `{"properties":{"a":{"type":"object","x-mcp-header":"A"}}}`,
		"through items":     `{"properties":{"a":{"type":"array","items":{"type":"string","x-mcp-header":"A"}}}}`,
		"through anyOf":     `{"properties":{"a":{"anyOf":[{"type":"string","x-mcp-header":"A"}]}}}`,
		"in $defs":          `{"$defs":{"d":{"type":"string","x-mcp-header":"A"}},"properties":{"a":{"$ref":"#/$defs/d"}}}`,
		"on the root":       `{"type":"string","x-mcp-header":"A"}`,
	} {
		_, err := toolParamHeaders(json.RawMessage(schema))
		assert.Error(t, err, name)
	}
}

func TestSetParamHeaders(t *testing.T) {
	headers := []paramHeader{
		{name: "Region", path: []string{"region"}},
		{name: "Priority", path: []string{"priority"}},
		{name: "Verbose", path: []string{"verbose"}},
		{name: "Missing", path: []string{"missing"}},
		{name: "Greeting", path: []string{"greeting"}},
	}
	h := http.Header{}
	require.NoError(t, setParamHeaders(h, headers,
		json.RawMessage(`{"region":"us-west1","priority":42,"verbose":null,"greeting":"Hello, 世界"}`)))
	assert.Equal(t, "us-west1", h.Get("Mcp-Param-Region"))
	assert.Equal(t, "42", h.Get("Mcp-Param-Priority"))
	assert.Empty(t, h.Values("Mcp-Param-Verbose"), "a null value gets no header")
	assert.Empty(t, h.Values("Mcp-Param-Missing"), "an absent value gets no header")
	assert.Equal(t, "=?base64?SGVsbG8sIOS4lueVjA==?=", h.Get("Mcp-Param-Greeting"))

	h = http.Header{}
	require.NoError(t, setParamHeaders(h, []paramHeader{{name: "B", path: []string{"b"}}}, json.RawMessage(`{"b":false}`)))
	assert.Equal(t, "false", h.Get("Mcp-Param-B"))

	err := setParamHeaders(http.Header{}, []paramHeader{{name: "N", path: []string{"n"}}}, json.RawMessage(`{"n":1.5}`))
	assert.Error(t, err, "a non-integer cannot fill an integer header")
	err = setParamHeaders(http.Header{}, []paramHeader{{name: "N", path: []string{"n"}}}, json.RawMessage(`{"n":9007199254740993}`))
	assert.Error(t, err, "integers beyond ±(2^53−1) are not safe")
}

func TestWithMeta_KeepsCallerMeta(t *testing.T) {
	out, err := withMeta(json.RawMessage(`{"name":"t","_meta":{"progressToken":"p1"}}`),
		map[string]any{metaProtocolVersion: "2026-07-28"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"t","_meta":{"progressToken":"p1","io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`,
		string(out))

	out, err = withMeta(nil, map[string]any{metaProtocolVersion: "v"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"_meta":{"io.modelcontextprotocol/protocolVersion":"v"}}`, string(out))

	_, err = withMeta(json.RawMessage(`[1]`), nil)
	assert.Error(t, err)
}

// modernServer is a 2026-07-28 Streamable HTTP server: stateless, no GET
// stream, every request validated for _meta and headers.
type modernServer struct {
	t        *testing.T
	mu       sync.Mutex
	requests []modernRequest
	gets     int
	handle   func(w http.ResponseWriter, req modernRequest) bool // true when handled
	versions []string
}

type modernRequest struct {
	ID      any
	Method  string
	Params  map[string]json.RawMessage
	Meta    map[string]json.RawMessage
	Headers http.Header
}

func (m *modernServer) start() *httptest.Server {
	if m.versions == nil {
		m.versions = []string{"2026-07-28"}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			m.mu.Lock()
			m.gets++
			m.mu.Unlock()
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var msg JSONRPCMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		req := modernRequest{ID: msg.ID, Method: msg.Method, Headers: r.Header.Clone()}
		_ = json.Unmarshal(msg.Params, &req.Params)
		_ = json.Unmarshal(req.Params["_meta"], &req.Meta)
		m.mu.Lock()
		m.requests = append(m.requests, req)
		m.mu.Unlock()

		var version string
		_ = json.Unmarshal(req.Meta[metaProtocolVersion], &version)
		if version == "" || r.Header.Get(headerProtocolVersion) != version {
			writeRPCError(w, http.StatusBadRequest, msg.ID, -32602, "missing or mismatched protocol version")
			return
		}
		supported := false
		for _, v := range m.versions {
			supported = supported || v == version
		}
		if !supported {
			w.Header().Set(headerContentType, contentTypeJSON)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32022,"message":"Unsupported protocol version",`+
				`"data":{"supported":%s,"requested":%q}}}`, mustJSON(msg.ID), mustJSON(m.versions), version)
			return
		}
		if m.handle != nil && m.handle(w, req) {
			return
		}
		switch msg.Method {
		case methodServerDiscover:
			writeJSONResult(w, msg.ID, fmt.Sprintf(`{"resultType":"complete","supportedVersions":%s,`+
				`"capabilities":{"tools":{}},"instructions":"be kind","ttlMs":0,"cacheScope":"private",`+
				`"_meta":{"io.modelcontextprotocol/serverInfo":{"name":"modern","version":"9"}}}`, mustJSON(m.versions)))
		case methodToolsList:
			writeJSONResult(w, msg.ID, `{"resultType":"complete","ttlMs":0,"cacheScope":"private","tools":[`+
				`{"name":"weather","inputSchema":{"type":"object","properties":{"region":{"type":"string","x-mcp-header":"Region"}}}},`+
				`{"name":"broken","inputSchema":{"type":"object","properties":{"f":{"type":"number","x-mcp-header":"F"}}}}]}`)
		default:
			writeJSONResult(w, msg.ID, `{"resultType":"complete","content":[{"type":"text","text":"done"}]}`)
		}
	}))
	m.t.Cleanup(srv.Close)
	return srv
}

func writeRPCError(w http.ResponseWriter, status int, id any, code int, message string) {
	w.Header().Set(headerContentType, contentTypeJSON)
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":%q}}`, mustJSON(id), code, message)
}

func (m *modernServer) snapshot() []modernRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]modernRequest(nil), m.requests...)
}

func newModernClient(t *testing.T, srv *httptest.Server, opts ClientOptions) *StreamableClient {
	t.Helper()
	c := NewStreamableClientWithOptions(ServerConfig{Name: "m", URL: srv.URL, TransportName: TransportStreamableHTTP}, opts)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestModern_DiscoveryAndStatelessRequests(t *testing.T) {
	m := &modernServer{t: t}
	srv := m.start()
	c := newModernClient(t, srv, DefaultClientOptions())

	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2026-07-28", info.ProtocolVersion)
	assert.Equal(t, "modern", info.ServerInfo.Name, "serverInfo comes from the discovery result's _meta")
	assert.Equal(t, "be kind", info.Instructions)

	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1, "a tool with an invalid x-mcp-header is excluded (2026-07-28 MUST)")
	assert.Equal(t, "weather", tools[0].Name)

	_, err = c.CallTool(context.Background(), "weather", json.RawMessage(`{"region":"us-west1"}`))
	require.NoError(t, err)

	reqs := m.snapshot()
	require.Len(t, reqs, 3)
	assert.Equal(t, []string{methodServerDiscover, methodToolsList, methodToolsCall},
		[]string{reqs[0].Method, reqs[1].Method, reqs[2].Method}, "no initialize handshake")
	for _, r := range reqs {
		assert.Contains(t, r.Meta, metaProtocolVersion)
		assert.Contains(t, r.Meta, metaClientCapabilities, "required on every request")
		assert.Contains(t, r.Meta, metaClientInfo)
		assert.Equal(t, r.Method, r.Headers.Get(headerMcpMethod))
		assert.Empty(t, r.Headers.Get(headerSessionID), "stateless: no session id")
	}
	call := reqs[2]
	assert.Equal(t, "weather", call.Headers.Get(headerMcpName))
	assert.Equal(t, "us-west1", call.Headers.Get("Mcp-Param-Region"))
	m.mu.Lock()
	assert.Zero(t, m.gets, "no standalone GET stream in the stateless revision")
	m.mu.Unlock()
}

func TestModern_InputRequiredIsAnsweredAndRetried(t *testing.T) {
	// 2026-07-28 MRTR: answer inputRequests, echo requestState exactly, use a
	// new id, and omit requestState when the server sent none.
	opts := DefaultClientOptions()
	opts.ElicitationHandler = func(context.Context, string, ElicitRequest) (ElicitResult, error) {
		return ElicitResult{Action: ElicitActionAccept, Content: json.RawMessage(`{"confirmed":true}`)}, nil
	}
	m := &modernServer{t: t}
	round := 0
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		if req.Method != methodToolsCall {
			return false
		}
		round++
		switch round {
		case 1:
			writeJSONResult(w, req.ID, `{"resultType":"input_required","requestState":"opaqueéstate==",`+
				`"inputRequests":{"ok":{"method":"elicitation/create","params":{"message":"sure?",`+
				`"requestedSchema":{"type":"object","properties":{"confirmed":{"type":"boolean"}}}}}}}`)
		case 2:
			writeJSONResult(w, req.ID, `{"resultType":"input_required","inputRequests":{"again":{"method":"elicitation/create",`+
				`"params":{"message":"really?"}}}}`)
		default:
			writeJSONResult(w, req.ID, `{"resultType":"complete","content":[{"type":"text","text":"done"}]}`)
		}
		return true
	}
	srv := m.start()
	c := newModernClient(t, srv, opts)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	resp, err := c.CallTool(context.Background(), "act", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "done", resp.Content[0].Text)

	var calls []modernRequest
	for _, r := range m.snapshot() {
		if r.Method == methodToolsCall {
			calls = append(calls, r)
		}
	}
	require.Len(t, calls, 3)
	assert.NotEqual(t, calls[0].ID, calls[1].ID, "the retry is a new request with a new id")
	assert.JSONEq(t, `{"ok":{"action":"accept","content":{"confirmed":true}}}`, string(calls[1].Params["inputResponses"]))
	assert.JSONEq(t, `"opaqueéstate=="`, string(calls[1].Params["requestState"]), "requestState is echoed unchanged")
	assert.NotContains(t, calls[2].Params, "requestState", "no requestState is sent when the server sent none")
	assert.Contains(t, string(calls[2].Params["inputResponses"]), `"again"`)
}

func TestModern_InputRequiredForUnadvertisedInputFails(t *testing.T) {
	m := &modernServer{t: t}
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		if req.Method != methodToolsCall {
			return false
		}
		writeJSONResult(w, req.ID, `{"resultType":"input_required","inputRequests":{"s":{"method":"sampling/createMessage"}}}`)
		return true
	}
	c := newModernClient(t, m.start(), DefaultClientOptions())
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	_, err = c.CallTool(context.Background(), "act", json.RawMessage(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sampling/createMessage")
}

func TestModern_ResultTypeIsChecked(t *testing.T) {
	m := &modernServer{t: t}
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		switch req.Method {
		case methodToolsList:
			writeJSONResult(w, req.ID, `{"resultType":"input_required","requestState":"x"}`)
		case methodToolsCall:
			writeJSONResult(w, req.ID, `{"resultType":"mystery","content":[]}`)
		default:
			return false
		}
		return true
	}
	opts := DefaultClientOptions()
	opts.EnableGracefulDegradation = false
	c := newModernClient(t, m.start(), opts)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	_, err = c.ListTools(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not allow it", "input_required is only valid for some methods")
	_, err = c.CallTool(context.Background(), "x", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unrecognized resultType "mystery"`)
}

func TestModern_UnsupportedVersionDuringDiscoveryRetriesWithASupportedOne(t *testing.T) {
	saved := modernProtocolVersions
	modernProtocolVersions = []string{"2099-01-01", "2026-07-28"}
	defer func() { modernProtocolVersions = saved }()

	m := &modernServer{t: t}
	c := newModernClient(t, m.start(), DefaultClientOptions())
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2026-07-28", info.ProtocolVersion)

	reqs := m.snapshot()
	require.Len(t, reqs, 2)
	assert.Equal(t, "2099-01-01", reqs[0].Headers.Get(headerProtocolVersion))
	assert.Equal(t, "2026-07-28", reqs[1].Headers.Get(headerProtocolVersion), "retried with the version the server listed")
}

func TestModern_HeaderMismatchRelistsAndRetries(t *testing.T) {
	m := &modernServer{t: t}
	mismatched := false
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		if req.Method == methodToolsCall && !mismatched {
			mismatched = true
			writeRPCError(w, http.StatusBadRequest, req.ID, codeHeaderMismatch, "Mcp-Param-Region missing")
			return true
		}
		return false
	}
	c := newModernClient(t, m.start(), DefaultClientOptions())
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	_, err = c.CallTool(context.Background(), "weather", json.RawMessage(`{"region":"eu"}`))
	require.NoError(t, err)
	var methods []string
	for _, r := range m.snapshot() {
		methods = append(methods, r.Method)
	}
	assert.Equal(t, []string{methodServerDiscover, methodToolsCall, methodToolsList, methodToolsCall}, methods,
		"after a HeaderMismatch the client re-lists tools, then resends")
	last := m.snapshot()[3]
	assert.Equal(t, "eu", last.Headers.Get("Mcp-Param-Region"))
}

func TestModern_BrokenResponseStreamIsReissued(t *testing.T) {
	// 2026-07-28: streams are not resumable; a broken one loses the request
	// and the client MUST re-issue it with a new id.
	m := &modernServer{t: t}
	calls := 0
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		if req.Method != methodToolsCall {
			return false
		}
		calls++
		w.Header().Set(headerContentType, contentTypeSSE)
		if calls == 1 {
			_, _ = fmt.Fprint(w, "id: 1\ndata: \n\n") // ends without a response
			return true
		}
		_, _ = fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"resultType\":\"complete\","+
			"\"content\":[{\"type\":\"text\",\"text\":\"second\"}]}}\n\n", mustJSON(req.ID))
		return true
	}
	c := newModernClient(t, m.start(), DefaultClientOptions())
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	resp, err := c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "second", resp.Content[0].Text)
	assert.Equal(t, 2, calls)
	m.mu.Lock()
	assert.Zero(t, m.gets, "no Last-Event-ID resumption in the stateless revision")
	m.mu.Unlock()
}

func TestModern_TimeoutClosesTheStreamWithoutANotification(t *testing.T) {
	// On Streamable HTTP closing the response stream is the cancellation;
	// no notifications/cancelled is expected.
	m := &modernServer{t: t}
	m.handle = func(_ http.ResponseWriter, req modernRequest) bool {
		if req.Method != methodToolsCall {
			return false
		}
		time.Sleep(300 * time.Millisecond)
		return true
	}
	opts := DefaultClientOptions()
	opts.RequestTimeout = 50 * time.Millisecond
	c := newModernClient(t, m.start(), opts)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	_, err = c.CallTool(context.Background(), "slow", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrServerUnresponsive)
	time.Sleep(100 * time.Millisecond)
	for _, r := range m.snapshot() {
		assert.NotEqual(t, methodNotificationsCancel, r.Method)
	}
}

func TestStdio_ProbesThenFallsBackToTheHandshake(t *testing.T) {
	// stdio: probe with server/discover; any non-modern error means legacy,
	// and the client falls back to initialize.
	p := newStdioPeer(t, DefaultClientOptions())
	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.connect(context.Background())
		done <- err
	}()

	probe := p.next()
	require.Equal(t, methodServerDiscover, probe.Method)
	var params map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(probe.Params, &params))
	assert.Contains(t, string(params["_meta"]), `"io.modelcontextprotocol/protocolVersion":"2026-07-28"`)
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"error":{"code":-32601,"message":"Method not found"}}`, probe.ID))

	init := p.next()
	require.Equal(t, methodInitialize, init.Method)
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":%s}`, init.ID, initResult2025))
	assert.Equal(t, methodNotificationsInitialized, p.next().Method)
	require.NoError(t, <-done)
	assert.False(t, p.client.sess.isModern())
}

func TestStdio_SilentProbeFallsBackAfterTheTimeout(t *testing.T) {
	opts := DefaultClientOptions()
	opts.EraProbeTimeout = 100 * time.Millisecond
	p := newStdioPeer(t, opts)
	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.connect(context.Background())
		done <- err
	}()
	assert.Equal(t, methodServerDiscover, p.next().Method) // never answered
	init := p.next()
	require.Equal(t, methodInitialize, init.Method, "a legacy server that ignores the probe gets the handshake")
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":%s}`, init.ID, initResult2025))
	p.next()
	require.NoError(t, <-done)
}

func TestStdio_ModernServer(t *testing.T) {
	p := newStdioPeer(t, DefaultClientOptions())
	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.connect(context.Background())
		done <- err
	}()
	probe := p.next()
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"resultType":"complete","supportedVersions":["2026-07-28"],`+
		`"capabilities":{"tools":{}},"ttlMs":0,"cacheScope":"private"}}`, probe.ID))
	require.NoError(t, <-done)
	assert.True(t, p.client.sess.isModern())

	go func() {
		_, err := p.client.sess.listTools(context.Background())
		done <- err
	}()
	list := p.next()
	assert.Contains(t, string(list.Params), metaClientCapabilities, "every stdio request carries the protocol _meta too")
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"resultType":"complete","tools":[]}}`, list.ID))
	require.NoError(t, <-done)
}

func TestDualEra_ModernErrorDuringProbeIsNotALegacySignal(t *testing.T) {
	m := &modernServer{t: t}
	m.handle = func(w http.ResponseWriter, req modernRequest) bool {
		writeRPCError(w, http.StatusBadRequest, req.ID, codeMissingRequiredClientCapability, "needs elicitation")
		return true
	}
	c := newModernClient(t, m.start(), DefaultClientOptions())
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected discovery")
	for _, r := range m.snapshot() {
		assert.NotEqual(t, methodInitialize, r.Method, "a modern error must not trigger the legacy fallback")
	}
}

func TestDualEra_DisableModernProtocolSkipsTheProbe(t *testing.T) {
	f := &streamableFake{}
	f.handle = func(w http.ResponseWriter, _ *http.Request, msg JSONRPCMessage) {
		writeJSONResult(w, msg.ID, initResult2025)
	}
	opts := DefaultClientOptions()
	opts.DisableModernProtocol = true
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: f.serve(t), TransportName: TransportStreamableHTTP}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "initialize", f.methods()[0])
	assert.False(t, strings.Contains(strings.Join(f.methods(), ","), methodServerDiscover))
}

func TestHTTPAuto_FallsBackToSSEWhenTheServerHasNoStreamableEndpoint(t *testing.T) {
	// A 2024-11-05 server hosts GET /sse and a message endpoint; a POST to
	// the base URL is refused (404). The auto client then uses HTTP+SSE.
	stream := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, contentTypeSSE)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case frame := <-stream:
				_, _ = fmt.Fprint(w, frame)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		var msg JSONRPCMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		w.WriteHeader(http.StatusAccepted)
		if msg.Method == methodInitialize {
			stream <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n",
				mustJSON(msg.ID), `{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"old","version":"1"}}`)
		}
	})
	mux.HandleFunc("/", http.NotFound)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	reg := NewRegistry()
	c := reg.newClientFunc(ServerConfig{Name: "old", URL: srv.URL})
	defer c.Close()
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "old", info.ServerInfo.Name)
	assert.True(t, c.IsAlive())
	_, isSSE := c.(*httpAutoClient).active.(*SSEClient)
	assert.True(t, isSSE)
}

func TestHTTPAuto_UsesStreamableHTTPWhenTheServerHostsIt(t *testing.T) {
	m := &modernServer{t: t}
	srv := m.start()
	c := NewRegistry().newClientFunc(ServerConfig{Name: "m", URL: srv.URL})
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	_, err = c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	require.NoError(t, err)
	_, isStreamable := c.(*httpAutoClient).active.(*StreamableClient)
	assert.True(t, isStreamable)

	require.NoError(t, c.Close())
	assert.False(t, c.IsAlive())
	_, err = c.ListTools(context.Background())
	require.ErrorIs(t, err, ErrClientClosed)
}

func TestHTTPAuto_OtherFailuresDoNotFallBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewRegistry().newClientFunc(ServerConfig{Name: "x", URL: srv.URL})
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.Nil(t, c.(*httpAutoClient).active)
	_, err = c.ListTools(context.Background())
	require.ErrorIs(t, err, ErrClientNotInitialized)
}
