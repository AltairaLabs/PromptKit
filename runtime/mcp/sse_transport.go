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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// errStreamClosed fails requests still waiting when the SSE stream ends.
var errStreamClosed = errors.New("mcp/sse: event stream closed")

// pendingRequests is an id→channel map for JSON-RPC request/response
// correlation over SSE. Callers register an id, then either wait on the
// channel or cancel() to free the slot.
//
// All channels are buffered (1) so deliver never blocks on a slow consumer.
type pendingRequests struct {
	mu     sync.Mutex
	chans  map[int64]chan *JSONRPCMessage
	closed bool
}

func newPendingRequests() *pendingRequests {
	return &pendingRequests{chans: make(map[int64]chan *JSONRPCMessage)}
}

// register returns the channel the response for id will be delivered on.
// After failAll it returns nil: the stream is gone.
func (p *pendingRequests) register(id int64) <-chan *JSONRPCMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	ch := make(chan *JSONRPCMessage, 1)
	p.chans[id] = ch
	return ch
}

// deliver routes a response to the waiting channel. Unknown ids are dropped.
func (p *pendingRequests) deliver(id int64, msg *JSONRPCMessage) {
	p.mu.Lock()
	ch, ok := p.chans[id]
	if ok {
		delete(p.chans, id)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	ch <- msg
}

// cancel frees a slot without delivering. Called when a caller gives up
// (context canceled, timeout, etc.).
func (p *pendingRequests) cancel(id int64) {
	p.mu.Lock()
	delete(p.chans, id)
	p.mu.Unlock()
}

// failAll closes every waiting channel and refuses new registrations: the
// stream responses arrive on has ended.
func (p *pendingRequests) failAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for id, ch := range p.chans {
		close(ch)
		delete(p.chans, id)
	}
}

// sseEvent is a minimal SSE frame — only the fields we care about.
type sseEvent struct {
	event string
	data  string
	id    string
	// retry is the reconnection delay the server asked for, or 0 if the
	// frame set none.
	retry time.Duration
}

// readSSEEvent reads a single SSE frame (terminated by a blank line) from r.
// Returns io.EOF when the stream ends cleanly between frames.
func readSSEEvent(r *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	var dataLines []string
	sawAny := false

	for {
		line, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			if !sawAny {
				return sseEvent{}, io.EOF
			}
			break
		}
		if err != nil {
			return sseEvent{}, err
		}
		sawAny = true
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			ev.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "id:"):
			ev.id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		case strings.HasPrefix(line, "retry:"):
			if ms, perr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "retry:"))); perr == nil && ms >= 0 {
				ev.retry = time.Duration(ms) * time.Millisecond
			}
		}
		// Unrecognized field lines (including SSE ":" comments) are ignored.
	}
	ev.data = strings.Join(dataLines, "\n")
	return ev, nil
}

// sseTransport implements the deprecated HTTP+SSE transport (2024-11-05): a
// long-lived GET stream carries every server message, and the client POSTs
// its messages to the endpoint the stream announces.
type sseTransport struct {
	config  ServerConfig
	options ClientOptions
	in      inbound

	doer       *httpDoer
	streamURL  string // the URL the stream was opened at
	messageURL string // absolute URL for POSTs (populated by connect())

	pending *pendingRequests

	ctx    context.Context //nolint:containedctx // long-lived transport lifecycle
	cancel context.CancelFunc

	stream io.ReadCloser // the SSE stream body; nil until connect() succeeds

	versionMu       sync.Mutex
	protocolVersion string // last MCP-Protocol-Version sent; repeated on replies

	wg     sync.WaitGroup
	closed atomic.Bool
	alive  atomic.Bool
}

