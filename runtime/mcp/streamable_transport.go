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
)

// jsonRPCVersion is the JSON-RPC envelope version used by all MCP messages.
const jsonRPCVersion = "2.0"

// sseEventMessage is the SSE event name carrying JSON-RPC payloads.
const sseEventMessage = "message"

// streamableTransport implements the MCP 2025-03-26 Streamable HTTP transport.
//
// One POST per JSON-RPC request; the response is delivered inline on the same
// POST as either application/json (one-shot) or text/event-stream (SSE-framed
// JSON-RPC messages). The transport is request-scoped — no persistent stream
// is held between calls.
type streamableTransport struct {
	config  ServerConfig
	options ClientOptions

	httpClient *http.Client
	url        string

	nextID atomic.Int64

	mu        sync.Mutex
	sessionID string // populated from Mcp-Session-Id on Initialize response

	closed atomic.Bool
	alive  atomic.Bool
}

// newStreamableTransport constructs a Streamable HTTP transport. The transport
// is considered "alive" once the first successful request has completed.
//
//nolint:gocritic // config matches existing Client constructor signatures
func newStreamableTransport(config ServerConfig, options ClientOptions) *streamableTransport {
	return &streamableTransport{
		config:     config,
		options:    options,
		httpClient: &http.Client{}, //nolint:exhaustruct // stdlib defaults are fine
		url:        config.URL,
	}
}

// close marks the transport closed. Idempotent. The Streamable HTTP transport
// holds no persistent connection, so there is nothing to tear down beyond
// flipping the flag.
func (t *streamableTransport) close() {
	if t.closed.Swap(true) {
		return
	}
	t.alive.Store(false)
}

// sendRequest issues a single JSON-RPC request and unmarshals the response
// into out. It dispatches on the response Content-Type:
//   - application/json: one-shot JSON-RPC response.
//   - text/event-stream: SSE stream; the first JSON-RPC message with a
//     matching id is the response.
func (t *streamableTransport) sendRequest(ctx context.Context, method string, params, out any) error {
	if t.closed.Load() {
		return ErrClientClosed
	}
	id := t.nextID.Add(1)

	body, err := t.buildRequestBody(id, method, params)
	if err != nil {
		return err
	}

	req, err := t.buildHTTPRequest(ctx, body)
	if err != nil {
		return err
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mcp/streamable: POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusAccepted {
		// 202 with no body: notification/empty response. No correlation needed.
		t.captureSession(resp)
		t.alive.Store(true)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcp/streamable: POST status %d", resp.StatusCode)
	}

	t.captureSession(resp)

	contentType := resp.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/json"):
		var msg JSONRPCMessage
		if derr := json.NewDecoder(resp.Body).Decode(&msg); derr != nil {
			return fmt.Errorf("mcp/streamable: decode json response: %w", derr)
		}
		t.alive.Store(true)
		return unmarshalStreamableReply(&msg, out)
	case strings.HasPrefix(contentType, "text/event-stream"):
		msg, rerr := readSSEUntilResponse(resp.Body, id)
		if rerr != nil {
			return rerr
		}
		t.alive.Store(true)
		return unmarshalStreamableReply(msg, out)
	default:
		return fmt.Errorf("mcp/streamable: unexpected content type %q", contentType)
	}
}

func (t *streamableTransport) buildRequestBody(id int64, method string, params any) ([]byte, error) {
	var paramBytes json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("mcp/streamable: marshal params: %w", err)
		}
		paramBytes = b
	}
	body, err := json.Marshal(JSONRPCMessage{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  paramBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("mcp/streamable: marshal request: %w", err)
	}
	return body, nil
}

func (t *streamableTransport) buildHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp/streamable: build POST: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	t.mu.Lock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.Unlock()
	return req, nil
}

func (t *streamableTransport) captureSession(resp *http.Response) {
	sid := resp.Header.Get("Mcp-Session-Id")
	if sid == "" {
		return
	}
	t.mu.Lock()
	t.sessionID = sid
	t.mu.Unlock()
}

// readSSEUntilResponse reads SSE frames from body until it finds a JSON-RPC
// message with the given id, then returns it. Frames whose data does not
// parse as a JSON-RPC message, or whose id does not match, are skipped
// (they may be server-initiated notifications or unrelated responses).
func readSSEUntilResponse(body io.Reader, wantID int64) (*JSONRPCMessage, error) {
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
		gotID, ok := coerceID(msg.ID)
		if !ok || gotID != wantID {
			continue
		}
		return &msg, nil
	}
}

func unmarshalStreamableReply(reply *JSONRPCMessage, out any) error {
	if reply.Error != nil {
		return fmt.Errorf("mcp/streamable: rpc error %d: %s", reply.Error.Code, reply.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(reply.Result, out); err != nil {
		return fmt.Errorf("mcp/streamable: unmarshal result: %w", err)
	}
	return nil
}
