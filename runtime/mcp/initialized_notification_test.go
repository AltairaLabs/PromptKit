package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MCP lifecycle requires the client to send notifications/initialized
// after a successful initialize response. Some servers (e.g.
// @modelcontextprotocol/server-everything) register capability-conditional
// tools only once that notification arrives, so a client that skips it sees
// fewer tools. The fakes below model that: the "gated" tool is listed only
// after the notification has been received.

const (
	gatedTestHeader     = "X-Test-Auth"
	gatedTestHeaderVal  = "secret"
	gatedTestSessionID  = "sess-gated-1"
	methodInitializedNt = "notifications/initialized"
)

// gatedRecord is one message the gated fake received.
type gatedRecord struct {
	method    string
	hasID     bool
	sessionID string
	header    string
}

// gatedServerState records every message and gates one tool behind the
// initialized notification. Shared by the SSE and streamable fakes.
type gatedServerState struct {
	mu          sync.Mutex
	records     []gatedRecord
	initialized bool
	// rejectNotifications makes the streamable fake answer notifications 400.
	rejectNotifications bool
}

// observe decodes a raw JSON-RPC body, records it, and applies the
// notification's side effect. Returns the decoded message.
func (s *gatedServerState) observe(r *http.Request, body []byte) (JSONRPCMessage, bool) {
	var raw map[string]json.RawMessage
	var msg JSONRPCMessage
	if json.Unmarshal(body, &raw) != nil || json.Unmarshal(body, &msg) != nil {
		return msg, false
	}
	_, hasID := raw["id"]
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, gatedRecord{
		method:    msg.Method,
		hasID:     hasID,
		sessionID: r.Header.Get("Mcp-Session-Id"),
		header:    r.Header.Get(gatedTestHeader),
	})
	if !hasID && msg.Method == methodInitializedNt {
		s.initialized = true
	}
	return msg, hasID
}

// reply builds the JSON-RPC response for a request.
func (s *gatedServerState) reply(req JSONRPCMessage) JSONRPCMessage {
	resp := JSONRPCMessage{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case methodInitialize:
		resp.Result = json.RawMessage(`{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},` +
			`"serverInfo":{"name":"gated","version":"0.1"}}`)
	case methodToolsList:
		s.mu.Lock()
		initialized := s.initialized
		s.mu.Unlock()
		tools := `{"name":"always","inputSchema":{}}`
		if initialized {
			tools += `,{"name":"gated","inputSchema":{}}`
		}
		resp.Result = json.RawMessage(`{"tools":[` + tools + `]}`)
	default:
		resp.Error = &JSONRPCError{Code: -32601, Message: "method not found"}
	}
	return resp
}

func (s *gatedServerState) snapshot() []gatedRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]gatedRecord(nil), s.records...)
}

