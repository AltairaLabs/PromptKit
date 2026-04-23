package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingRequests_RegisterAndDeliver(t *testing.T) {
	pr := newPendingRequests()
	ch, id := pr.register()
	require.NotZero(t, id)

	go pr.deliver(id, &JSONRPCMessage{ID: id, Result: json.RawMessage(`"ok"`)})

	select {
	case msg := <-ch:
		gotID, ok := msg.ID.(int64)
		require.True(t, ok, "id type = %T", msg.ID)
		assert.Equal(t, id, gotID)
		assert.Equal(t, `"ok"`, string(msg.Result))
	case <-time.After(time.Second):
		t.Fatal("deliver did not forward the response")
	}
}

func TestPendingRequests_DeliverUnknownIDDrops(t *testing.T) {
	pr := newPendingRequests()
	// Must not panic.
	pr.deliver(999, &JSONRPCMessage{ID: int64(999)})
}

func TestPendingRequests_Cancel(t *testing.T) {
	pr := newPendingRequests()
	_, id := pr.register()
	pr.cancel(id)
	// Delivery after cancel is a no-op.
	pr.deliver(id, &JSONRPCMessage{ID: id, Result: json.RawMessage(`"late"`)})
}

func TestPendingRequests_UniqueIDs(t *testing.T) {
	pr := newPendingRequests()
	_, a := pr.register()
	_, b := pr.register()
	_, c := pr.register()
	assert.NotEqual(t, a, b)
	assert.NotEqual(t, b, c)
	assert.NotEqual(t, a, c)
}

func TestReadSSEEvent_EndpointFrame(t *testing.T) {
	raw := "event: endpoint\ndata: /message?sessionID=abc\n\n"
	ev, err := readSSEEvent(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	assert.Equal(t, "endpoint", ev.event)
	assert.Equal(t, "/message?sessionID=abc", ev.data)
}

func TestReadSSEEvent_MultilineData(t *testing.T) {
	raw := "event: message\ndata: line1\ndata: line2\n\n"
	ev, err := readSSEEvent(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	assert.Equal(t, "line1\nline2", ev.data)
}

func TestReadSSEEvent_CommentLinesIgnored(t *testing.T) {
	raw := ": keep-alive\nevent: message\ndata: hi\n\n"
	ev, err := readSSEEvent(bufio.NewReader(strings.NewReader(raw)))
	require.NoError(t, err)
	assert.Equal(t, "message", ev.event)
	assert.Equal(t, "hi", ev.data)
}

func TestReadSSEEvent_EOFBetweenFrames(t *testing.T) {
	_, err := readSSEEvent(bufio.NewReader(strings.NewReader("")))
	assert.ErrorIs(t, err, io.EOF)
}

func TestSSETransport_Connect_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions())
	defer tr.close()
	err := tr.connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 500")
}

func TestSSETransport_Connect_WrongFirstEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event: hello\ndata: world\n\n"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions())
	defer tr.close()
	err := tr.connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected endpoint event")
}

func TestSSETransport_ResolveMessageURL_Absolute(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:8080"}, DefaultClientOptions())
	defer tr.close()
	got, err := tr.resolveMessageURL("https://other.host/xyz")
	require.NoError(t, err)
	assert.Equal(t, "https://other.host/xyz", got)
}

func TestSSETransport_ResolveMessageURL_Relative(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:8080"}, DefaultClientOptions())
	defer tr.close()
	got, err := tr.resolveMessageURL("/message?sessionID=abc")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/message?sessionID=abc", got)
}

func TestSSETransport_SendRequest_NotConnected(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:0"}, DefaultClientOptions())
	defer tr.close()
	err := tr.sendRequest(context.Background(), "tools/list", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestCoerceID(t *testing.T) {
	tests := []struct {
		name   string
		in     interface{}
		wantID int64
		wantOK bool
	}{
		{"float64", float64(42), 42, true},
		{"int64", int64(43), 43, true},
		{"int", 44, 44, true},
		{"string ignored", "not-numeric", 0, false},
		{"nil ignored", nil, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, ok := coerceID(tc.in)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantID, id)
			}
		})
	}
}

func TestSSETransport_Close_Idempotent(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://x"}, DefaultClientOptions())
	tr.close()
	tr.close() // must not panic
	assert.False(t, tr.alive.Load())
}
