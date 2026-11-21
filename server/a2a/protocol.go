package a2aserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// The server speaks A2A 1.0 and 0.3 over one endpoint.
//
// Every request is answered in the version its A2A-Version header (or query
// parameter) names, and a request without one is 0.3 (A2A 1.0 §3.6.1,
// §3.6.2). The method name has to be one that version has: a 1.0 name in a
// 0.3 request is VersionNotSupportedError, telling the 1.0 client that left
// the header off to send it, and a 0.3 name in a 1.0 request is
// MethodNotFound, since 1.0 has no such method. Decoding needs no version at
// all: the runtime types accept every version's spelling.

// rpcCall is one JSON-RPC request with the protocol version to answer it in
// and, when tasks are scoped by caller, who the caller is.
type rpcCall struct {
	w     http.ResponseWriter
	r     *http.Request
	req   *a2a.JSONRPCRequest
	v     a2a.ProtocolVersion
	owner string
	// caller is who the request acts for, under NewServerFor (convCaller).
	caller string
	// log returns the server's logger; nil means PromptKit's logger.
	log func() *slog.Logger
}

// logger returns the logger the call writes to.
func (c *rpcCall) logger() *slog.Logger {
	if c.log != nil {
		return c.log()
	}
	return logger.GetLogger()
}

// ctx is the request's context, for log records, or a background context for
// a call built without a request.
func (c *rpcCall) ctx() context.Context {
	if c.r != nil {
		return c.r.Context()
	}
	return context.Background()
}

// requestVersion resolves the protocol version a request names in its
// A2A-Version header or query parameter, and 0.3 when it names none (A2A 1.0
// §3.6.2). raw is the value it named.
func requestVersion(r *http.Request) (v a2a.ProtocolVersion, raw string, err error) {
	raw = r.Header.Get(a2a.HeaderVersion)
	if raw == "" {
		raw = r.URL.Query().Get(a2a.HeaderVersion)
	}
	if v, err = a2a.ParseProtocolVersion(raw); err != nil || v != "" {
		return v, raw, err
	}
	return a2a.ProtocolVersion03, raw, nil
}

// methodMismatch reports a method name the request's version does not have,
// as the error code and message to answer with; code is 0 when they agree.
func methodMismatch(method string, methodVersion, v a2a.ProtocolVersion, headerSent bool) (code int, msg string) {
	switch {
	case methodVersion == v:
		return 0, ""
	case methodVersion == a2a.ProtocolVersion10 && !headerSent:
		return a2a.ErrCodeVersionNotSupported, fmt.Sprintf("Version not supported: %s is an A2A 1.0 method, "+
			"and a request without A2A-Version is A2A 0.3; send A2A-Version: 1.0", method)
	case methodVersion == a2a.ProtocolVersion10:
		return a2a.ErrCodeVersionNotSupported, fmt.Sprintf("Version not supported: %s is an A2A 1.0 method, "+
			"and the request names A2A %s; send A2A-Version: 1.0", method, v)
	default:
		op, _, _ := a2a.LookupMethod(method)
		return a2a.ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s is not an A2A %s method%s",
			method, v, renamedIn(v, op))
	}
}

// renamedIn says what version v calls op, when it has one name for it.
func renamedIn(v a2a.ProtocolVersion, op a2a.Operation) string {
	if name := v.Method(op); name != "" {
		return fmt.Sprintf(" (A2A %s names it %s)", v, name)
	}
	return ""
}

// decodeRequest checks that body is a JSON-RPC 2.0 Request object (JSON-RPC
// 2.0 §4) and returns it. When it is not, problem says why and id is the
// request's id, if that could be read, for the Invalid Request response.
//
// The id is kept as the JSON text it was sent as (a json.RawMessage), so the
// response echoes it exactly: a number past 2^53 is not rounded through a
// float64.
func decodeRequest(body json.RawMessage) (req *a2a.JSONRPCRequest, id any, problem string) {
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		return nil, nil, "batch requests are not supported"
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(body, &env); err != nil || env == nil {
		return nil, nil, "the request must be a JSON object"
	}
	id, ok := requestID(env["id"])
	if !ok {
		return nil, nil, `"id" must be a string, a number or null`
	}
	var version string
	if err := json.Unmarshal(env["jsonrpc"], &version); err != nil || version != jsonRPCVersion {
		return nil, id, `"jsonrpc" must be exactly "2.0"`
	}
	var method string
	if err := json.Unmarshal(env["method"], &method); err != nil || method == "" {
		return nil, id, `"method" must be a non-empty string`
	}
	return &a2a.JSONRPCRequest{JSONRPC: version, ID: id, Method: method, Params: env["params"]}, id, ""
}

