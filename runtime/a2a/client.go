package a2a

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// HTTP client defaults for A2A communication.
const (
	defaultClientTimeout       = 60 * time.Second
	defaultDialTimeout         = 30 * time.Second
	defaultDialKeepAlive       = 30 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 10
	defaultMaxConnsPerHost     = 10
	defaultIdleConnTimeout     = 90 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second

	// sseClientTimeout is the HTTP client timeout for SSE streaming requests.
	// SSE connections are long-lived, so the default 60s timeout is too short.
	sseClientTimeout = 30 * time.Minute

	// sseMaxTokenSize is the maximum token size for the SSE scanner (1MB).
	// The default bufio.Scanner buffer of 64KB is too small for large artifacts
	// such as base64-encoded images.
	sseMaxTokenSize = 1 << 20

	// DefaultSSEIdleTimeout is the default idle timeout for SSE streams.
	// If no event is received within this duration, ReadSSE returns an error
	// so callers can reconnect.
	DefaultSSEIdleTimeout = 5 * time.Minute
)

// RPCError represents a JSON-RPC error returned by an A2A agent.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("a2a: rpc error %d: %s", e.Code, e.Message)
}

// HTTPStatusError is returned when an A2A HTTP request receives a non-200 status code.
type HTTPStatusError struct {
	StatusCode int
	Method     string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("a2a: %s: status %d", e.Method, e.StatusCode)
}

// StreamEvent represents a single event received during message streaming.
// Exactly one field will be non-nil. A conforming agent opens a task stream
// with the Task itself, or answers with a single Message.
type StreamEvent struct {
	Task           *Task
	Message        *Message
	StatusUpdate   *TaskStatusUpdateEvent
	ArtifactUpdate *TaskArtifactUpdateEvent
}

// ClientOption configures a [Client].
type ClientOption func(*Client)

// WithHTTPClient sets the underlying HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// WithAuth sets the Authorization header on all requests.
func WithAuth(scheme, token string) ClientOption {
	return func(c *Client) {
		c.authScheme = scheme
		c.authToken = token
	}
}

// WithHeaders sets custom headers that are sent on all requests.
func WithHeaders(headers map[string]string) ClientOption {
	return func(c *Client) { c.customHeaders = headers }
}

// WithProtocolVersion pins the A2A protocol version the client speaks,
// disabling negotiation. Without it the client speaks 1.0, or what a
// discovered agent card prefers, and falls back to 0.3 when the agent rejects
// a 1.0 method.
func WithProtocolVersion(v ProtocolVersion) ClientOption {
	return func(c *Client) {
		c.version = v
		c.versionPinned = true
	}
}

// WithRequestTimeout sets the timeout for non-streaming requests (agent card
// discovery and message/send, tasks/get, ...). The default is 60s. It does not
// affect SSE streams, which are bounded by the SSE idle timeout instead.
// A zero or negative value leaves the default in place.
func WithRequestTimeout(d time.Duration) ClientOption {
	return func(c *Client) {
		if d <= 0 {
			return
		}
		hc := *c.httpClient
		hc.Timeout = d
		c.httpClient = &hc
	}
}

// WithSSEIdleTimeout sets the idle timeout for SSE streams. If no event
// is received within this duration, the stream is considered stale and
// ReadSSE returns [ErrSSEIdleTimeout] so callers can reconnect.
// A zero or negative value disables the idle timeout.
func WithSSEIdleTimeout(d time.Duration) ClientOption {
	return func(c *Client) { c.sseIdleTimeout = d }
}

// ErrSSEIdleTimeout is returned when an SSE stream has not received any
// event within the configured idle timeout period.
var ErrSSEIdleTimeout = fmt.Errorf("a2a: SSE idle timeout exceeded")

// Client is an HTTP client for discovering and calling external A2A agents.
type Client struct {
	baseURL        string
	httpClient     *http.Client
	sseClient      *http.Client // separate client for long-lived SSE streams
	sseIdleTimeout time.Duration
	authScheme     string
	authToken      string
	customHeaders  map[string]string
	reqID          int64

	mu        sync.RWMutex
	agentCard *AgentCard

	// version is the protocol version spoken to the agent. Until it is
	// pinned — by WithProtocolVersion, a discovered card, or a fallback — the
	// client speaks 1.0 and drops to 0.3 if the agent does not know the
	// 1.0 method.
	version       ProtocolVersion
	versionPinned bool
}

