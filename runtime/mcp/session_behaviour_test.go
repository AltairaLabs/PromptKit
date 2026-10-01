package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Behavioural tests for the protocol layer, one per conformance gap it
// closes (#2100). Each drives a client against a fake server over a real
// transport and checks what crossed the wire.

// stdioPeer is the server end of a piped StdioClient.
type stdioPeer struct {
	t      *testing.T
	client *StdioClient
	in     *bufio.Scanner // messages the client wrote
	out    io.WriteCloser // write here to send to the client
}

func newStdioPeer(t *testing.T, opts ClientOptions) *stdioPeer {
	t.Helper()
	client, serverReader, serverWriter := newTestClientWithPipes(t, opts)
	client.wg.Add(1)
	go client.readLoop(client.stdout)
	scanner := bufio.NewScanner(serverReader)
	scanner.Buffer(make([]byte, 0, 1<<16), 8<<20)
	t.Cleanup(func() {
		_ = serverReader.Close()
		_ = serverWriter.Close()
		_ = client.Close()
	})
	return &stdioPeer{t: t, client: client, in: scanner, out: serverWriter}
}

func (p *stdioPeer) send(msg string) {
	p.t.Helper()
	_, err := p.out.Write([]byte(msg + "\n"))
	require.NoError(p.t, err)
}

// next returns the next message the client wrote.
func (p *stdioPeer) next() JSONRPCMessage {
	p.t.Helper()
	got := make(chan JSONRPCMessage, 1)
	go func() {
		if p.in.Scan() {
			var msg JSONRPCMessage
			_ = json.Unmarshal(p.in.Bytes(), &msg)
			got <- msg
		}
	}()
	select {
	case msg := <-got:
		return msg
	case <-time.After(2 * time.Second):
		p.t.Fatal("client wrote nothing")
		return JSONRPCMessage{}
	}
}

func TestStdio_AnswersPingAndRefusesUnadvertisedServerRequests(t *testing.T) {
	// A server request nobody answers leaves the server waiting forever
	// (2025-06-18 basic/utilities/ping: the receiver MUST respond promptly).
	p := newStdioPeer(t, DefaultClientOptions())

	p.send(`{"jsonrpc":"2.0","id":"srv-1","method":"ping"}`)
	pong := p.next()
	assert.Equal(t, "srv-1", pong.ID)
	assert.Nil(t, pong.Error)
	assert.JSONEq(t, `{}`, string(pong.Result))

	p.send(`{"jsonrpc":"2.0","id":"srv-2","method":"elicitation/create","params":{"message":"name?"}}`)
	refused := p.next()
	assert.Equal(t, "srv-2", refused.ID)
	require.NotNil(t, refused.Error, "an unadvertised capability's request is refused, not ignored")
	assert.Equal(t, codeMethodNotFound, refused.Error.Code)
}

func TestStdio_ServerRequestWithCollidingIDDoesNotTakeTheResponse(t *testing.T) {
	// The server's request ids are its own; one can equal a pending client
	// request's id. Only a message without a method is a response.
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	var resp ToolCallResponse
	go func() {
		done <- p.client.sendRequest(context.Background(), "tools/call", ToolCallRequest{Name: "x"}, &resp)
	}()
	req := p.next()
	require.Equal(t, "tools/call", req.Method)

	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"method":"ping"}`, req.ID))
	pong := p.next()
	assert.Equal(t, req.ID, pong.ID, "the ping is answered with its own id")
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"content":[{"type":"text","text":"real"}]}}`, req.ID))

	require.NoError(t, <-done)
	require.Len(t, resp.Content, 1)
	assert.Equal(t, "real", resp.Content[0].Text)
}