// newSSETransport constructs an SSE transport bound to its own background
// lifecycle context.
//
//nolint:gocritic // config matches existing Client constructor signatures
func newSSETransport(config ServerConfig, options ClientOptions, in inbound) *sseTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &sseTransport{
		config:  config,
		options: options,
		in:      in,
		doer: &httpDoer{
			client: &http.Client{}, //nolint:exhaustruct // per-request timeouts come from the session's context
			auth:   options.Authorizer,
			server: config.Name,
		},
		pending: newPendingRequests(),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// connect opens the SSE stream and blocks until the initial endpoint event
// arrives, at which point messageURL is populated.
//
// IMPORTANT: the HTTP request is bound to t.ctx (the transport's own
// lifecycle), NOT the caller's ctx. The caller's ctx only bounds how long we
// wait for the endpoint event via a watchdog that closes the stream on
// caller-ctx expiry. This prevents the caller's short init timeout from
// killing the long-lived SSE stream after connect returns.
func (t *sseTransport) connect(ctx context.Context) error {
	// NB: on success we hand resp.Body off to t.stream and close it in
	// sseTransport.close(); error paths close it explicitly.
	resp, err := t.openStream() //nolint:bodyclose // body adopted by t.stream or closed below
	if err != nil {
		return err
	}

	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-watchdogDone:
		case <-ctx.Done():
			_ = resp.Body.Close()
		}
	}()
	defer close(watchdogDone)

	reader := bufio.NewReader(resp.Body)
	ev, err := readSSEEvent(reader)
	if err != nil {
		_ = resp.Body.Close()
		if ctx.Err() != nil {
			return fmt.Errorf("mcp/sse: endpoint timeout: %w", ctx.Err())
		}
		return fmt.Errorf("mcp/sse: read endpoint event: %w", err)
	}
	if ev.event != "endpoint" {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp/sse: expected endpoint event, got %q", ev.event)
	}
	messageURL, err := t.resolveMessageURL(ev.data)
	if err != nil {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp/sse: resolve message URL: %w", err)
	}
	t.messageURL = messageURL
	t.stream = resp.Body
	t.alive.Store(true)
	t.wg.Add(1)
	go t.readLoop(reader)
	return nil
}

// sseEndpoints lists the URLs to open the stream at. The configured URL is
// the SSE endpoint (2024-11-05 basic/transports; the Streamable HTTP
// fallback GETs the same URL). Earlier releases appended "/sse" to it, so
// that is tried second, for configs that name the server's base URL.
func sseEndpoints(configured string) []string {
	base := strings.TrimRight(configured, "/")
	if strings.HasSuffix(base, "/sse") {
		return []string{configured}
	}
	return []string{configured, base + "/sse"}
}

// openStream GETs the SSE stream from the first endpoint that serves one.
// On success the caller owns the response body.
func (t *sseTransport) openStream() (*http.Response, error) {
	endpoints := sseEndpoints(t.config.URL)
	for i, endpoint := range endpoints {
		resp, err := t.getStream(endpoint)
		if err != nil {
			return nil, err
		}
		isStream := resp.StatusCode == http.StatusOK &&
			strings.HasPrefix(resp.Header.Get(headerContentType), contentTypeSSE)
		if isStream {
			if i > 0 {
				logger.Warn("MCP SSE stream found at the URL plus /sse; configure the SSE endpoint URL itself",
					"server", t.config.Name, "url", endpoint)
			}
			t.streamURL = endpoint
			return resp, nil
		}
		_ = resp.Body.Close()
		// A page that is not an event stream (a landing page at a base URL)
		// means the stream is elsewhere, as do 404 and 405.
		notHere := resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed
		if !notHere || i == len(endpoints)-1 {
			return nil, streamError(endpoint, resp)
		}
	}
	return nil, errors.New("mcp/sse: no endpoint") // unreachable: endpoints is never empty
}

// streamError describes a GET that did not open an event stream.
func streamError(endpoint string, resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return fmt.Errorf("mcp/sse: GET %s returned %q, not an event stream", endpoint, resp.Header.Get(headerContentType))
	}
	return fmt.Errorf("mcp/sse: GET %s status %d", endpoint, resp.StatusCode)
}

// getStream sends the GET that opens an SSE stream.
func (t *sseTransport) getStream(endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: build GET %s: %w", endpoint, err)
	}
	req.Header.Set(headerAccept, contentTypeSSE)
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	resp, err := t.doer.do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp/sse: GET %s: %w", endpoint, err)
	}
	return resp, nil
}