// gatedStreamableServer answers notifications with 202 and no body, as the
// Streamable HTTP transport specifies, and issues a session id on initialize.
func gatedStreamableServer(t *testing.T) (string, *gatedServerState) {
	t.Helper()
	state := &gatedServerState{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, hasID := state.observe(r, body)
		if !hasID {
			status := http.StatusAccepted
			if state.rejectNotifications {
				status = http.StatusBadRequest
			}
			w.WriteHeader(status)
			return
		}
		if msg.Method == methodInitialize {
			w.Header().Set("Mcp-Session-Id", gatedTestSessionID)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state.reply(msg))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp", state
}

// gatedSSEServer answers every POST with 202; responses to requests arrive on
// the SSE stream, and notifications produce no event at all.
func gatedSSEServer(t *testing.T) (string, *gatedServerState) {
	t.Helper()
	state := &gatedServerState{}
	events := make(chan []byte, 32)
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /message?sessionId=s1\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-events:
				_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				w.(http.Flusher).Flush()
			}
		}
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, hasID := state.observe(r, body)
		if !hasID && state.rejectNotifications {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		if hasID {
			b, _ := json.Marshal(state.reply(msg))
			events <- b
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, state
}

func toolNames(tools []Tool) []string {
	names := make([]string, 0, len(tools))
	for i := range tools {
		names = append(names, tools[i].Name)
	}
	return names
}

func TestStreamableClient_Initialize_SendsInitializedNotification(t *testing.T) {
	url, state := gatedStreamableServer(t)
	c := NewStreamableClient(ServerConfig{
		Name: "gated", URL: url, TransportName: TransportStreamableHTTP,
		Headers: map[string]string{gatedTestHeader: gatedTestHeaderVal},
	})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.Initialize(ctx)
	require.NoError(t, err)
	tools, err := c.ListTools(ctx)
	require.NoError(t, err)

	assert.Equal(t, []string{"always", "gated"}, toolNames(tools))
	recs := state.snapshot()
	require.Len(t, recs, 3)
	assert.Equal(t, []string{methodInitialize, methodInitializedNt, methodToolsList},
		[]string{recs[0].method, recs[1].method, recs[2].method})
	assert.False(t, recs[1].hasID, "a notification must not carry an id")
	assert.Equal(t, gatedTestSessionID, recs[1].sessionID, "notification must carry the session id")
	assert.Equal(t, gatedTestHeaderVal, recs[1].header, "notification must carry the configured headers")
}

func TestSSEClient_Initialize_SendsInitializedNotification(t *testing.T) {
	url, state := gatedSSEServer(t)
	c := NewSSEClient(ServerConfig{
		Name: "gated", URL: url,
		Headers: map[string]string{gatedTestHeader: gatedTestHeaderVal},
	})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.Initialize(ctx)
	require.NoError(t, err)
	tools, err := c.ListTools(ctx)
	require.NoError(t, err)

	assert.Equal(t, []string{"always", "gated"}, toolNames(tools))
	recs := state.snapshot()
	require.Len(t, recs, 3)
	assert.Equal(t, []string{methodInitialize, methodInitializedNt, methodToolsList},
		[]string{recs[0].method, recs[1].method, recs[2].method})
	assert.False(t, recs[1].hasID, "a notification must not carry an id")
	assert.Equal(t, gatedTestHeaderVal, recs[1].header, "notification must carry the configured headers")
}

// A rejected notification is logged and Initialize still succeeds, matching
// StdioClient; the notification was nonetheless sent in order.
func TestStreamableClient_Initialize_NotificationRejectedIsNonFatal(t *testing.T) {
	url, state := gatedStreamableServer(t)
	state.rejectNotifications = true
	c := NewStreamableClient(ServerConfig{Name: "gated", URL: url, TransportName: TransportStreamableHTTP})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.Equal(t, "gated", resp.ServerInfo.Name)
	assert.True(t, c.IsAlive())
	recs := state.snapshot()
	require.Len(t, recs, 2)
	assert.Equal(t, methodInitializedNt, recs[1].method)
}

// The SSE client treats a rejected notification the same way: logged, and
// Initialize still succeeds.
func TestSSEClient_Initialize_NotificationRejectedIsNonFatal(t *testing.T) {
	url, state := gatedSSEServer(t)
	state.rejectNotifications = true
	c := NewSSEClient(ServerConfig{Name: "gated", URL: url})
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := c.Initialize(ctx)
	require.NoError(t, err)
	assert.Equal(t, "gated", resp.ServerInfo.Name)
	recs := state.snapshot()
	require.Len(t, recs, 2)
	assert.Equal(t, methodInitializedNt, recs[1].method)
}

func TestStreamableTransport_SendNotification_RequestErrors(t *testing.T) {
	ctx := context.Background()

	tr := newStreamableTransport(ServerConfig{URL: statusServer(t, http.StatusAccepted)}, DefaultClientOptions(), nil)
	err := tr.sendNotification(ctx, methodInitializedNt, make(chan int))
	require.Error(t, err, "params that cannot be encoded must fail before sending")

	tr = newStreamableTransport(ServerConfig{URL: "http://[::1"}, DefaultClientOptions(), nil)
	require.Error(t, tr.sendNotification(ctx, methodInitializedNt, nil), "an invalid URL must fail to build a request")

	down := httptest.NewServer(http.NotFoundHandler())
	downURL := down.URL
	down.Close()
	tr = newStreamableTransport(ServerConfig{URL: downURL}, DefaultClientOptions(), nil)
	err = tr.sendNotification(ctx, methodInitializedNt, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mcp/streamable: POST:", "a transport failure must be reported as such")
}

// statusServer answers every POST with the given status and no body.
func statusServer(t *testing.T, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestStreamableTransport_SendNotification_StatusHandling(t *testing.T) {
	ctx := context.Background()
	tr := newStreamableTransport(ServerConfig{URL: statusServer(t, http.StatusAccepted)}, DefaultClientOptions(), nil)
	require.NoError(t, tr.sendNotification(ctx, methodInitializedNt, nil), "202 with no body is success")

	tr = newStreamableTransport(ServerConfig{URL: statusServer(t, http.StatusBadRequest)}, DefaultClientOptions(), nil)
	err := tr.sendNotification(ctx, methodInitializedNt, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")

	tr.close()
	assert.ErrorIs(t, tr.sendNotification(ctx, methodInitializedNt, nil), ErrClientClosed)
}

func TestSSETransport_SendNotification_StatusHandling(t *testing.T) {
	ctx := context.Background()
	tr := newSSETransport(ServerConfig{}, DefaultClientOptions(), nil)
	require.Error(t, tr.sendNotification(ctx, methodInitializedNt, nil), "unconnected transport must refuse")

	tr.messageURL = statusServer(t, http.StatusAccepted)
	require.NoError(t, tr.sendNotification(ctx, methodInitializedNt, nil), "202 with no body is success")

	tr.messageURL = statusServer(t, http.StatusBadRequest)
	err := tr.sendNotification(ctx, methodInitializedNt, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 400")
}