func TestStdio_ReadsMessagesLargerThanOneMegabyte(t *testing.T) {
	// The reader used to stop at 1 MB and never recover.
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	var resp ToolCallResponse
	go func() {
		done <- p.client.sendRequest(context.Background(), "tools/call", ToolCallRequest{Name: "x"}, &resp)
	}()
	req := p.next()
	big := strings.Repeat("a", 3<<20)
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"content":[{"type":"text","text":%q}]}}`, req.ID, big))

	require.NoError(t, <-done)
	require.Len(t, resp.Content, 1)
	assert.Len(t, resp.Content[0].Text, 3<<20)
	assert.True(t, p.client.IsAlive())
}

func TestStdio_ClosedStdoutFailsPendingRequestsAndReportsNotAlive(t *testing.T) {
	opts := DefaultClientOptions()
	opts.RequestTimeout = 10 * time.Second
	p := newStdioPeer(t, opts)

	done := make(chan error, 1)
	go func() { done <- p.client.sendRequest(context.Background(), "tools/list", nil, nil) }()
	p.next()
	require.NoError(t, p.out.Close())

	select {
	case err := <-done:
		require.Error(t, err, "a request outstanding when the server goes away fails at once")
	case <-time.After(2 * time.Second):
		t.Fatal("pending request still waiting after stdout closed")
	}
	assert.False(t, p.client.IsAlive())
}

func TestSession_ListToolsFollowsPagination(t *testing.T) {
	// 2025-06-18 server/utilities/pagination: the client requests the next
	// page with the nextCursor it was given until none is returned.
	p := newStdioPeer(t, DefaultClientOptions())
	p.client.sess.version = ProtocolVersion

	done := make(chan []Tool, 1)
	go func() {
		tools, err := p.client.sess.listTools(context.Background())
		assert.NoError(t, err)
		done <- tools
	}()

	first := p.next()
	assert.Empty(t, string(first.Params), "the first page is requested without a cursor")
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"tools":[{"name":"a","inputSchema":{}}],"nextCursor":"p2"}}`,
		first.ID))
	second := p.next()
	assert.JSONEq(t, `{"cursor":"p2"}`, string(second.Params))
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"tools":[{"name":"b","inputSchema":{}}]}}`, second.ID))

	tools := <-done
	require.Len(t, tools, 2)
	assert.Equal(t, "a", tools[0].Name)
	assert.Equal(t, "b", tools[1].Name)
}

func TestSession_ListToolsRejectsARepeatedCursor(t *testing.T) {
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.listTools(context.Background())
		done <- err
	}()
	for range 2 {
		req := p.next()
		p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"tools":[],"nextCursor":"same"}}`, req.ID))
	}
	err := <-done
	require.Error(t, err)
	assert.Contains(t, err.Error(), `repeated tools/list cursor "same"`)
}

func TestSession_RPCErrorKeepsData(t *testing.T) {
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	go func() { done <- p.client.sendRequest(context.Background(), "tools/call", nil, nil) }()
	req := p.next()
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"error":{"code":-32602,"message":"bad","data":{"field":"a"}}}`, req.ID))

	err := <-done
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32602, rpcErr.Code)
	assert.JSONEq(t, `{"field":"a"}`, string(rpcErr.Data))
	assert.Contains(t, err.Error(), `"field":"a"`)
}

func TestSession_AdvertisesOnlyWhatItImplements(t *testing.T) {
	// Advertising elicitation invites servers to send elicitation/create,
	// which nothing answers; the client used to advertise it anyway.
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.initialize(context.Background())
		done <- err
	}()
	init := p.next()
	require.Equal(t, methodInitialize, init.Method)
	var params InitializeRequest
	require.NoError(t, json.Unmarshal(init.Params, &params))
	assert.Equal(t, ProtocolVersion, params.ProtocolVersion)
	raw, _ := json.Marshal(params.Capabilities)
	assert.JSONEq(t, `{}`, string(raw))
	assert.Equal(t, clientImplName, params.ClientInfo.Name)
	assert.NotEmpty(t, params.ClientInfo.Version)

	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"protocolVersion":"2025-03-26","capabilities":{},`+
		`"serverInfo":{"name":"s","version":"1"}}}`, init.ID))
	assert.Equal(t, methodNotificationsInitialized, p.next().Method)
	require.NoError(t, <-done)
	assert.Equal(t, "2025-03-26", p.client.sess.negotiatedVersion())
}

