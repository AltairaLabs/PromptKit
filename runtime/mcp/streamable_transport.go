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

// Stream resumption (2025-11-25 basic/transports): a server may close a
// request's SSE connection before the response, once it has sent an event
// id; the client resumes with GET and Last-Event-ID after the retry delay
// the server asked for.
const (
	headerLastEventID = "Last-Event-ID"
	// defaultSSERetry is the reconnection delay when the server set none.
	defaultSSERetry = time.Second
	// maxStreamResumes bounds how often one request's stream is resumed, so
	// a server that keeps closing it cannot hold the client polling forever.
	maxStreamResumes = 30
)

// errStreamEnded is returned when a request's stream ends without its
// response and cannot be resumed.
var errStreamEnded = errors.New("mcp/streamable: SSE stream closed without matching response")

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

	doer *httpDoer
	url  string

	mu        sync.Mutex
	sessionID string // from Mcp-Session-Id on the initialize response
	// protocolVersion is the MCP-Protocol-Version the session last sent,
	// repeated on messages the transport originates (replies, DELETE).
	protocolVersion string

	// listening is set while the standalone GET stream runs; listenCancel
	// stops it.
	listening    atomic.Bool
	listenCancel context.CancelFunc
	wg           sync.WaitGroup

	// modern is set once the server is known to speak a stateless revision
	// (2026-07-28): no sessions, no standalone stream, no resumption, and
	// closing a response stream is the cancellation.
	modern atomic.Bool

	closed atomic.Bool
	alive  atomic.Bool
}

func (t *streamableTransport) supportsModern() bool { return true }

func (t *streamableTransport) carriesHeaders() {}

func (t *streamableTransport) setModern(modern bool) { t.modern.Store(modern) }

// newStreamableTransport constructs a Streamable HTTP transport. The transport
// is considered "alive" once the first successful request has completed.
//
//nolint:gocritic // config matches existing Client constructor signatures
func newStreamableTransport(config ServerConfig, options ClientOptions, in inbound) *streamableTransport {
	return &streamableTransport{
		config:  config,
		options: options,
		in:      in,
		doer: &httpDoer{
			client: &http.Client{}, //nolint:exhaustruct // per-request timeouts come from the session's context
			auth:   options.Authorizer,
			server: config.Name,
		},
		url: config.URL,
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
	stopListening := t.listenCancel
	t.mu.Unlock()
	if stopListening != nil {
		stopListening()
	}
	t.wg.Wait()
	if sid != "" {
		t.deleteSession(sid)
	}
	return nil
}

// Standalone stream (2025-03-26 – 2025-11-25 basic/transports: "Listening
// for Messages from the Server"): servers send requests and notifications not
// tied to a client request — elicitation among them — on a GET stream.
const (
	// maxListenFailures stops reopening the standalone stream after this
	// many consecutive failures; requests still work without it.
	maxListenFailures = 5
	// listenReadyTimeout bounds how long the handshake waits for the
	// standalone stream to connect. A server sends standalone requests only
	// on a connected stream, so a request made before it connects could
	// trigger one that is lost.
	listenReadyTimeout = 2 * time.Second
)

// handshakeDone opens the standalone stream once a session is established,
// and returns when it has connected (or been refused), bounded by
// listenReadyTimeout. It runs at most one listener; a server without the
// stream answers 405.
func (t *streamableTransport) handshakeDone() {
	if t.closed.Load() || !t.listening.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.listenCancel = cancel
	t.mu.Unlock()
	ready := make(chan struct{})
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.listening.Store(false)
		t.listen(ctx, ready)
	}()
	timer := time.NewTimer(listenReadyTimeout)
	defer timer.Stop()
	select {
	case <-ready:
	case <-timer.C:
	}
}

// listen keeps the standalone stream open, reconnecting after the server's
// retry delay, until the transport closes, the server refuses the stream,
// or it keeps failing. ready is closed after the first attempt to connect.
func (t *streamableTransport) listen(ctx context.Context, ready chan struct{}) {
	cur := streamCursor{retry: defaultSSERetry}
	failures := 0
	signal := sync.OnceFunc(func() { close(ready) })
	defer signal()
	for ctx.Err() == nil && failures < maxListenFailures {
		body, refused, err := t.openListen(ctx, cur.lastEventID)
		signal()
		if refused {
			return
		}
		if err != nil {
			failures++
			logger.Debug("MCP/Streamable standalone stream failed", "server", t.config.Name, "error", err)
		} else {
			failures = 0
			t.drainListen(ctx, body, &cur)
			_ = body.Close()
		}
		if sleepCtx(ctx, cur.retry) != nil {
			return
		}
	}
}

// openListen opens the standalone stream. refused reports that the server
// does not offer one (405) or no longer knows the session (404).
func (t *streamableTransport) openListen(ctx context.Context, lastEventID string) (io.ReadCloser, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, http.NoBody)
	if err != nil {
		return nil, true, err
	}
	req.Header.Set(headerAccept, contentTypeSSE)
	if lastEventID != "" {
		req.Header.Set(headerLastEventID, lastEventID)
	}
	t.applyConfigHeaders(req)
	t.applySessionHeaders(req, "")
	resp, err := t.doer.do(req)
	if err != nil {
		return nil, false, err
	}
	switch {
	case resp.StatusCode == http.StatusMethodNotAllowed, resp.StatusCode == http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, true, nil
	case resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get(headerContentType), contentTypeSSE):
		_ = resp.Body.Close()
		return nil, false, fmt.Errorf("GET status %d", resp.StatusCode)
	}
	return resp.Body, false, nil
}