// newDefaultTransport creates an HTTP transport with connection pooling,
// shared by both the regular and SSE HTTP clients.
func newDefaultTransport() *http.Transport {
	return &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: defaultDialKeepAlive,
		}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		MaxIdleConns:        defaultMaxIdleConns,
		MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost,
		MaxConnsPerHost:     defaultMaxConnsPerHost,
		IdleConnTimeout:     defaultIdleConnTimeout,
		TLSHandshakeTimeout: defaultTLSHandshakeTimeout,
		ForceAttemptHTTP2:   true,
	}
}

// newDefaultHTTPClient creates an HTTP client with connection pooling and a timeout,
// used as the default for non-streaming A2A communication.
func newDefaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   defaultClientTimeout,
		Transport: newDefaultTransport(),
	}
}

// newDefaultSSEClient creates an HTTP client for long-lived SSE streaming
// connections. It uses a longer timeout than the regular client because SSE
// streams remain open for the duration of a task.
func newDefaultSSEClient() *http.Client {
	return &http.Client{
		Timeout:   sseClientTimeout,
		Transport: newDefaultTransport(),
	}
}

// NewClient creates a Client targeting baseURL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:        strings.TrimRight(baseURL, "/"),
		httpClient:     newDefaultHTTPClient(),
		sseClient:      newDefaultSSEClient(),
		sseIdleTimeout: DefaultSSEIdleTimeout,
		version:        ProtocolVersion10,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) setAuth(req *http.Request) {
	if c.authToken != "" {
		req.Header.Set("Authorization", c.authScheme+" "+c.authToken)
	}
	for k, v := range c.customHeaders {
		req.Header.Set(k, v)
	}
}

func (c *Client) nextID() int64 {
	return atomic.AddInt64(&c.reqID, 1)
}

// Agent card discovery paths. A2A 0.3 (§5.3) and 1.0 (§8.2) serve the card at
// AgentCardPath; 0.2 and earlier servers used LegacyAgentCardPath.
const (
	AgentCardPath       = "/.well-known/agent-card.json"
	LegacyAgentCardPath = "/.well-known/agent.json"
)

// Discover fetches the agent card, trying [AgentCardPath] first and falling
// back to [LegacyAgentCardPath] when the agent does not serve it (404/405).
// The card is cached after the first successful call.
func (c *Client) Discover(ctx context.Context) (*AgentCard, error) {
	c.mu.RLock()
	if c.agentCard != nil {
		card := c.agentCard
		c.mu.RUnlock()
		return card, nil
	}
	c.mu.RUnlock()

	card, status, err := c.fetchCard(ctx, AgentCardPath)
	if err != nil && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		card, _, err = c.fetchCard(ctx, LegacyAgentCardPath)
	}
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.agentCard = card
	// Only a card that says which versions it serves settles the question.
	// One that declares no interfaces — a pre-1.0 PromptKit server's, say —
	// leaves negotiation on, so the 0.3 fallback still works.
	if v, declared := card.declaredVersion(); declared && !c.versionPinned {
		c.version = v
		c.versionPinned = true
	}
	c.mu.Unlock()

	return card, nil
}

// ProtocolVersion returns the protocol version the client currently speaks.
func (c *Client) ProtocolVersion() ProtocolVersion {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// currentVersion returns the version to speak and whether it may still fall
// back to 0.3.
func (c *Client) currentVersion() (v ProtocolVersion, canFallBack bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version, !c.versionPinned && c.version == ProtocolVersion10
}

// settleVersion pins v once the agent has answered a request made in it.
func (c *Client) settleVersion(v ProtocolVersion) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.versionPinned {
		c.version = v
		c.versionPinned = true
	}
}

// isVersionMismatch reports whether err says the agent does not speak the
// version (or method name) the request used.
func isVersionMismatch(err error) bool {
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Code == ErrCodeMethodNotFound || rpcErr.Code == ErrCodeVersionNotSupported
}

