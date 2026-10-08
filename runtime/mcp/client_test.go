package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gosdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envTestServer makes the test binary, re-executed, act as a stdio MCP
// server: "gosdk" serves goSDKTestServer, "silent" a handshake-era server
// that never answers requests it does not know.
const envTestServer = "PROMPTKIT_TEST_MCP_SERVER"

func TestMain(m *testing.M) {
	switch os.Getenv(envTestServer) {
	case "gosdk":
		_ = goSDKTestServer(nil).Run(context.Background(), &gosdk.StdioTransport{})
		return
	case "silent":
		serveSilentLegacy()
		return
	}
	os.Exit(m.Run())
}

type addIn struct {
	A int `json:"a"`
	B int `json:"b"`
}

type addOut struct {
	Total int `json:"total"`
}

// goSDKTestServer is an official-SDK server: a structured tool, a failing
// tool, a slow tool, a tool that elicits (mid-call on a handshake-era
// session, by multi round-trip on a stateless one) and a tool that kills a
// stdio server's process.
func goSDKTestServer(opts *gosdk.ServerOptions) *gosdk.Server {
	s := gosdk.NewServer(&gosdk.Implementation{Name: "ref", Version: "1"}, opts)
	gosdk.AddTool(s, &gosdk.Tool{Name: "add"}, func(_ context.Context, _ *gosdk.CallToolRequest, in addIn) (*gosdk.CallToolResult, addOut, error) {
		return nil, addOut{Total: in.A + in.B}, nil
	})
	gosdk.AddTool(s, &gosdk.Tool{Name: "boom"}, func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
		return nil, nil, errors.New("kaboom")
	})
	gosdk.AddTool(s, &gosdk.Tool{Name: "slow"}, func(ctx context.Context, _ *gosdk.CallToolRequest, _ struct{}) (*gosdk.CallToolResult, any, error) {
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
		return &gosdk.CallToolResult{Content: []gosdk.Content{&gosdk.TextContent{Text: "done"}}}, nil, nil
	})
	gosdk.AddTool(s, &gosdk.Tool{Name: "exit"}, func(context.Context, *gosdk.CallToolRequest, struct{}) (*gosdk.CallToolResult, any, error) {
		os.Exit(3)
		return nil, nil, nil
	})
	gosdk.AddTool(s, &gosdk.Tool{Name: "ask"}, askColor)
	return s
}

var colorSchema = map[string]any{
	"type": "object", "required": []string{"color"},
	"properties": map[string]any{"color": map[string]any{"type": "string", "default": "blue"}},
}

func askColor(ctx context.Context, r *gosdk.CallToolRequest, _ struct{}) (*gosdk.CallToolResult, any, error) {
	text := func(action string, content map[string]any) *gosdk.CallToolResult {
		return &gosdk.CallToolResult{Content: []gosdk.Content{&gosdk.TextContent{Text: fmt.Sprintf("%s:%v", action, content["color"])}}}
	}
	if resp, ok := r.Params.InputResponses["c"]; ok {
		er := resp.(*gosdk.ElicitResult)
		return text(er.Action+"/mrtr", er.Content), nil, nil
	}
	if p := r.Session.InitializeParams(); p == nil || p.ProtocolVersion >= ProtocolVersion {
		return &gosdk.CallToolResult{
			InputRequests: gosdk.InputRequestMap{"c": &gosdk.ElicitParams{Message: "color?", RequestedSchema: colorSchema}},
			RequestState:  "s1",
		}, nil, nil
	}
	res, err := r.Session.Elicit(ctx, &gosdk.ElicitParams{Message: "color?", RequestedSchema: colorSchema})
	if err != nil {
		return nil, nil, err
	}
	return text(res.Action, res.Content), nil, nil
}