func TestSession_RejectsAnUnsupportedNegotiatedVersion(t *testing.T) {
	// 2025-06-18 basic/lifecycle: if the client does not support the version
	// the server answers with, it SHOULD disconnect.
	p := newStdioPeer(t, DefaultClientOptions())

	done := make(chan error, 1)
	go func() {
		_, err := p.client.sess.initialize(context.Background())
		done <- err
	}()
	init := p.next()
	p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"protocolVersion":"1999-01-01","capabilities":{},`+
		`"serverInfo":{"name":"s","version":"1"}}}`, init.ID))

	err := <-done
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"1999-01-01"`)
}

// streamableFake is a Streamable HTTP server driven by a handler per method.
type streamableFake struct {
	mu       sync.Mutex
	received []JSONRPCMessage
	headers  []http.Header
	deletes  []string
	handle   func(w http.ResponseWriter, r *http.Request, msg JSONRPCMessage)
}

func (f *streamableFake) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			f.mu.Lock()
			f.deletes = append(f.deletes, r.Header.Get(headerSessionID))
			f.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var msg JSONRPCMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		f.mu.Lock()
		f.received = append(f.received, msg)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		if msg.ID == nil && msg.Method != "" || isResponse(&msg) {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		f.handle(w, r, msg)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *streamableFake) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, m := range f.received {
		switch {
		case m.Method != "":
			out = append(out, m.Method)
		case m.Error != nil:
			out = append(out, fmt.Sprintf("error:%v", m.ID))
		default:
			out = append(out, fmt.Sprintf("result:%v", m.ID))
		}
	}
	return out
}

func writeJSONResult(w http.ResponseWriter, id any, result string) {
	w.Header().Set(headerContentType, contentTypeJSON)
	_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":%s}`, mustJSON(id), result)
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const initResult2025 = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"s","version":"1"}}`

func TestStreamable_ServerRequestOnTheResponseStreamIsAnsweredNotTakenAsTheResponse(t *testing.T) {
	// A server request on a request's SSE stream, reusing that request's id,
	// is answered by POST; the call returns the real response after it.
	f := &streamableFake{}
	f.handle = func(w http.ResponseWriter, _ *http.Request, msg JSONRPCMessage) {
		switch msg.Method {
		case methodInitialize:
			writeJSONResult(w, msg.ID, initResult2025)
		case methodToolsCall:
			w.Header().Set(headerContentType, contentTypeSSE)
			id := mustJSON(msg.ID)
			_, _ = fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"ping\"}\n\n", id)
			_, _ = fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,"+
				"\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"real\"}]}}\n\n", id)
		}
	}
	c := NewStreamableClient(ServerConfig{Name: "s", URL: f.serve(t), TransportName: TransportStreamableHTTP})
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	resp, err := c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	require.NoError(t, err)
	require.Len(t, resp.Content, 1)
	assert.Equal(t, "real", resp.Content[0].Text)
	f.mu.Lock()
	callID := f.received[2].ID
	f.mu.Unlock()
	assert.Contains(t, f.methods(), fmt.Sprintf("result:%v", callID),
		"the ping, sent with the call's own id, was answered with a POST of its result")
}

func TestStreamable_ReinitializesWhenTheSessionExpires(t *testing.T) {
	// 2025-06-18 basic/transports: a 404 for a request carrying a session id
	// means the session is gone; the client MUST start a new one.
	f := &streamableFake{}
	var mu sync.Mutex
	session := "s1"
	expired := false
	f.handle = func(w http.ResponseWriter, r *http.Request, msg JSONRPCMessage) {
		mu.Lock()
		defer mu.Unlock()
		switch msg.Method {
		case methodInitialize:
			if expired {
				session = "s2"
			}
			w.Header().Set(headerSessionID, session)
			writeJSONResult(w, msg.ID, initResult2025)
		case methodToolsList:
			if r.Header.Get(headerSessionID) != session || !expired {
				expired = true
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSONResult(w, msg.ID, `{"tools":[{"name":"t","inputSchema":{}}]}`)
		}
	}
	opts := DefaultClientOptions()
	opts.EnableGracefulDegradation = false
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: f.serve(t), TransportName: TransportStreamableHTTP}, opts)
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	tools, err := c.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 1)
	assert.Equal(t, []string{
		"initialize", "notifications/initialized", "tools/list",
		"initialize", "notifications/initialized", "tools/list",
	}, f.methods())
	f.mu.Lock()
	assert.Equal(t, "s2", f.headers[len(f.headers)-1].Get(headerSessionID), "the retry uses the new session")
	f.mu.Unlock()

	require.NoError(t, c.Close())
	assert.Equal(t, []string{"s2"}, f.deletes, "closing ends the session with DELETE")
}

func TestStreamable_JSONRPCErrorOnAnHTTPErrorStatusIsTheServersAnswer(t *testing.T) {
	f := &streamableFake{}
	f.handle = func(w http.ResponseWriter, _ *http.Request, msg JSONRPCMessage) {
		w.Header().Set(headerContentType, contentTypeJSON)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32602,"message":"bad params"}}`, mustJSON(msg.ID))
	}
	tr := newStreamableTransport(ServerConfig{Name: "s", URL: f.serve(t)}, DefaultClientOptions(), nil)
	err := tr.sendRequest(context.Background(), methodToolsList, nil, nil)
	var rpcErr *RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, -32602, rpcErr.Code)
}