// jsonNull is JSON's null literal.
const jsonNull = "null"

// requestID validates a request's id member: a string, a number or null. An
// absent id is answered as null.
func requestID(raw json.RawMessage) (any, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == jsonNull {
		return nil, true
	}
	switch c := trimmed[0]; {
	case c == '"', c == '-', c >= '0' && c <= '9':
		return json.RawMessage(trimmed), true
	}
	return nil, false
}

// result writes a JSON-RPC success response.
func (c *rpcCall) result(result any) {
	writeRPCResult(c.w, c.req.ID, result, c.logger())
}

// fail writes a JSON-RPC error response.
func (c *rpcCall) fail(code int, msg string) {
	writeRPCErrorObject(c.w, http.StatusOK, c.req.ID, rpcError(c.v, code, msg, nil))
}

// failTask writes a JSON-RPC error response about taskID, which a 1.0
// error's ErrorInfo names.
func (c *rpcCall) failTask(code int, msg, taskID string) {
	writeRPCErrorObject(c.w, http.StatusOK, c.req.ID, rpcError(c.v, code, msg, map[string]string{"taskId": taskID}))
}

// errorInfoReasons are the google.rpc.ErrorInfo reasons of the A2A errors:
// the error's name in UPPER_SNAKE_CASE without "Error" (A2A 1.0 §9.5).
var errorInfoReasons = map[int]string{
	a2a.ErrCodeTaskNotFound:                   "TASK_NOT_FOUND",
	a2a.ErrCodeTaskNotCancelable:              "TASK_NOT_CANCELABLE",
	a2a.ErrCodePushNotificationNotSupported:   "PUSH_NOTIFICATION_NOT_SUPPORTED",
	a2a.ErrCodeUnsupportedOperation:           "UNSUPPORTED_OPERATION",
	a2a.ErrCodeContentTypeNotSupported:        "CONTENT_TYPE_NOT_SUPPORTED",
	a2a.ErrCodeInvalidAgentResponse:           "INVALID_AGENT_RESPONSE",
	a2a.ErrCodeExtendedAgentCardNotConfigured: "EXTENDED_AGENT_CARD_NOT_CONFIGURED",
	a2a.ErrCodeExtensionSupportRequired:       "EXTENSION_SUPPORT_REQUIRED",
	a2a.ErrCodeVersionNotSupported:            "VERSION_NOT_SUPPORTED",
}

// errorInfo is a google.rpc.ErrorInfo in ProtoJSON form.
type errorInfo struct {
	Type     string            `json:"@type"`
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// rpcError builds the error object for code in version v's shape. A 1.0 A2A
// error carries a google.rpc.ErrorInfo in data, an array of typed details
// (A2A 1.0 §9.5); so does VersionNotSupportedError, which only 1.0 defines,
// whatever version the request was read as. A 0.3 error, whose clients read
// data as an object, and a standard JSON-RPC error carry no data.
func rpcError(v a2a.ProtocolVersion, code int, msg string, metadata map[string]string) *a2a.JSONRPCError {
	e := &a2a.JSONRPCError{Code: code, Message: msg}
	reason, isA2A := errorInfoReasons[code]
	if isA2A && (v != a2a.ProtocolVersion03 || code == a2a.ErrCodeVersionNotSupported) {
		e.Data = []errorInfo{{
			Type:     "type.googleapis.com/google.rpc.ErrorInfo",
			Reason:   reason,
			Domain:   "a2a-protocol.org",
			Metadata: metadata,
		}}
	}
	return e
}

// internalError logs cause and answers with a generic internal error, so
// server-side detail does not reach the caller.
func (c *rpcCall) internalError(what string, cause error) {
	c.logger().ErrorContext(c.ctx(), "a2a: "+what, "error", cause)
	c.fail(a2a.ErrCodeInternal, "Internal error")
}

// decodeParams unmarshals the request params into dst, answering with
// InvalidParams when they do not parse.
func (c *rpcCall) decodeParams(dst any) bool {
	if len(c.req.Params) == 0 || string(c.req.Params) == jsonNull {
		c.fail(a2a.ErrCodeInvalidParams, "Invalid params: params are required")
		return false
	}
	if err := json.Unmarshal(c.req.Params, dst); err != nil {
		c.fail(a2a.ErrCodeInvalidParams, fmt.Sprintf("Invalid params: %v", err))
		return false
	}
	return true
}

// applyHistoryLength trims task history per the historyLength semantics
// (A2A 1.0 §3.2.4): unset keeps it all, 0 drops it, n keeps the last n.
func applyHistoryLength(task *a2a.Task, historyLength *int) {
	if historyLength == nil {
		return
	}
	n := *historyLength
	switch {
	case n <= 0:
		task.History = nil
	case n < len(task.History):
		task.History = task.History[len(task.History)-n:]
	}
}

// encodePageToken and decodePageToken make an opaque cursor from a position in
// the ordered result set.
func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(raw), "o:") {
		return 0, fmt.Errorf("invalid page token")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid page token")
	}
	return offset, nil
}