// resolveMessageURL turns the endpoint event's data (absolute or relative)
// into an absolute URL against the stream's URL.
func (t *sseTransport) resolveMessageURL(data string) (string, error) {
	u, err := url.Parse(t.streamURL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(data)
	if err != nil {
		return "", err
	}
	return u.ResolveReference(ref).String(), nil
}

// close tears down the transport. Safe to call multiple times.
func (t *sseTransport) close() error {
	if t.closed.Swap(true) {
		return nil
	}
	t.alive.Store(false)
	t.cancel()
	if t.stream != nil {
		_ = t.stream.Close()
	}
	t.wg.Wait()
	return nil
}

// send POSTs a request and waits for its response on the stream.
func (t *sseTransport) send(ctx context.Context, req *request) (*JSONRPCMessage, error) {
	if t.messageURL == "" {
		return nil, errors.New("mcp/sse: transport not connected")
	}
	ch := t.pending.register(req.id)
	if ch == nil {
		return nil, errStreamClosed
	}

	if err := t.post(ctx, req.message(), req.header); err != nil {
		t.pending.cancel(req.id)
		return nil, err
	}

	select {
	case reply, ok := <-ch:
		if !ok {
			return nil, errStreamClosed
		}
		return reply, nil
	case <-ctx.Done():
		t.pending.cancel(req.id)
		return nil, ctx.Err()
	}
}

// notify POSTs a notification. Notifications get no response, so the POST's
// 2xx status is the only acknowledgment.
func (t *sseTransport) notify(ctx context.Context, req *request) error {
	return t.post(ctx, req.message(), req.header)
}

// cancelRequest sends the cancellation notification: the response would arrive on
// the shared stream, so there is no per-request stream to close.
func (t *sseTransport) cancelRequest(ctx context.Context, id int64, reason string, header http.Header) {
	if err := t.notify(ctx, cancelNotification(id, reason, header)); err != nil {
		logger.Debug("MCP/SSE failed to send cancellation", "server", t.config.Name, "error", err)
	}
}

// post POSTs one JSON-RPC message to the message endpoint.
func (t *sseTransport) post(ctx context.Context, msg *JSONRPCMessage, header http.Header) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mcp/sse: marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mcp/sse: build POST: %w", err)
	}
	req.Header.Set(headerContentType, contentTypeJSON)
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	t.versionMu.Lock()
	if v := header.Get(headerProtocolVersion); v != "" {
		t.protocolVersion = v
	}
	if t.protocolVersion != "" {
		req.Header.Set(headerProtocolVersion, t.protocolVersion)
	}
	t.versionMu.Unlock()

	resp, err := t.doer.do(req)
	if err != nil {
		return fmt.Errorf("mcp/sse: POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp/sse: POST status %d", resp.StatusCode)
	}
	return nil
}

// readLoop reads the stream until it ends. Responses go to the request
// waiting for them; server requests and notifications go to the session,
// and the session's answers are POSTed back.
func (t *sseTransport) readLoop(reader *bufio.Reader) {
	defer t.wg.Done()
	defer func() {
		t.alive.Store(false)
		t.pending.failAll()
	}()
	for {
		ev, err := readSSEEvent(reader)
		if err != nil {
			return
		}
		if ev.event != sseEventMessage {
			continue
		}
		var msg JSONRPCMessage
		if jerr := json.Unmarshal([]byte(ev.data), &msg); jerr != nil {
			continue
		}
		t.route(&msg)
	}
}

// route delivers a response to the request waiting for it, and answers
// anything else off the read loop.
func (t *sseTransport) route(msg *JSONRPCMessage) {
	if isResponse(msg) {
		if id, ok := coerceID(msg.ID); ok {
			t.pending.deliver(id, msg)
		}
		return
	}
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		dispatchInbound(t.ctx, t.in, msg, func(reply *JSONRPCMessage) {
			if perr := t.post(t.ctx, reply, nil); perr != nil {
				logger.Warn("MCP/SSE failed to answer server request", "server", t.config.Name, "error", perr)
			}
		})
	}()
}

func (t *sseTransport) isAlive() bool { return t.alive.Load() }
