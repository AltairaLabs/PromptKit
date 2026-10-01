package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// JSON-RPC and MCP method names the client sends or answers.
const (
	methodInitialize          = "initialize"
	methodPing                = "ping"
	methodToolsList           = "tools/list"
	methodToolsCall           = "tools/call"
	methodNotificationsCancel = "notifications/cancelled" //nolint:misspell // the spec's method name
)

// Standard JSON-RPC error codes the client emits when answering a server.
const (
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// RPCError is a JSON-RPC error returned by an MCP server. Callers can
// errors.As a request error into it to read the code and data.
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	if len(e.Data) > 0 {
		return fmt.Sprintf("JSON-RPC error %d: %s (data: %s)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("JSON-RPC error %d: %s", e.Code, e.Message)
}

// rpcErrorFrom converts a decoded JSON-RPC error object into an *RPCError,
// keeping its data.
func rpcErrorFrom(e *JSONRPCError) *RPCError {
	out := &RPCError{Code: e.Code, Message: e.Message}
	if e.Data != nil {
		if raw, err := json.Marshal(e.Data); err == nil {
			out.Data = raw
		}
	}
	return out
}

// jsonNull is the JSON null literal.
const jsonNull = "null"

// errNoResponse is returned by a conn when the server answered a request
// without a JSON-RPC response (for example a Streamable HTTP 202).
var errNoResponse = errors.New("mcp: server returned no response to the request")

// errSessionExpired is returned by a conn when the server has discarded the
// transport session the request was sent on (Streamable HTTP 404 with a
// session id). The request was not processed; the session re-initializes and
// sends it again.
var errSessionExpired = errors.New("mcp: server session expired")

// request is one outgoing JSON-RPC message. An id of 0 makes it a
// notification (the client never allocates id 0).
type request struct {
	id     int64
	method string
	params json.RawMessage
	// header carries per-request HTTP headers (MCP-Protocol-Version and the
	// like). Ignored by stdio.
	header http.Header
}

func (r *request) message() *JSONRPCMessage {
	msg := &JSONRPCMessage{JSONRPC: jsonRPCVersion, Method: r.method, Params: r.params}
	if r.id != 0 {
		msg.ID = r.id
	}
	return msg
}

// conn is one transport's connection to an MCP server: it moves JSON-RPC
// messages and nothing else. Everything the protocol says — handshakes,
// versions, retries, which methods a server may call — lives in session.
//
// send delivers a request and returns the server's response to it, and only
// that: a server-initiated request or notification that arrives while the
// caller waits (even one reusing the request's id) goes to the inbound
// handler, never to the caller.
type conn interface {
	send(ctx context.Context, req *request) (*JSONRPCMessage, error)
	notify(ctx context.Context, req *request) error
	// cancelRequest tells the server the client has stopped waiting for a
	// request: a cancellation notification, where the transport has no
	// per-request stream whose closing says so.
	cancelRequest(ctx context.Context, id int64, reason string, header http.Header)
	close() error
}

// inbound receives the messages a server sends that are not responses to
// the client's requests.
type inbound interface {
	// serverRequest answers a server-initiated request. The returned message
	// is sent back to the server as the response.
	serverRequest(ctx context.Context, msg *JSONRPCMessage) *JSONRPCMessage
	// notification handles a server notification.
	notification(msg *JSONRPCMessage)
}

// isResponse reports whether msg is a response (result or error) rather than
// a request or notification. Requests and notifications carry a method;
// responses never do.
func isResponse(msg *JSONRPCMessage) bool {
	return msg.Method == "" && msg.ID != nil
}

// isServerRequest reports whether msg is a request from the server.
func isServerRequest(msg *JSONRPCMessage) bool {
	return msg.Method != "" && msg.ID != nil
}

// replyTo builds the response message for a server request.
func replyTo(id, result any, rpcErr *JSONRPCError) *JSONRPCMessage {
	msg := &JSONRPCMessage{JSONRPC: jsonRPCVersion, ID: id, Error: rpcErr}
	if rpcErr == nil {
		raw, err := json.Marshal(result)
		if err != nil {
			msg.Error = &JSONRPCError{Code: codeInternalError, Message: err.Error()}
		} else {
			msg.Result = raw
		}
	}
	return msg
}

// dispatchInbound routes a message that is not the awaited response. Server
// requests are answered through reply; notifications go to the handler.
// Responses are dropped (an unknown or late id).
func dispatchInbound(ctx context.Context, h inbound, msg *JSONRPCMessage, reply func(*JSONRPCMessage)) {
	if h == nil {
		return
	}
	switch {
	case isServerRequest(msg):
		reply(h.serverRequest(ctx, msg))
	case msg.ID == nil && msg.Method != "":
		h.notification(msg)
	}
}

// coerceID turns a JSON-decoded id (float64 / int / int64) back into int64.
// The client only ever sends int64 ids, so non-numeric ids never match one.
func coerceID(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// cancelNotification builds the cancellation notification for an
// abandoned request.
func cancelNotification(id int64, reason string, header http.Header) *request {
	params, _ := json.Marshal(map[string]any{"requestId": id, "reason": reason})
	return &request{method: methodNotificationsCancel, params: params, header: header}
}

// routeStreamMessage handles one message read from a stream that carries
// responses alongside server traffic. It returns the response (and true) when
// msg is the response to wantID; anything else that is not a response is
// dispatched to the inbound handler.
func routeStreamMessage(ctx context.Context, h inbound, msg *JSONRPCMessage, wantID int64,
	reply func(*JSONRPCMessage)) (*JSONRPCMessage, bool) {
	if !isResponse(msg) {
		dispatchInbound(ctx, h, msg, reply)
		return nil, false
	}
	gotID, ok := coerceID(msg.ID)
	return msg, ok && gotID == wantID
}

// marshalParams encodes request params; nil stays absent.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal params: %w", err)
	}
	return b, nil
}