// drainListen routes the standalone stream's messages until it ends.
// Responses do not belong on it (they arrive on resumed request streams).
func (t *streamableTransport) drainListen(ctx context.Context, body io.Reader, cur *streamCursor) {
	reader := bufio.NewReader(body)
	for {
		ev, err := readSSEEvent(reader)
		if err != nil {
			return
		}
		cur.observe(ev)
		msg, ok := decodeStreamEvent(ev)
		if !ok || isResponse(msg) {
			continue
		}
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			dispatchInbound(ctx, t.in, msg, func(r *JSONRPCMessage) { t.postReply(ctx, r) })
		}()
	}
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
	resp, err := t.doer.do(req)
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
	case resp.StatusCode == http.StatusNotFound && sentSession != "" && !t.modern.Load():
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
	if t.modern.Load() {
		return // closing the response stream was the cancellation
	}
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
	resp, err := t.doer.do(httpReq)
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
	if sid == "" || t.modern.Load() {
		return // stateless revisions have no sessions
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
//
// If the connection closes before the response and the server has given the
// stream an event id, the stream is resumed with GET and Last-Event-ID after
// the server's retry delay. Closing the connection is not the server
// canceling the request.
func (t *streamableTransport) readStream(ctx context.Context, body io.Reader, wantID int64) (*JSONRPCMessage, error) {
	cur := streamCursor{retry: defaultSSERetry}
	var resumed io.ReadCloser
	defer func() {
		if resumed != nil {
			_ = resumed.Close()
		}
	}()
	for resumes := 0; ; resumes++ {
		resp, err := t.readStreamOnce(ctx, body, wantID, &cur)
		if resp != nil || err != nil {
			return resp, err
		}
		if t.modern.Load() {
			// Streams are not resumable in stateless revisions; the request is
			// lost and must be re-issued.
			return nil, errStreamBroken
		}
		if cur.lastEventID == "" || resumes >= maxStreamResumes {
			return nil, errStreamEnded
		}
		if werr := sleepCtx(ctx, cur.retry); werr != nil {
			return nil, werr
		}
		next, rerr := t.resume(ctx, cur.lastEventID)
		if rerr != nil {
			return nil, rerr
		}
		if resumed != nil {
			_ = resumed.Close()
		}
		resumed, body = next, next
	}
}

// streamCursor is what a request's stream has delivered so far: where to
// resume it, and how long to wait first.
type streamCursor struct {
	lastEventID string
	retry       time.Duration
}

// readStreamOnce reads one connection of a request's stream. It returns the
// response, an error, or (nil, nil) when the connection ended first.
func (t *streamableTransport) readStreamOnce(
	ctx context.Context, body io.Reader, wantID int64, cur *streamCursor,
) (*JSONRPCMessage, error) {
	reader := bufio.NewReader(body)
	reply := func(r *JSONRPCMessage) { t.postReply(ctx, r) }
	for {
		ev, err := readSSEEvent(reader)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, nil
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, nil // a dropped connection; resume if the stream allows
		}
		cur.observe(ev)
		msg, ok := decodeStreamEvent(ev)
		if !ok {
			continue
		}
		if resp, ok := routeStreamMessage(ctx, t.in, msg, wantID, reply); ok {
			return resp, nil
		}
	}
}

// decodeStreamEvent returns the JSON-RPC message an SSE event carries, if
// it carries one. Priming events (an id with empty data) and other event
// types carry none.
func decodeStreamEvent(ev sseEvent) (*JSONRPCMessage, bool) {
	if ev.data == "" || (ev.event != "" && ev.event != sseEventMessage) {
		return nil, false
	}
	var msg JSONRPCMessage
	if json.Unmarshal([]byte(ev.data), &msg) != nil {
		return nil, false
	}
	return &msg, true
}

func (c *streamCursor) observe(ev sseEvent) {
	if ev.id != "" {
		c.lastEventID = ev.id
	}
	if ev.retry > 0 {
		c.retry = ev.retry
	}
}

// resume reopens a stream with GET and Last-Event-ID.
func (t *streamableTransport) resume(ctx context.Context, lastEventID string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("mcp/streamable: build resume GET: %w", err)
	}
	req.Header.Set(headerAccept, contentTypeSSE)
	req.Header.Set(headerLastEventID, lastEventID)
	t.applyConfigHeaders(req)
	t.applySessionHeaders(req, "")
	resp, err := t.doer.do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp/streamable: resume stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get(headerContentType), contentTypeSSE) {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp/streamable: resume stream: GET status %d: %w", resp.StatusCode, errStreamEnded)
	}
	return resp.Body, nil
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
	resp, err := t.doer.do(httpReq)
	if err != nil {
		logger.Warn("MCP/Streamable failed to answer server request", "server", t.config.Name, "error", err)
		return
	}
	_ = resp.Body.Close()
}

func (t *streamableTransport) isAlive() bool { return t.alive.Load() }