// withVersionFallback runs call in the client's version and, when the version
// is not yet settled and the agent rejects a 1.0 request as an unknown method
// or unsupported version, once more in 0.3.
func (c *Client) withVersionFallback(call func(ProtocolVersion) error) error {
	v, canFallBack := c.currentVersion()
	err := call(v)
	if err == nil {
		c.settleVersion(v)
		return nil
	}
	if !canFallBack || !isVersionMismatch(err) {
		return err
	}
	if err := call(ProtocolVersion03); err != nil {
		return err
	}
	c.settleVersion(ProtocolVersion03)
	return nil
}

// fetchCard GETs the agent card at path. It returns the HTTP status alongside
// any error so Discover can decide whether to try the legacy path.
func (c *Client) fetchCard(ctx context.Context, path string) (*AgentCard, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("a2a: discover: %w", err)
	}
	c.setAuth(httpReq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("a2a: discover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("a2a: discover %s: status %d", path, resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("a2a: decode agent card: %w", err)
	}
	return &card, resp.StatusCode, nil
}

// newRPCRequest builds the HTTP request for one JSON-RPC call in version v.
func (c *Client) newRPCRequest(
	ctx context.Context, v ProtocolVersion, method string, params any,
) (*http.Request, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal params: %w", err)
	}

	body, err := json.Marshal(JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  paramsJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/a2a", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("a2a: %s: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(HeaderVersion, string(v))
	c.setAuth(httpReq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))
	return httpReq, nil
}

// rpcCall performs op as a JSON-RPC 2.0 POST to /a2a in the negotiated
// protocol version and returns the raw result. params builds the request
// params for a version, since 0.3 and 1.0 shape them differently.
func (c *Client) rpcCall(
	ctx context.Context, op Operation, params func(ProtocolVersion) any,
) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.withVersionFallback(func(v ProtocolVersion) error {
		var err error
		result, err = c.rpcCallVersion(ctx, v, op, params(v))
		return err
	})
	return result, err
}

// rpcCallVersion performs one JSON-RPC call in version v.
func (c *Client) rpcCallVersion(
	ctx context.Context, v ProtocolVersion, op Operation, params any,
) (json.RawMessage, error) {
	method := v.Method(op)
	httpReq, err := c.newRPCRequest(ctx, v, method, params)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Method: method}
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("a2a: %s: decode response: %w", method, err)
	}

	if rpcResp.Error != nil {
		return nil, &RPCError{Code: rpcResp.Error.Code, Message: rpcResp.Error.Message}
	}
	return rpcResp.Result, nil
}

// sameParams is a params builder for operations whose params have one shape
// in every version.
func sameParams(p any) func(ProtocolVersion) any {
	return func(ProtocolVersion) any { return p }
}

// SendMessage sends a message (SendMessage; 0.3: message/send) and returns the
// resulting task.
//
// An agent that answers with a Message rather than a Task gets a completed
// task synthesized around it, the message as its status message, so callers
// have one shape to read.
func (c *Client) SendMessage(ctx context.Context, params *SendMessageRequest) (*Task, error) {
	raw, err := c.rpcCall(ctx, OpSendMessage, func(v ProtocolVersion) any {
		return v.WireSendParams(params)
	})
	if err != nil {
		return nil, err
	}
	task, err := decodeSendResult(raw)
	if err != nil {
		return nil, fmt.Errorf("a2a: %s: decode result: %w", MethodV1SendMessage, err)
	}
	return task, nil
}