// serveSilentLegacy is a handshake-era stdio server that ignores requests it
// does not know, server/discover among them.
func serveSilentLegacy() {
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var m JSONRPCMessage
		if json.Unmarshal(in.Bytes(), &m) != nil || m.ID == nil {
			continue
		}
		var result string
		switch m.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"silent","version":"1"}}`
		case "tools/list":
			result = `{"tools":[{"name":"t","inputSchema":{"type":"object"}}]}`
		default:
			continue
		}
		id, _ := json.Marshal(m.ID)
		fmt.Printf(`{"jsonrpc":"2.0","id":%s,"result":%s}`+"\n", id, result)
	}
}

func acceptOffered(context.Context, string, ElicitRequest) (ElicitResult, error) {
	return ElicitResult{Action: ElicitActionAccept}, nil
}

func testOptions() ClientOptions {
	opts := DefaultClientOptions()
	opts.ElicitationHandler = acceptOffered
	opts.EnableGracefulDegradation = false
	return opts
}

func stdioConfig(t *testing.T, mode string) ServerConfig {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return ServerConfig{Name: "s", Command: exe, Args: []string{"-test.run=^$"}, Env: map[string]string{envTestServer: mode}}
}

func serveStreamable(t *testing.T, stateless bool) string {
	t.Helper()
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(
		func(*http.Request) *gosdk.Server { return goSDKTestServer(nil) }, &gosdk.StreamableHTTPOptions{Stateless: stateless}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func serveSSE(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(gosdk.NewSSEHandler(func(*http.Request) *gosdk.Server { return goSDKTestServer(nil) }, nil))
	t.Cleanup(srv.Close)
	return srv.URL
}

func firstText(t *testing.T, res *ToolCallResponse) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	return res.Content[0].Text
}

func TestClient_StatelessStreamableHTTP(t *testing.T) {
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: serveStreamable(t, true)}, testOptions())
	defer c.Close()

	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, info.ProtocolVersion, "a stateless server is reached with the stateless protocol")
	assert.Equal(t, "ref", info.ServerInfo.Name)

	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	byName := map[string]Tool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	require.Contains(t, byName, "add")
	assert.Contains(t, string(byName["add"].OutputSchema), "total", "outputSchema is carried")

	res, err := c.CallTool(context.Background(), "add", json.RawMessage(`{"a":2,"b":3}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"total":5}`, string(res.StructuredContent), "structuredContent is carried (#2099)")

	res, err = c.CallTool(context.Background(), "boom", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Contains(t, firstText(t, res), "kaboom")

	_, err = c.CallTool(context.Background(), "nope", nil)
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr, "a server's JSON-RPC error surfaces as *RPCError")
	assert.NotZero(t, rpcErr.Code)
}

func TestClient_MultiRoundTripInputIsAnswered(t *testing.T) {
	// 2026-07-28: the server answers input_required; the client asks the
	// host, then retries with the answers and the request state.
	var asked ElicitRequest
	opts := testOptions()
	opts.ElicitationHandler = func(_ context.Context, server string, req ElicitRequest) (ElicitResult, error) {
		asked = req
		assert.Equal(t, "s", server)
		return ElicitResult{Action: ElicitActionAccept, Content: json.RawMessage(`{"color":"green"}`)}, nil
	}
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: serveStreamable(t, true)}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	res, err := c.CallTool(context.Background(), "ask", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "accept/mrtr:green", firstText(t, res))
	assert.Equal(t, "color?", asked.Message)
	assert.Equal(t, ElicitModeForm, asked.Mode)
}

func TestClient_SSEOutlivesTheInitializeContext(t *testing.T) {
	// go-sdk binds the SSE event stream to the context given to Connect.
	c := NewSSEClientWithOptions(ServerConfig{Name: "s", URL: serveSSE(t)}, testOptions())
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	_, err := c.Initialize(ctx)
	require.NoError(t, err)
	cancel()

	res, err := c.CallTool(context.Background(), "add", json.RawMessage(`{"a":1,"b":1}`))
	require.NoError(t, err, "the stream survives the end of Initialize's context")
	assert.JSONEq(t, `{"total":2}`, string(res.StructuredContent))
}

func TestClient_URLOnlyFallsBackToSSEAndElicitsWithDefaults(t *testing.T) {
	reg := NewRegistryWithOptions(RegistryOptions{ConfigureClient: func(_ ServerConfig, o *ClientOptions) {
		*o = testOptions()
	}})
	defer reg.Close()
	require.NoError(t, reg.RegisterServer(ServerConfig{Name: "s", URL: serveSSE(t)}))
	c, err := reg.GetClient(context.Background(), "s")
	require.NoError(t, err)
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, LegacyProtocolVersion, info.ProtocolVersion, "an SSE server is handshake-era")

	// Accepting as offered takes the schema's defaults (SEP-1034); go-sdk
	// v1.8.0 skips them for an empty answer and validates first.
	res, err := c.CallTool(context.Background(), "ask", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "accept:blue", firstText(t, res))
}

func TestClient_SSEBaseURLStillReachesTheStreamAtSlashSSE(t *testing.T) {
	// Earlier releases appended /sse to the configured URL.
	mux := http.NewServeMux()
	mux.Handle("/sse", gosdk.NewSSEHandler(func(*http.Request) *gosdk.Server { return goSDKTestServer(nil) }, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewSSEClientWithOptions(ServerConfig{Name: "s", URL: srv.URL}, testOptions())
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	res, err := c.CallTool(context.Background(), "add", json.RawMessage(`{"a":1,"b":2}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"total":3}`, string(res.StructuredContent))
}

func TestClient_Stdio(t *testing.T) {
	c := NewStdioClientWithOptions(stdioConfig(t, "gosdk"), testOptions())
	assert.False(t, c.IsAlive())
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, ProtocolVersion, info.ProtocolVersion)
	again, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Same(t, info, again, "Initialize is idempotent")
	assert.True(t, c.IsAlive())

	res, err := c.CallTool(context.Background(), "add", json.RawMessage(`{"a":20,"b":22}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"total":42}`, string(res.StructuredContent))

	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "Close is idempotent")
	assert.False(t, c.IsAlive())
	_, err = c.CallTool(context.Background(), "add", nil)
	assert.ErrorIs(t, err, ErrClientClosed)
	_, err = c.Initialize(context.Background())
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestClient_StdioServerSilentOnDiscoverGetsTheHandshake(t *testing.T) {
	// go-sdk waits for a server/discover answer that never comes; the client
	// restarts such a server with the initialize handshake.
	opts := testOptions()
	opts.EraProbeTimeout = 300 * time.Millisecond
	c := NewStdioClientWithOptions(stdioConfig(t, "silent"), opts)
	defer c.Close()
	start := time.Now()
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, LegacyProtocolVersion, info.ProtocolVersion)
	assert.Less(t, time.Since(start), 5*time.Second)
	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, "t", tools[0].Name)
}

func TestClient_StdioReconnectsAfterTheProcessDies(t *testing.T) {
	c := NewStdioClientWithOptions(stdioConfig(t, "gosdk"), testOptions())
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	_, err = c.CallTool(context.Background(), "exit", nil)
	require.Error(t, err, "the server died mid-call")
	require.Eventually(t, func() bool { return !c.IsAlive() }, 5*time.Second, 10*time.Millisecond)

	res, err := c.CallTool(context.Background(), "add", json.RawMessage(`{"a":1,"b":2}`))
	require.NoError(t, err, "the next call reconnects")
	assert.JSONEq(t, `{"total":3}`, string(res.StructuredContent))
	assert.True(t, c.IsAlive())

	opts := testOptions()
	opts.MaxReconnectAttempts = 0
	once := NewStdioClientWithOptions(stdioConfig(t, "gosdk"), opts)
	defer once.Close()
	_, err = once.Initialize(context.Background())
	require.NoError(t, err)
	_, _ = once.CallTool(context.Background(), "exit", nil)
	require.Eventually(t, func() bool { return !once.IsAlive() }, 5*time.Second, 10*time.Millisecond)
	_, err = once.CallTool(context.Background(), "add", nil)
	assert.ErrorIs(t, err, ErrProcessDied, "without reconnection a dead server stays dead")
}

func TestClient_RequestTimeoutPausesWhileTheUserAnswers(t *testing.T) {
	// A handshake-era server asks mid-call; the user's time to answer does
	// not count against RequestTimeout, but a slow server still times out.
	opts := testOptions()
	opts.RequestTimeout = 150 * time.Millisecond
	opts.ElicitationHandler = func(context.Context, string, ElicitRequest) (ElicitResult, error) {
		time.Sleep(450 * time.Millisecond)
		return ElicitResult{Action: ElicitActionAccept, Content: json.RawMessage(`{"color":"red"}`)}, nil
	}
	c := NewSSEClientWithOptions(ServerConfig{Name: "s", URL: serveSSE(t)}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	res, err := c.CallTool(context.Background(), "ask", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "accept:red", firstText(t, res))

	_, err = c.CallTool(context.Background(), "slow", json.RawMessage(`{}`))
	assert.ErrorIs(t, err, ErrServerUnresponsive)
}

func TestClient_NotInitializedAndInitTimeout(t *testing.T) {
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: "http://127.0.0.1:1"}, testOptions())
	_, err := c.ListTools(context.Background())
	assert.ErrorIs(t, err, ErrClientNotInitialized)

	// go-sdk leaves the abandoned request running; the handler gives up.
	hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer hang.Close()
	opts := testOptions()
	opts.InitTimeout = 100 * time.Millisecond
	opts.MaxRetries = 0
	slow := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: hang.URL}, opts)
	_, err = slow.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialization timeout")
}

func TestClient_ConnectingIsRetried(t *testing.T) {
	opts := testOptions()
	opts.MaxRetries = 2
	opts.RetryDelay = time.Millisecond
	c := NewStdioClientWithOptions(ServerConfig{Name: "s", Command: "/nonexistent/mcp-server"}, opts)
	_, err := c.Initialize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/mcp-server")
}

func TestClient_SendsHeadersAndTheAuthorizersCredentials(t *testing.T) {
	var mu sync.Mutex
	var seen []http.Header
	inner := gosdk.NewStreamableHTTPHandler(func(*http.Request) *gosdk.Server { return goSDKTestServer(nil) },
		&gosdk.StreamableHTTPOptions{Stateless: true})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Clone())
		mu.Unlock()
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()
	opts := testOptions()
	opts.Authorizer = &recordingAuthorizer{token: 1}
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: srv.URL, Headers: map[string]string{"X-Tenant": "t1"}}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, seen)
	for _, h := range seen {
		assert.Equal(t, "t1", h.Get("X-Tenant"))
		assert.Equal(t, "Bearer token-1", h.Get("Authorization"), "every request goes through the Authorizer")
	}
}

// legacyJSONServer is a minimal handshake-era Streamable HTTP server whose
// tools/list answer is given, for wire shapes go-sdk's server cannot produce.
func legacyJSONServer(t *testing.T, toolsList string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var m JSONRPCMessage
		_ = json.NewDecoder(r.Body).Decode(&m)
		if m.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result string
		switch m.Method {
		case "initialize":
			result = `{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"raw","version":"1"}}`
		case "tools/list":
			result = toolsList
		default:
			w.Header().Set("Content-Type", "application/json")
			id, _ := json.Marshal(m.ID)
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"no"}}`, id)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		id, _ := json.Marshal(m.ID)
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestClient_ToolsThatMustRunAsTasksAreLeftOut(t *testing.T) {
	// 2025-11-25: such a tool can only be called as a task, which the
	// client does not implement. go-sdk v1.8.0 drops the field, so this
	// documents the gap until it carries it.
	url := legacyJSONServer(t, `{"tools":[`+
		`{"name":"plain","inputSchema":{"type":"object"}},`+
		`{"name":"tasked","inputSchema":{"type":"object"},"execution":{"taskSupport":"required"}}]}`)
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: url}, testOptions())
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "plain")
}