func TestStreamable_RequestTimeoutAppliesAndCancels(t *testing.T) {
	// RequestTimeout used to reach only stdio; an HTTP server that never
	// answered hung the caller.
	release := make(chan struct{})
	f := &streamableFake{}
	f.handle = func(w http.ResponseWriter, r *http.Request, msg JSONRPCMessage) {
		if msg.Method == methodInitialize {
			writeJSONResult(w, msg.ID, initResult2025)
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	defer close(release)
	opts := DefaultClientOptions()
	opts.RequestTimeout = 100 * time.Millisecond
	c := NewStreamableClientWithOptions(ServerConfig{Name: "s", URL: f.serve(t), TransportName: TransportStreamableHTTP}, opts)
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	start := time.Now()
	_, err = c.CallTool(context.Background(), "slow", json.RawMessage(`{}`))
	require.ErrorIs(t, err, ErrServerUnresponsive)
	assert.Less(t, time.Since(start), 2*time.Second)
	require.Eventually(t, func() bool {
		return strings.Contains(strings.Join(f.methods(), ","), methodNotificationsCancel)
	}, 2*time.Second, 10*time.Millisecond, "the abandoned call is cancelled")
}

func TestSSE_ServerRequestWithCollidingIDIsAnsweredAndClosedStreamFailsPending(t *testing.T) {
	stream := make(chan string, 10)
	var mu sync.Mutex
	var posted []JSONRPCMessage
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, contentTypeSSE)
		flusher := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		flusher.Flush()
		for {
			select {
			case frame, ok := <-stream:
				if !ok {
					return
				}
				_, _ = fmt.Fprint(w, frame)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		var msg JSONRPCMessage
		_ = json.NewDecoder(r.Body).Decode(&msg)
		mu.Lock()
		posted = append(posted, msg)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		id := mustJSON(msg.ID)
		switch msg.Method {
		case methodInitialize:
			stream <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n\n", id, initResult2025)
		case methodToolsCall:
			stream <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"method\":\"ping\"}\n\n", id)
			stream <- fmt.Sprintf("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,"+
				"\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"real\"}]}}\n\n", id)
		case methodToolsList:
			close(stream) // the server drops the stream before answering
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewSSEClient(ServerConfig{Name: "s", URL: srv.URL})
	defer c.Close()
	_, err := c.Initialize(context.Background())
	require.NoError(t, err)

	resp, err := c.CallTool(context.Background(), "x", json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "real", resp.Content[0].Text)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range posted {
			if isResponse(&m) {
				return true
			}
		}
		return false
	}, 2*time.Second, 10*time.Millisecond, "the ping is answered by POST")

	done := make(chan error, 1)
	go func() {
		_, err := c.sess.listTools(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, errStreamClosed)
	case <-time.After(3 * time.Second):
		t.Fatal("request still pending after the stream closed")
	}
	assert.False(t, c.IsAlive())
}