// decodeSendResult reads a SendMessage result in any version's shape: 1.0's
// {"task"|"message": ...} or a bare task or message (0.3 and legacy).
func decodeSendResult(raw json.RawMessage) (*Task, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if t, ok := fields[streamKeyTask]; ok {
		raw = t
	} else if m, ok := fields[streamKeyMessage]; ok {
		return decodeMessageResult(m)
	} else if kind := string(fields["kind"]); kind == `"`+kindMessage+`"` {
		return decodeMessageResult(raw)
	}
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func decodeMessageResult(raw json.RawMessage) (*Task, error) {
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	return &Task{
		ID:        msg.TaskID,
		ContextID: msg.ContextID,
		Status:    TaskStatus{State: TaskStateCompleted, Message: &msg},
	}, nil
}

// SendMessageStream sends a streaming message (SendStreamingMessage; 0.3:
// message/stream) and returns a channel of streaming events. The channel is
// closed when the stream ends or the context is canceled.
func (c *Client) SendMessageStream(ctx context.Context, params *SendMessageRequest) (<-chan StreamEvent, error) {
	var resp *http.Response
	err := c.withVersionFallback(func(v ProtocolVersion) error {
		var err error
		//nolint:bodyclose // closed by the reader goroutine below
		resp, err = c.openStream(ctx, v, v.Method(OpSendStreamingMessage), v.WireSendParams(params))
		return err
	})
	if err != nil {
		return nil, err
	}

	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		// Close the response body on context cancellation to unblock the scanner
		// goroutine inside ReadSSEWithIdleTimeout, preventing a goroutine leak.
		// Use streamDone to ensure the inner goroutine exits on normal completion.
		streamDone := make(chan struct{})
		defer close(streamDone)
		go func() {
			select {
			case <-ctx.Done():
				_ = resp.Body.Close()
			case <-streamDone:
			}
		}()
		ReadSSEWithIdleTimeout(ctx, resp.Body, ch, c.sseIdleTimeout)
	}()

	return ch, nil
}

// openStream POSTs a streaming request and returns the open SSE response. A
// server that refuses before streaming answers with a plain JSON-RPC error,
// which is returned as an *RPCError.
func (c *Client) openStream(ctx context.Context, v ProtocolVersion, method string, params any) (*http.Response, error) {
	httpReq, err := c.newRPCRequest(ctx, v, method, params)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.sseClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Method: method}
	}

	// A refusal is a JSON object, not an event stream. Tell them apart by the
	// body rather than the Content-Type header, which not every server sets.
	body := bufio.NewReader(resp.Body)
	if first, err := firstNonSpace(body); err == nil && first == '{' {
		defer resp.Body.Close()
		var rpcResp JSONRPCResponse
		if err := json.NewDecoder(body).Decode(&rpcResp); err != nil {
			return nil, fmt.Errorf("a2a: %s: decode response: %w", method, err)
		}
		if rpcResp.Error != nil {
			return nil, &RPCError{Code: rpcResp.Error.Code, Message: rpcResp.Error.Message}
		}
		return nil, fmt.Errorf("a2a: %s: expected an event stream, got a JSON result", method)
	}
	resp.Body = readCloser{Reader: body, Closer: resp.Body}
	return resp, nil
}

// readCloser pairs a buffered reader with the body it wraps.
type readCloser struct {
	io.Reader
	io.Closer
}

// firstNonSpace peeks past leading whitespace and returns the next byte
// without consuming it.
func firstNonSpace(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.Peek(1)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			_, _ = r.ReadByte()
		default:
			return b[0], nil
		}
	}
}

// Polling intervals for WaitForTask: quick at first, since most turns are
// short, backing off so a long turn is not polled hard.
const (
	waitInitialInterval = 100 * time.Millisecond
	waitMaxInterval     = time.Second
	waitBackoffFactor   = 2
	// waitMaxConsecutiveFailures is how many polls in a row may fail before
	// WaitForTask gives up.
	waitMaxConsecutiveFailures = 5
)

// WaitForTask polls task until it finishes or needs the caller (a terminal or
// interrupted state), and returns it as it then stands. A task that is
// already there is returned as is. ctx bounds the wait; when it ends, the last
// state seen is returned with ctx's error.
//
// A SendMessage can come back before its task is done: with
// returnImmediately, from a 0.3 agent that does not block, or from a server
// that caps how long it holds a request.
func (c *Client) WaitForTask(ctx context.Context, task *Task) (*Task, error) {
	interval := waitInitialInterval
	failures := 0
	for !task.Status.State.IsTerminal() && !task.Status.State.IsInterrupted() {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return task, ctx.Err()
		case <-timer.C:
		}
		interval = min(interval*waitBackoffFactor, waitMaxInterval)

		next, err := c.GetTask(ctx, task.ID)
		if err != nil {
			// GetTask is safe to repeat, so a blip — a 503, a reset, a
			// timeout — is polled through; an answer from the agent (task
			// not found) or repeated failure is not.
			failures++
			if !isTransientPollError(err) || failures >= waitMaxConsecutiveFailures {
				return task, err
			}
			continue
		}
		failures = 0
		task = next
	}
	return task, nil
}

