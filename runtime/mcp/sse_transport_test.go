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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingRequests_RegisterAndDeliver(t *testing.T) {
	pr := newPendingRequests()
	id := int64(7)
	ch := pr.register(id)
	require.NotNil(t, ch)

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
	id := int64(3)
	pr.register(id)
	pr.cancel(id)
	// Delivery after cancel is a no-op.
	pr.deliver(id, &JSONRPCMessage{ID: id, Result: json.RawMessage(`"late"`)})
}

func TestPendingRequests_FailAllClosesWaitersAndRefusesNew(t *testing.T) {
	pr := newPendingRequests()
	ch := pr.register(1)
	pr.failAll()

	_, open := <-ch
	assert.False(t, open, "a waiter must see its channel closed when the stream ends")
	assert.Nil(t, pr.register(2), "no request can wait on a stream that has ended")
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

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions(), nil)
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

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions(), nil)
	defer tr.close()
	err := tr.connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected endpoint event")
}

func TestSSETransport_ResolveMessageURL_Absolute(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:8080"}, DefaultClientOptions(), nil)
	defer tr.close()
	got, err := tr.resolveMessageURL("https://other.host/xyz")
	require.NoError(t, err)
	assert.Equal(t, "https://other.host/xyz", got)
}

func TestSSETransport_ResolveMessageURL_Relative(t *testing.T) {
	// The endpoint event's URI is relative to the stream it arrived on.
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:8080/mcp/sse"}, DefaultClientOptions(), nil)
	defer tr.close()
	tr.streamURL = "http://localhost:8080/mcp/sse"
	got, err := tr.resolveMessageURL("/message?sessionID=abc")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/message?sessionID=abc", got)
	got, err = tr.resolveMessageURL("message?sessionID=abc")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080/mcp/message?sessionID=abc", got)
}

func TestSSETransport_OpensTheConfiguredURLFirst(t *testing.T) {
	// The configured URL is the SSE endpoint; servers that host it anywhere
	// but <base>/sse (the TypeScript SDK's examples among them) failed.
	var gets []string
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		gets = append(gets, r.URL.Path)
		w.Header().Set("Content-Type", contentTypeSSE)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: post\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gets = append(gets, r.URL.Path)
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL + "/events"}, DefaultClientOptions(), nil)
	defer tr.close()
	require.NoError(t, tr.connect(context.Background()))
	assert.Equal(t, []string{"/events"}, gets, "no /sse suffix is appended to a URL that serves the stream")
	assert.Equal(t, srv.URL+"/post", tr.messageURL)
}

func TestSSETransport_FallsBackToTheLegacySSESuffix(t *testing.T) {
	// Configs written for earlier releases name the base URL.
	var gets []string
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		gets = append(gets, r.URL.Path)
		w.Header().Set("Content-Type", contentTypeSSE)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /message\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gets = append(gets, r.URL.Path)
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions(), nil)
	defer tr.close()
	require.NoError(t, tr.connect(context.Background()))
	assert.Equal(t, []string{"/", "/sse"}, gets)

	other := newSSETransport(ServerConfig{Name: "y", URL: srv.URL + "/nothing"}, DefaultClientOptions(), nil)
	defer other.close()
	err := other.connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nothing/sse status 404", "the last endpoint's failure is reported")
}

func TestSSETransport_SendRequest_NotConnected(t *testing.T) {
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://localhost:0"}, DefaultClientOptions(), nil)
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
	tr := newSSETransport(ServerConfig{Name: "x", URL: "http://x"}, DefaultClientOptions(), nil)
	tr.close()
	tr.close() // must not panic
	assert.False(t, tr.alive.Load())
}

func TestSSETransport_ALandingPageAtTheBaseURLIsNotTheStream(t *testing.T) {
	// Earlier releases always opened <url>/sse; a base URL that serves a
	// page must still reach the stream there.
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(headerContentType, contentTypeSSE)
		_, _ = fmt.Fprint(w, "event: endpoint\ndata: /message\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerContentType, "text/html")
		_, _ = fmt.Fprint(w, "<html>welcome</html>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := newSSETransport(ServerConfig{Name: "x", URL: srv.URL}, DefaultClientOptions(), nil)
	defer tr.close()
	require.NoError(t, tr.connect(context.Background()))
	assert.Equal(t, srv.URL+"/sse", tr.streamURL)

	page := newSSETransport(ServerConfig{Name: "y", URL: srv.URL + "/page/sse"}, DefaultClientOptions(), nil)
	defer page.close()
	err := page.connect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `returned "text/html", not an event stream`)
}
