package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// RPCError represents a JSON-RPC error returned by an A2A agent.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("a2a: rpc error %d: %s", e.Code, e.Message)
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

// Client is an HTTP client for discovering and calling external A2A agents.
type Client struct {
	baseURL    string
	httpClient *http.Client
	authScheme string
	authToken  string
	reqID      int64

	mu        sync.RWMutex
	agentCard *AgentCard
}

// NewClient creates a Client targeting baseURL.
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
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
		return fmt.Errorf("a2a: %s: status %d", method, resp.StatusCode)
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

	resp, err := c.httpClient.Do(httpReq) //nolint:bodyclose // closed in goroutine below
	if err != nil {
		return nil, fmt.Errorf("a2a: stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("a2a: stream: status %d", resp.StatusCode)
	}

	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		ReadSSE(ctx, resp.Body, ch)
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
func ReadSSE(ctx context.Context, r io.Reader, ch chan<- StreamEvent) {
	scanner := bufio.NewScanner(r)
	var buf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, ":") {
			continue // SSE comment
		}

		if strings.HasPrefix(line, "data:") {
			appendDataLine(&buf, line)
			continue
		}

		// Empty line terminates the current event.
		if line == "" && buf.Len() > 0 {
			if !emitEvent(ctx, buf.String(), ch) {
				return
			}
			buf.Reset()
		}
	}

	// Handle any remaining buffered data.
	if buf.Len() > 0 {
		emitEvent(ctx, buf.String(), ch)
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
