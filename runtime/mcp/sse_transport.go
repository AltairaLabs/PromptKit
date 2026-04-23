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
	"strings"
	"sync"
	"sync/atomic"
)

// pendingRequests is an id→channel map for JSON-RPC request/response
// correlation over SSE. Callers register() to get a channel + id, then
// either wait on the channel or cancel() to free the slot.
//
// All channels are buffered (1) so deliver never blocks on a slow consumer.
type pendingRequests struct {
	mu     sync.Mutex
	nextID atomic.Int64
	chans  map[int64]chan *JSONRPCMessage
}

func newPendingRequests() *pendingRequests {
	return &pendingRequests{chans: make(map[int64]chan *JSONRPCMessage)}
}

// register allocates a new id and returns a receive-only channel that will
// receive the response for that id (or nothing, if cancel is called first).
func (p *pendingRequests) register() (reply <-chan *JSONRPCMessage, id int64) {
	id = p.nextID.Add(1)
	ch := make(chan *JSONRPCMessage, 1)
	p.mu.Lock()
	p.chans[id] = ch
	p.mu.Unlock()
	return ch, id
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

// sseEvent is a minimal SSE frame — only the fields we care about.
type sseEvent struct {
	event string
	data  string
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
		}
		// Unrecognized field lines (including SSE ":" comments) are ignored.
	}
	ev.data = strings.Join(dataLines, "\n")
	return ev, nil
}

// sseTransport owns the HTTP connection to the server's /sse endpoint,
// the background reader loop, and the pending-request map.
type sseTransport struct {
	config  ServerConfig
	options ClientOptions

	httpClient *http.Client
	baseURL    string
	messageURL string // absolute URL for POSTs (populated by connect())

	pending *pendingRequests

	ctx    context.Context //nolint:containedctx // long-lived transport lifecycle
	cancel context.CancelFunc

	stream io.ReadCloser // the SSE stream body; nil until connect() succeeds

	wg     sync.WaitGroup
	closed atomic.Bool
	alive  atomic.Bool
}

// newSSETransport constructs an SSE transport bound to its own background
// lifecycle context.
//
//nolint:gocritic // config matches existing Client constructor signatures
func newSSETransport(config ServerConfig, options ClientOptions) *sseTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &sseTransport{
		config:     config,
		options:    options,
		httpClient: &http.Client{}, //nolint:exhaustruct // stdlib defaults are fine
		baseURL:    strings.TrimRight(config.URL, "/"),
		pending:    newPendingRequests(),
		ctx:        ctx,
		cancel:     cancel,
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
	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, t.baseURL+"/sse", http.NoBody)
	if err != nil {
		return fmt.Errorf("mcp/sse: build GET /sse: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}

	// NB: on success we hand resp.Body off to t.stream and close it in
	// sseTransport.close(); error paths close it explicitly.
	resp, err := t.httpClient.Do(req) //nolint:bodyclose // body adopted by t.stream or closed below
	if err != nil {
		return fmt.Errorf("mcp/sse: GET /sse: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp/sse: GET /sse status %d", resp.StatusCode)
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
	return nil
}

// resolveMessageURL turns the endpoint event's data (absolute or relative)
// into an absolute URL against baseURL.
func (t *sseTransport) resolveMessageURL(data string) (string, error) {
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
		return data, nil
	}
	u, err := url.Parse(t.baseURL)
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
func (t *sseTransport) close() {
	if t.closed.Swap(true) {
		return
	}
	t.alive.Store(false)
	t.cancel()
	if t.stream != nil {
		_ = t.stream.Close()
	}
	t.wg.Wait()
}

// sendRequest marshals a JSON-RPC request, POSTs it to messageURL, and waits
// for the matching response on the pending-request channel. The ctx bounds
// how long we wait for the response; cancellation does not tear down the
// transport.
func (t *sseTransport) sendRequest(ctx context.Context, method string, params, out any) error {
	if t.messageURL == "" {
		return errors.New("mcp/sse: transport not connected")
	}
	ch, id := t.pending.register()

	if err := t.postJSONRPC(ctx, id, method, params); err != nil {
		t.pending.cancel(id)
		return err
	}

	select {
	case reply := <-ch:
		return unmarshalReply(reply, out)
	case <-ctx.Done():
		t.pending.cancel(id)
		return ctx.Err()
	}
}

func (t *sseTransport) postJSONRPC(ctx context.Context, id int64, method string, params any) error {
	var paramBytes json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("mcp/sse: marshal params: %w", err)
		}
		paramBytes = b
	}
	body, err := json.Marshal(JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramBytes,
	})
	if err != nil {
		return fmt.Errorf("mcp/sse: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.messageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mcp/sse: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp/sse: POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp/sse: POST status %d", resp.StatusCode)
	}
	return nil
}

func unmarshalReply(reply *JSONRPCMessage, out any) error {
	if reply.Error != nil {
		return fmt.Errorf("mcp/sse: rpc error %d: %s", reply.Error.Code, reply.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("mcp/sse: unmarshal result: %w", err)
	}
	return nil
}

// startReadLoop runs the SSE read goroutine. Must be called after connect().
func (t *sseTransport) startReadLoop() {
	t.wg.Add(1)
	go t.readLoop()
}

func (t *sseTransport) readLoop() {
	defer t.wg.Done()
	reader := bufio.NewReader(t.stream)
	for {
		ev, err := readSSEEvent(reader)
		if err != nil {
			t.alive.Store(false)
			return
		}
		if ev.event != "message" {
			continue
		}
		var msg JSONRPCMessage
		if jerr := json.Unmarshal([]byte(ev.data), &msg); jerr != nil {
			continue
		}
		if msg.ID == nil {
			// notification — no response handler in v1
			continue
		}
		id, ok := coerceID(msg.ID)
		if !ok {
			continue
		}
		t.pending.deliver(id, &msg)
	}
}

// coerceID turns a JSON-decoded id (float64 / int / int64) back into int64.
// We only ever send int64 ids, so ignore non-numeric ids.
func coerceID(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}