// servedCard returns the card as this server should publish it: a copy with
// the JSON-RPC interface declared for both versions the server speaks, and
// the streaming and push notification capabilities the server actually has.
//
// A card with no interfaces gets one pointing at this server's /a2a endpoint,
// derived from the request's Host. A JSON-RPC interface under any of the names
// PromptKit used before 1.0 is renamed "JSONRPC", one without a version is
// 1.0, and if no 0.3 JSON-RPC interface is declared a twin of the first 1.0 one
// is added, so 0.3 clients (which read url/preferredTransport) and 1.0 clients
// (which read supportedInterfaces) both find the endpoint.
func servedCard(card *a2a.AgentCard, r *http.Request) *a2a.AgentCard {
	cp := *card
	cp.SupportedInterfaces = normalizeInterfaces(card.SupportedInterfaces)

	first, declared := firstJSONRPCInterface(cp.SupportedInterfaces)
	if first == nil {
		self := a2a.AgentInterface{
			URL:             requestBaseURL(r) + "/a2a",
			ProtocolBinding: a2a.ProtocolBindingJSONRPC,
			ProtocolVersion: string(a2a.ProtocolVersion10),
		}
		cp.SupportedInterfaces = append([]a2a.AgentInterface{self}, cp.SupportedInterfaces...)
		first = &self
		declared[a2a.ProtocolVersion10] = true
	}
	twin := *first
	for _, v := range []a2a.ProtocolVersion{a2a.ProtocolVersion10, a2a.ProtocolVersion03} {
		if !declared[v] {
			twin.ProtocolVersion = string(v)
			cp.SupportedInterfaces = append(cp.SupportedInterfaces, twin)
		}
	}

	// Declare what the server serves, whatever the card said (A2A 1.0
	// §3.3.4): it answers SendStreamingMessage and SubscribeToTask in every
	// mode — a conversation that cannot stream is streamed from its Send
	// result (sendAsStream) — and refuses every push notification method.
	cp.Capabilities.Streaming = true
	cp.Capabilities.PushNotifications = false
	return &cp
}

// normalizeInterfaces copies ifaces with JSON-RPC bindings under their 1.0
// name, and version 1.0 where none is declared.
func normalizeInterfaces(ifaces []a2a.AgentInterface) []a2a.AgentInterface {
	out := make([]a2a.AgentInterface, 0, len(ifaces))
	for _, iface := range ifaces {
		if a2a.IsJSONRPCBinding(iface.ProtocolBinding) {
			iface.ProtocolBinding = a2a.ProtocolBindingJSONRPC
		}
		if iface.ProtocolBinding == a2a.ProtocolBindingJSONRPC && iface.ProtocolVersion == "" {
			iface.ProtocolVersion = string(a2a.ProtocolVersion10)
		}
		out = append(out, iface)
	}
	return out
}

// firstJSONRPCInterface returns the first JSON-RPC interface in ifaces, and the
// protocol versions JSON-RPC interfaces declare.
func firstJSONRPCInterface(ifaces []a2a.AgentInterface) (*a2a.AgentInterface, map[a2a.ProtocolVersion]bool) {
	var first *a2a.AgentInterface
	declared := map[a2a.ProtocolVersion]bool{}
	for i := range ifaces {
		iface := &ifaces[i]
		if iface.ProtocolBinding != a2a.ProtocolBindingJSONRPC {
			continue
		}
		if v, err := a2a.ParseProtocolVersion(iface.ProtocolVersion); err == nil {
			declared[v] = true
		}
		if first == nil {
			first = iface
		}
	}
	return first, declared
}

// requestBaseURL reconstructs the scheme and host a caller reached the server
// on. X-Forwarded-* headers are deliberately ignored: any caller can set them,
// and a cached card that trusted them would send other callers — and their
// credentials — wherever the header pointed. Behind a proxy, declare the
// public URL in the card's SupportedInterfaces instead.
func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
