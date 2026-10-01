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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// jsonRPCVersion is the JSON-RPC envelope version used by all MCP messages.
const jsonRPCVersion = "2.0"

// sseEventMessage is the SSE event name carrying JSON-RPC payloads.
const sseEventMessage = "message"

// HTTP headers the transports set.
const (
	headerProtocolVersion = "MCP-Protocol-Version"
	headerSessionID       = "Mcp-Session-Id"
	headerContentType     = "Content-Type"
	headerAccept          = "Accept"
	contentTypeJSON       = "application/json"
	contentTypeSSE        = "text/event-stream"
)

// maxErrorBodyBytes bounds how much of a non-2xx body is read for a
// JSON-RPC error or a diagnostic.
const maxErrorBodyBytes = 64 << 10

// sessionDeleteTimeout bounds the best-effort DELETE that ends a session.
const sessionDeleteTimeout = 5 * time.Second

// streamableTransport implements the Streamable HTTP transport.
//
// One POST per JSON-RPC message; a request's response comes back on the same
// POST as either application/json or an SSE stream scoped to that request.
// On the stream the server may send notifications and (before 2026-07-28)
// requests of its own before the response; those go to the session, and the
// session's answers are POSTed back.
type streamableTransport struct {
	config  ServerConfig
	options ClientOptions
	in      inbound

	httpClient *http.Client
	url        string

	mu        sync.Mutex
	sessionID string // from Mcp-Session-Id on the initialize response
	// protocolVersion is the MCP-Protocol-Version the session last sent,
	// repeated on messages the transport originates (replies, DELETE).
	protocolVersion string

	closed atomic.Bool
	alive  atomic.Bool
}

// newStreamableTransport constructs a Streamable HTTP transport. The transport
// is considered "alive" once the first successful request has completed.
//
//nolint:gocritic // config matches existing Client constructor signatures
func newStreamableTransport(config ServerConfig, options ClientOptions, in inbound) *streamableTransport {
	return &streamableTransport{
		config:     config,
		options:    options,
		in:         in,
		httpClient: &http.Client{}, //nolint:exhaustruct // per-request timeouts come from the session's context
		url:        config.URL,
	}
}

// close ends the transport. If the server assigned a session, it is told the
// session is over with an HTTP DELETE, so it can release it. Idempotent.
func (t *streamableTransport) close() error {
	if t.closed.Swap(true) {
		return nil
	}
	t.alive.Store(false)
	t.mu.Lock()
	sid := t.sessionID
	t.sessionID = ""
	t.mu.Unlock()
	if sid != "" {
		t.deleteSession(sid)
	}
	return nil
}

func (t *streamableTransport) deleteSession(sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionDeleteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, t.url, http.NoBody)
	if err != nil {
		return
	}
	t.applyConfigHeaders(req)
	t.applySessionHeaders(req, sid)
	resp, err := t.httpClient.Do(req)
	if err != nil {
		logger.Debug("MCP/Streamable session DELETE failed", "server", t.config.Name, "error", err)
		return
	}
	_ = resp.Body.Close()
}

// send POSTs a request and returns its response.
func (t *streamableTransport) send(ctx context.Context, req *request) (*JSONRPCMessage, error) {
	if t.closed.Load() {
		return nil, ErrClientClosed
	}
	resp, sentSession, err := t.post(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound && sentSession != "":
		// The server no longer knows the session (2025-03-26+): the client MUST
		// start a new one with a fresh initialize.
		t.dropSession(sentSession)
		return nil, errSessionExpired
	case resp.StatusCode == http.StatusAccepted:
		return nil, fmt.Errorf("mcp/streamable: %s: %w", req.method, errNoResponse)
	case resp.StatusCode != http.StatusOK:
		return nil, t.statusError(resp)
	}
	t.captureSession(resp)

	contentType := resp.Header.Get(headerContentType)
	switch {
	case strings.HasPrefix(contentType, contentTypeJSON):
		var msg JSONRPCMessage
		if derr := json.NewDecoder(resp.Body).Decode(&msg); derr != nil {
			return nil, fmt.Errorf("mcp/streamable: decode json response: %w", derr)
		}
		if !isResponse(&msg) {
			return nil, fmt.Errorf("mcp/streamable: %s: JSON body is not a response", req.method)
		}
		t.alive.Store(true)
		return &msg, nil
	case strings.HasPrefix(contentType, contentTypeSSE):
		msg, rerr := t.readStream(ctx, resp.Body, req.id)
		if rerr != nil {
			return nil, rerr
		}
		t.alive.Store(true)
		return msg, nil
	default:
		return nil, fmt.Errorf("mcp/streamable: unexpected content type %q", contentType)
	}
}

// statusError turns a non-2xx response into an error. A JSON-RPC error in
// the body is the server's answer and is returned as an *RPCError.
func (t *streamableTransport) statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	var msg JSONRPCMessage
	if json.Unmarshal(body, &msg) == nil && msg.Error != nil {
		return rpcErrorFrom(msg.Error)
	}
	return &httpStatusError{status: resp.StatusCode, body: strings.TrimSpace(string(body))}
}

