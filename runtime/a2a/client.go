package a2a

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
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
// Exactly one field will be non-nil.
type StreamEvent struct {
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

// Discover fetches the agent card from /.well-known/agent.json.
// The card is cached after the first successful call.
func (c *Client) Discover(ctx context.Context) (*AgentCard, error) {
	c.mu.RLock()
	if c.agentCard != nil {
		card := c.agentCard
		c.mu.RUnlock()
		return card, nil
	}
	c.mu.RUnlock()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/.well-known/agent.json", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("a2a: discover: %w", err)
	}
	c.setAuth(httpReq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: discover: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("a2a: discover: status %d", resp.StatusCode)
	}

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("a2a: decode agent card: %w", err)
	}

	c.mu.Lock()
	c.agentCard = &card
	c.mu.Unlock()

	return &card, nil
}

// rpcCall performs a JSON-RPC 2.0 POST to /a2a.
func (c *Client) rpcCall(ctx context.Context, method string, params, result any) error {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("a2a: marshal params: %w", err)
	}

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  paramsJSON,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return fmt.Errorf("a2a: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/a2a", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("a2a: %s: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuth(httpReq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("a2a: %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPStatusError{StatusCode: resp.StatusCode, Method: method}
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("a2a: %s: decode response: %w", method, err)
	}

	if rpcResp.Error != nil {
		return &RPCError{Code: rpcResp.Error.Code, Message: rpcResp.Error.Message}
	}

	if result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("a2a: %s: decode result: %w", method, err)
		}
	}

	return nil
}

// SendMessage sends a message/send JSON-RPC request.
func (c *Client) SendMessage(ctx context.Context, params *SendMessageRequest) (*Task, error) {
	var task Task
	if err := c.rpcCall(ctx, MethodSendMessage, params, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// SendMessageStream sends a message/stream request and returns a channel of
// streaming events. The channel is closed when the stream ends or the context
// is canceled.
func (c *Client) SendMessageStream(ctx context.Context, params *SendMessageRequest) (<-chan StreamEvent, error) {
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal params: %w", err)
	}

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  MethodSendStreamingMessage,
		Params:  paramsJSON,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/a2a", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("a2a: stream: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.setAuth(httpReq)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(httpReq.Header))

	resp, err := c.sseClient.Do(httpReq) //nolint:bodyclose // closed in goroutine below
	if err != nil {
		return nil, fmt.Errorf("a2a: stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Method: MethodSendStreamingMessage}
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

// GetTask retrieves a task by ID via tasks/get.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	var task Task
	if err := c.rpcCall(ctx, MethodGetTask, GetTaskRequest{ID: taskID}, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

// CancelTask cancels a task by ID via tasks/cancel.
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	return c.rpcCall(ctx, MethodCancelTask, CancelTaskRequest{ID: taskID}, nil)
}

// ListTasks lists tasks via tasks/list.
func (c *Client) ListTasks(ctx context.Context, params *ListTasksRequest) ([]*Task, error) {
	var resp ListTasksResponse
	if err := c.rpcCall(ctx, MethodListTasks, params, &resp); err != nil {
		return nil, err
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
// It handles both raw event objects and JSON-RPC wrapped responses.
func parseStreamEvent(data string) (StreamEvent, bool) {
	raw := json.RawMessage(data)

	// Unwrap JSON-RPC envelope if present.
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Result) > 0 {
		raw = envelope.Result
	}

	// Discriminate by field presence.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return StreamEvent{}, false
	}

	if _, ok := fields["artifact"]; ok {
		var evt TaskArtifactUpdateEvent
		if json.Unmarshal(raw, &evt) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{ArtifactUpdate: &evt}, true
	}

	if _, ok := fields["status"]; ok {
		var evt TaskStatusUpdateEvent
		if json.Unmarshal(raw, &evt) != nil {
			return StreamEvent{}, false
		}
		return StreamEvent{StatusUpdate: &evt}, true
	}

	return StreamEvent{}, false
}