func TestClient_GracefulDegradation(t *testing.T) {
	url := legacyJSONServer(t, `"not a list"`)
	opts := testOptions()
	strict := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: url}, opts)
	defer strict.Close()
	_, err := strict.Initialize(context.Background())
	require.NoError(t, err)
	_, err = strict.ListTools(context.Background())
	require.Error(t, err)

	opts.EnableGracefulDegradation = true
	lenient := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: url}, opts)
	defer lenient.Close()
	_, err = lenient.Initialize(context.Background())
	require.NoError(t, err)
	tools, err := lenient.ListTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestClient_ListToolsFollowsPagination(t *testing.T) {
	srv := httptest.NewServer(gosdk.NewStreamableHTTPHandler(
		func(*http.Request) *gosdk.Server { return goSDKTestServer(&gosdk.ServerOptions{PageSize: 1}) },
		&gosdk.StreamableHTTPOptions{Stateless: true}))
	defer srv.Close()
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: srv.URL}, testOptions())
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)
	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 5, "every page is fetched")
}

func TestAnswerElicitation(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"color":{"type":"string","default":"blue"},"n":{"type":"integer"}}}`)
	reply := func(res ElicitResult, err error) ElicitationHandler {
		return func(context.Context, string, ElicitRequest) (ElicitResult, error) { return res, err }
	}

	res, err := answerElicitation(context.Background(), reply(ElicitResult{Action: ElicitActionAccept,
		Content: json.RawMessage(`{"n":3}`)}, nil), "s", ElicitRequest{Message: "x", RequestedSchema: schema})
	require.NoError(t, err)
	assert.JSONEq(t, `{"n":3,"color":"blue"}`, string(res.Content), "defaults fill what the answer omits")

	res, err = answerElicitation(context.Background(), reply(ElicitResult{Action: ElicitActionDecline,
		Content: json.RawMessage(`{"leak":true}`)}, nil), "s", ElicitRequest{RequestedSchema: schema})
	require.NoError(t, err)
	assert.Empty(t, res.Content, "only an accepted form carries content")

	_, err = answerElicitation(context.Background(), reply(ElicitResult{}, nil), "s",
		ElicitRequest{Mode: ElicitModeURL, URL: "https://x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported elicitation mode", "url mode is not advertised")

	_, err = answerElicitation(context.Background(), reply(ElicitResult{Action: "maybe"}, nil), "s", ElicitRequest{})
	assert.ErrorContains(t, err, `"maybe"`)

	_, err = answerElicitation(context.Background(), reply(ElicitResult{}, errors.New("no user")), "s", ElicitRequest{})
	assert.ErrorContains(t, err, "no user")
}

func TestClient_ElicitationIsAdvertisedOnlyWithAHandler(t *testing.T) {
	// Advertising elicitation invites servers to ask; without a host handler
	// nothing could answer.
	//
	// The capabilities travel in the initialize request, so they are read
	// there: it completes before Initialize returns. Reading them from the
	// server's InitializedHandler waited on the later notification, which the
	// test's go-sdk SSE server occasionally did not handle within 5s under a
	// loaded CI run, although the client had sent it (#2189).
	advertised := func(opts ClientOptions) string {
		var caps atomic.Value
		srv := httptest.NewServer(gosdk.NewSSEHandler(func(*http.Request) *gosdk.Server {
			s := goSDKTestServer(nil)
			s.AddReceivingMiddleware(func(next gosdk.MethodHandler) gosdk.MethodHandler {
				return func(ctx context.Context, method string, req gosdk.Request) (gosdk.Result, error) {
					if p, ok := req.GetParams().(*gosdk.InitializeParams); ok && method == "initialize" {
						raw, _ := json.Marshal(p.Capabilities)
						caps.Store(string(raw))
					}
					return next(ctx, method, req)
				}
			})
			return s
		}, nil))
		defer srv.Close()
		c := NewSSEClientWithOptions(ServerConfig{Name: "s", URL: srv.URL}, opts)
		defer c.Close()
		_, err := c.Initialize(context.Background())
		require.NoError(t, err)
		got, ok := caps.Load().(string)
		require.True(t, ok, "the server saw no initialize request")
		return got
	}
	assert.NotContains(t, advertised(DefaultClientOptions()), "elicitation")
	assert.Contains(t, advertised(testOptions()), `"elicitation"`)
}

func TestNewClientAdapter_PicksTheTransport(t *testing.T) {
	for _, tc := range []struct {
		cfg  ServerConfig
		want Transport
	}{
		{ServerConfig{URL: "http://x"}, transportAuto},
		{ServerConfig{URL: "http://x", TransportName: TransportSSE}, TransportSSE},
		{ServerConfig{URL: "http://x", TransportName: TransportStreamableHTTP}, TransportStreamableHTTP},
		{ServerConfig{Command: "srv"}, TransportStdio},
		// Neither set: validation upstream prevents it; Initialize fails
		// clearly, as for a stdio server that cannot start.
		{ServerConfig{}, TransportUnknown},
	} {
		c := newClientAdapter(&tc.cfg, nil).(*sdkClient)
		assert.Equal(t, tc.want, c.transport, "%+v", tc.cfg)
	}
}

// countingRefuser refuses every request, counting how often it is asked.
type countingRefuser struct{ calls atomic.Int32 }

func (a *countingRefuser) Authorize(context.Context, *http.Request) error {
	a.calls.Add(1)
	return errors.New("consent declined")
}

func (a *countingRefuser) Challenge(context.Context, *AuthChallenge) error { return nil }

func TestClient_RefusedAuthorizationIsNotRetried(t *testing.T) {
	auth := &countingRefuser{}
	opts := testOptions()
	opts.MaxRetries = 3
	opts.RetryDelay = time.Millisecond
	opts.Authorizer = auth
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: serveStreamable(t, true)}, opts)
	_, err := c.Initialize(context.Background())
	var authErr *AuthError
	require.ErrorAs(t, err, &authErr)
	assert.Contains(t, err.Error(), "consent declined")
	// One connect: server/discover, and the handshake it falls back to.
	assert.LessOrEqual(t, auth.calls.Load(), int32(2), "a refusal is not retried")
}

func TestConstructors_FixTheTransportAndDefaults(t *testing.T) {
	cfg := ServerConfig{Name: "s", URL: "http://x", Command: "srv"}
	for _, tc := range []struct {
		c    *sdkClient
		want Transport
	}{
		{NewStdioClient(cfg).sdkClient, TransportStdio},
		{NewStreamableClient(cfg).sdkClient, TransportStreamableHTTP},
		{NewSSEClient(cfg).sdkClient, TransportSSE},
	} {
		assert.Equal(t, tc.want, tc.c.transport)
		assert.Equal(t, DefaultClientOptions(), tc.c.options)
	}
}

func TestRPCError_Error(t *testing.T) {
	assert.Equal(t, "JSON-RPC error -32602: bad", (&RPCError{Code: -32602, Message: "bad"}).Error())
	assert.Equal(t, `JSON-RPC error -32602: bad (data: {"field":"a"})`,
		(&RPCError{Code: -32602, Message: "bad", Data: json.RawMessage(`{"field":"a"}`)}).Error())
}

// HTTP+SSE is a handshake-era transport, so the client never offers it
// 2026-07-28: no server/discover, and the initialize handshake every time.
// Offering it let go-sdk's SSE server answer discover with 2026-07-28 when the
// request beat the server's own record of what the transport supports, and
// the session then ran without a handshake (#2189).
func TestClient_SSEAlwaysUsesTheHandshake(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	sse := gosdk.NewSSEHandler(func(*http.Request) *gosdk.Server { return goSDKTestServer(nil) }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var msg struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &msg)
			mu.Lock()
			methods = append(methods, msg.Method)
			mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		sse.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := NewSSEClientWithOptions(ServerConfig{Name: "s", URL: srv.URL}, testOptions())
	defer c.Close()
	info, err := c.Initialize(context.Background())
	require.NoError(t, err)
	assert.Equal(t, LegacyProtocolVersion, info.ProtocolVersion)

	mu.Lock()
	defer mu.Unlock()
	assert.NotContains(t, methods, "server/discover")
	assert.Contains(t, methods, "initialize")
	assert.Contains(t, methods, "notifications/initialized")
}