// isTransientPollError reports whether a failed GetTask is worth repeating:
// anything but an agent's JSON-RPC answer or the caller giving up.
func isTransientPollError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var rpcErr *RPCError
	return !errors.As(err, &rpcErr)
}

// GetTask retrieves a task by ID (GetTask; 0.3: tasks/get).
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	raw, err := c.rpcCall(ctx, OpGetTask, sameParams(GetTaskRequest{ID: taskID}))
	if err != nil {
		return nil, err
	}
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return nil, fmt.Errorf("a2a: %s: decode result: %w", MethodV1GetTask, err)
	}
	return &task, nil
}

// CancelTask cancels a task by ID (CancelTask; 0.3: tasks/cancel).
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	_, err := c.rpcCall(ctx, OpCancelTask, sameParams(CancelTaskRequest{ID: taskID}))
	return err
}

// ListTasks lists tasks (ListTasks, which A2A 1.0 added; to a 0.3 agent the
// client sends the legacy PromptKit tasks/list, which only PromptKit servers
// answer).
func (c *Client) ListTasks(ctx context.Context, params *ListTasksRequest) ([]*Task, error) {
	raw, err := c.rpcCall(ctx, OpListTasks, sameParams(params))
	if err != nil {
		return nil, err
	}
	var resp ListTasksResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("a2a: %s: decode result: %w", MethodV1ListTasks, err)
	}
	tasks := make([]*Task, len(resp.Tasks))
	for i := range resp.Tasks {
		tasks[i] = &resp.Tasks[i]
	}
	return tasks, nil
}

// ReadSSE reads SSE events from r and sends parsed StreamEvents to ch.
// It has no idle timeout; use [ReadSSEWithIdleTimeout] for timeout support.
func ReadSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent) {
	ReadSSEWithIdleTimeout(ctx, r, ch, 0)
}

// scanLine is a line read by the background scanner goroutine.
type scanLine struct {
	text string
}

// startScanner launches a background goroutine that reads lines from r and
// sends them to the returned channel. The channel is closed on EOF or error.
func startScanner(r io.Reader) <-chan scanLine {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, sseMaxTokenSize), sseMaxTokenSize)

	lineCh := make(chan scanLine, 1)
	go func() {
		defer close(lineCh)
		for scanner.Scan() {
			lineCh <- scanLine{text: scanner.Text()}
		}
	}()
	return lineCh
}

// idleTimer wraps an optional timer for SSE idle detection.
type idleTimer struct {
	timer  *time.Timer
	C      <-chan time.Time
	period time.Duration
}

// newIdleTimer creates an idle timer. If d <= 0, the timer is disabled (C is nil).
func newIdleTimer(d time.Duration) *idleTimer {
	if d <= 0 {
		return &idleTimer{}
	}
	t := time.NewTimer(d)
	return &idleTimer{timer: t, C: t.C, period: d}
}

// reset restarts the idle timer. No-op if disabled.
func (it *idleTimer) reset() {
	if it.timer == nil {
		return
	}
	if !it.timer.Stop() {
		select {
		case <-it.timer.C:
		default:
		}
	}
	it.timer.Reset(it.period)
}

// stop releases timer resources. No-op if disabled.
func (it *idleTimer) stop() {
	if it.timer != nil {
		it.timer.Stop()
	}
}

// processSSELine processes a single SSE line, updating buf and emitting events.
// Returns false if the caller should stop reading.
func processSSELine(ctx context.Context, line string, buf *strings.Builder, ch chan<- StreamEvent) bool {
	if strings.HasPrefix(line, ":") {
		return true // SSE comment
	}
	if strings.HasPrefix(line, "data:") {
		appendDataLine(buf, line)
		return true
	}
	// Empty line terminates the current event.
	if line == "" && buf.Len() > 0 {
		if !emitEvent(ctx, buf.String(), ch) {
			return false
		}
		buf.Reset()
	}
	return true
}