// httpStatusError is a non-2xx response without a JSON-RPC error body.
type httpStatusError struct {
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("mcp/streamable: POST status %d", e.status)
	}
	return fmt.Sprintf("mcp/streamable: POST status %d: %s", e.status, e.body)
}

// notify POSTs a notification. The server answers 202 Accepted; a 200 is
// tolerated and its body ignored.
func (t *streamableTransport) notify(ctx context.Context, req *request) error {
	if t.closed.Load() {
		return ErrClientClosed
	}
	resp, _, err := t.post(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp/streamable: POST status %d", resp.StatusCode)
	}
	return nil
}

// cancelRequest POSTs the cancellation notification for an abandoned request.
// The abandoned POST has already been closed, which a server may also take
// as cancellation.
func (t *streamableTransport) cancelRequest(ctx context.Context, id int64, reason string, header http.Header) {
	if err := t.notify(ctx, cancelNotification(id, reason, header)); err != nil {
		logger.Debug("MCP/Streamable failed to send cancellation", "server", t.config.Name, "error", err)
	}
}

// post sends one message and returns the HTTP response, plus the session id
// it was sent with.
func (t *streamableTransport) post(ctx context.Context, req *request) (*http.Response, string, error) {
	body, err := json.Marshal(req.message())
	if err != nil {
		return nil, "", fmt.Errorf("mcp/streamable: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("mcp/streamable: build POST: %w", err)
	}
	httpReq.Header.Set(headerContentType, contentTypeJSON)
	httpReq.Header.Set(headerAccept, contentTypeJSON+", "+contentTypeSSE)
	t.applyConfigHeaders(httpReq)
	for k, vs := range req.header {
		httpReq.Header[k] = vs
	}
	t.mu.Lock()
	sid := t.sessionID
	if v := req.header.Get(headerProtocolVersion); v != "" {
		t.protocolVersion = v
	}
	t.mu.Unlock()
	if sid != "" {
		httpReq.Header.Set(headerSessionID, sid)
	}
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("mcp/streamable: POST: %w", err)
	}
	return resp, sid, nil
}

func (t *streamableTransport) applyConfigHeaders(req *http.Request) {
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
}

// applySessionHeaders sets the session id and protocol version on a message
// the transport originates itself.
func (t *streamableTransport) applySessionHeaders(req *http.Request, sid string) {
	t.mu.Lock()
	v := t.protocolVersion
	if sid == "" {
		sid = t.sessionID
	}
	t.mu.Unlock()
	if v != "" {
		req.Header.Set(headerProtocolVersion, v)
	}
	if sid != "" {
		req.Header.Set(headerSessionID, sid)
	}
}

func (t *streamableTransport) captureSession(resp *http.Response) {
	sid := resp.Header.Get(headerSessionID)
	if sid == "" {
		return
	}
	t.mu.Lock()
	t.sessionID = sid
	t.mu.Unlock()
}

// dropSession forgets an expired session id, unless another request has
// already replaced it.
func (t *streamableTransport) dropSession(sid string) {
	t.mu.Lock()
	if t.sessionID == sid {
		t.sessionID = ""
	}
	t.mu.Unlock()
}

// readStream reads a request's SSE response stream until the response with
// the request's id. Other messages on the stream — notifications, and
// requests from the server — are handed to the session, and the session's
// answers are POSTed back.
func (t *streamableTransport) readStream(ctx context.Context, body io.Reader, wantID int64) (*JSONRPCMessage, error) {
	reader := bufio.NewReader(body)
	for {
		ev, err := readSSEEvent(reader)
		if errors.Is(err, io.EOF) {
			return nil, errors.New("mcp/streamable: SSE stream closed without matching response")
		}
		if err != nil {
			return nil, fmt.Errorf("mcp/streamable: read SSE: %w", err)
		}
		if ev.event != "" && ev.event != sseEventMessage {
			continue
		}
		var msg JSONRPCMessage
		if jerr := json.Unmarshal([]byte(ev.data), &msg); jerr != nil {
			continue
		}
		reply := func(r *JSONRPCMessage) { t.postReply(ctx, r) }
		if resp, ok := routeStreamMessage(ctx, t.in, &msg, wantID, reply); ok {
			return resp, nil
		}
	}
}

// postReply POSTs the client's response to a server request.
func (t *streamableTransport) postReply(ctx context.Context, reply *JSONRPCMessage) {
	body, err := json.Marshal(reply)
	if err != nil {
		return
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	httpReq.Header.Set(headerContentType, contentTypeJSON)
	httpReq.Header.Set(headerAccept, contentTypeJSON+", "+contentTypeSSE)
	t.applyConfigHeaders(httpReq)
	t.applySessionHeaders(httpReq, "")
	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		logger.Warn("MCP/Streamable failed to answer server request", "server", t.config.Name, "error", err)
		return
	}
	_ = resp.Body.Close()
}