// ReadSSEWithIdleTimeout reads SSE events from r and sends parsed StreamEvents
// to ch. If idleTimeout is positive and no line is received within that
// duration, reading stops (callers should reconnect). A zero or negative
// idleTimeout disables idle detection.
func ReadSSEWithIdleTimeout(ctx context.Context, r io.Reader, ch chan<- StreamEvent, idleTimeout time.Duration) {
	var buf strings.Builder
	lineCh := startScanner(r)
	idle := newIdleTimer(idleTimeout)
	defer idle.stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-idle.C:
			return
		case sl, open := <-lineCh:
			if !open {
				if buf.Len() > 0 {
					emitEvent(ctx, buf.String(), ch)
				}
				return
			}
			idle.reset()
			if !processSSELine(ctx, sl.text, &buf, ch) {
				return
			}
		}
	}
}

// appendDataLine extracts the data payload from an SSE "data:" line and
// appends it to buf, joining multiple data lines with newlines per the spec.
func appendDataLine(buf *strings.Builder, line string) {
	d := line[len("data:"):]
	if d != "" && d[0] == ' ' {
		d = d[1:]
	}
	if buf.Len() > 0 {
		buf.WriteByte('\n')
	}
	buf.WriteString(d)
}

// emitEvent parses data as a stream event and sends it to ch.
// Returns false if the context is canceled and the caller should stop.
func emitEvent(ctx context.Context, data string, ch chan<- StreamEvent) bool {
	evt, ok := parseStreamEvent(data)
	if !ok {
		return true
	}
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

// parseStreamEvent parses a JSON payload into a StreamEvent.
//
// It reads every version's shape: a JSON-RPC envelope or a bare object; A2A
// 1.0's {"task"|"message"|"statusUpdate"|"artifactUpdate": ...} wrapper; 0.3's
// "kind" discriminator; and the legacy untagged events, told apart by field.
func parseStreamEvent(data string) (StreamEvent, bool) {
	raw := json.RawMessage(data)

	// Unwrap JSON-RPC envelope if present.
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Result) > 0 {
		raw = envelope.Result
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return StreamEvent{}, false
	}

	// 1.0 wrapper.
	for key, inner := range fields {
		switch key {
		case streamKeyTask, streamKeyMessage, streamKeyStatus, streamKeyArtifact:
			if evt, ok := decodeStreamPayload(key, inner); ok {
				return evt, true
			}
		}
	}

	// 0.3 discriminator.
	var kind string
	if k, ok := fields["kind"]; ok && json.Unmarshal(k, &kind) == nil {
		switch kind {
		case kindTask:
			return decodeStreamPayload(streamKeyTask, raw)
		case kindMessage:
			return decodeStreamPayload(streamKeyMessage, raw)
		case kindStatusUpdate:
			return decodeStreamPayload(streamKeyStatus, raw)
		case kindArtifactUpdate:
			return decodeStreamPayload(streamKeyArtifact, raw)
		}
	}

	// Legacy: discriminate by field presence.
	if _, ok := fields["artifact"]; ok {
		return decodeStreamPayload(streamKeyArtifact, raw)
	}
	if _, ok := fields["status"]; ok {
		if _, isTask := fields["id"]; isTask {
			return decodeStreamPayload(streamKeyTask, raw)
		}
		return decodeStreamPayload(streamKeyStatus, raw)
	}

	return StreamEvent{}, false
}

// The keys of A2A 1.0's StreamResponse oneof.
const (
	streamKeyTask     = "task"
	streamKeyMessage  = "message"
	streamKeyStatus   = "statusUpdate"
	streamKeyArtifact = "artifactUpdate"
)

// decodeStreamPayload decodes raw as the named event type.
func decodeStreamPayload(kind string, raw json.RawMessage) (StreamEvent, bool) {
	switch kind {
	case streamKeyTask:
		var t Task
		if json.Unmarshal(raw, &t) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{Task: &t}, true
	case streamKeyMessage:
		var m Message
		if json.Unmarshal(raw, &m) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{Message: &m}, true
	case streamKeyStatus:
		var e TaskStatusUpdateEvent
		if json.Unmarshal(raw, &e) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{StatusUpdate: &e}, true
	case streamKeyArtifact:
		var e TaskArtifactUpdateEvent
		if json.Unmarshal(raw, &e) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{ArtifactUpdate: &e}, true
	}
	return StreamEvent{}, false
}
