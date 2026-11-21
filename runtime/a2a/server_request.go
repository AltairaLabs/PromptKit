package a2a

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// This file is the request half of an A2A JSON-RPC server: how a server reads
// one request, decides its protocol version and refuses what it must. The
// PromptKit server (server/a2a) and the mock server (a2a/mock) both use it, so
// a test against the mock sees what the real server does.

// jsonRPCVersion is the only JSON-RPC version an A2A request may name.
const jsonRPCVersion = "2.0"

// jsonNull is JSON's null literal.
const jsonNull = "null"

// ServerRequest is one JSON-RPC request a server has read and checked: a
// JSON-RPC 2.0 Request object, for a method the server knows, in a protocol
// version that has that method.
type ServerRequest struct {
	JSONRPCRequest
	// Operation is what Method names, independent of its version.
	Operation Operation
	// Version is the protocol version to answer in.
	Version ProtocolVersion
}

// ReadServerRequest reads one JSON-RPC request from r's body and checks it as
// an A2A server must. When the request cannot be served it returns nil and
// the error response to send, with the request's id when that could be read:
//
//   - a body that is not JSON is Parse error (-32700);
//   - a body that is not a JSON-RPC 2.0 Request object is Invalid Request
//     (-32600): "jsonrpc" not exactly "2.0", a method that is not a non-empty
//     string, an id that is not a string, number or null, or a batch;
//   - an unknown method is Method not found (-32601);
//   - an unsupported A2A-Version, or a 1.0 method name in a 0.3 request, is
//     VersionNotSupportedError (-32009), and a 0.3 method name in a 1.0
//     request is Method not found.
//
// The version is the one A2A-Version names (see [RequestVersion]); a request
// that names none is 0.3 (A2A 1.0 §3.6.2). The id is kept as the JSON text it
// was sent as, so a response echoes it exactly.
func ReadServerRequest(r *http.Request) (*ServerRequest, *JSONRPCResponse) {
	var body json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, errorResponse(nil, &JSONRPCError{Code: ErrCodeParse, Message: "Parse error"})
	}
	req, id, problem := decodeRequest(body)
	if problem != "" {
		return nil, errorResponse(id, &JSONRPCError{Code: ErrCodeInvalidRequest, Message: "Invalid Request: " + problem})
	}
	op, methodVersion, known := LookupMethod(req.Method)
	if !known {
		return nil, errorResponse(id, &JSONRPCError{Code: ErrCodeMethodNotFound, Message: "Method not found"})
	}
	v, named, err := RequestVersion(r)
	if err != nil {
		return nil, errorResponse(id, ErrorFor(ProtocolVersion10, ErrCodeVersionNotSupported, err.Error(),
			versionMetadata(named)))
	}
	if code, msg := methodMismatch(req.Method, methodVersion, v, named != ""); code != 0 {
		return nil, errorResponse(id, ErrorFor(v, code, msg, versionMetadata(string(v))))
	}
	return &ServerRequest{JSONRPCRequest: *req, Operation: op, Version: v}, nil
}

// errorResponse is a JSON-RPC 2.0 error response.
func errorResponse(id any, e *JSONRPCError) *JSONRPCResponse {
	return &JSONRPCResponse{JSONRPC: jsonRPCVersion, ID: id, Error: e}
}

// RequestVersion resolves the protocol version a request names in its
// A2A-Version header or, when no header line names one, its A2A-Version query
// parameter. A blank value names nothing, and a request that names nothing is
// 0.3 (A2A 1.0 §3.6.2). named is the value the request named, "" for none.
// Header lines that name different versions are an error, as is a version the
// package does not speak.
func RequestVersion(r *http.Request) (v ProtocolVersion, named string, err error) {
	values := namedVersions(r)
	if len(values) == 0 {
		return ProtocolVersion03, "", nil
	}
	for _, s := range values {
		parsed, err := ParseProtocolVersion(s)
		if err != nil {
			return "", s, err
		}
		if v != "" && parsed != v {
			return "", strings.Join(values, ", "), conflictingVersions(values)
		}
		v = parsed
	}
	return v, values[0], nil
}

// namedVersions returns the non-blank A2A-Version values of r's header lines,
// or, when there are none, of its query parameter.
func namedVersions(r *http.Request) []string {
	var values []string
	for _, line := range r.Header.Values(HeaderVersion) {
		if s := strings.TrimSpace(line); s != "" {
			values = append(values, s)
		}
	}
	if len(values) == 0 {
		if s := strings.TrimSpace(r.URL.Query().Get(HeaderVersion)); s != "" {
			values = append(values, s)
		}
	}
	return values
}

// conflictingVersions is the error for header lines naming different
// versions.
func conflictingVersions(values []string) error {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return fmt.Errorf("a2a: %s names more than one version (%s)", HeaderVersion, strings.Join(quoted, ", "))
}

// methodMismatch reports a method name the request's version does not have,
// as the error code and message to answer with; code is 0 when they agree.
func methodMismatch(method string, methodVersion, v ProtocolVersion, named bool) (code int, msg string) {
	switch {
	case methodVersion == v:
		return 0, ""
	case methodVersion == ProtocolVersion10 && !named:
		return ErrCodeVersionNotSupported, fmt.Sprintf("Version not supported: %s is an A2A 1.0 method, "+
			"and a request without A2A-Version is A2A 0.3; send A2A-Version: 1.0", method)
	case methodVersion == ProtocolVersion10:
		return ErrCodeVersionNotSupported, fmt.Sprintf("Version not supported: %s is an A2A 1.0 method, "+
			"and the request names A2A %s; send A2A-Version: 1.0", method, v)
	default:
		op, _, _ := LookupMethod(method)
		return ErrCodeMethodNotFound, fmt.Sprintf("Method not found: %s is not an A2A %s method%s",
			method, v, renamedIn(v, op))
	}
}

// renamedIn says what version v calls op, when it has one name for it.
func renamedIn(v ProtocolVersion, op Operation) string {
	if name := v.Method(op); name != "" {
		return fmt.Sprintf(" (A2A %s names it %s)", v, name)
	}
	return ""
}

// decodeRequest checks that body is a JSON-RPC 2.0 Request object (JSON-RPC
// 2.0 §4) and returns it. When it is not, problem says why and id is the
// request's id, if that could be read, for the Invalid Request response.
func decodeRequest(body json.RawMessage) (req *JSONRPCRequest, id any, problem string) {
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
	return &JSONRPCRequest{JSONRPC: version, ID: id, Method: method, Params: env["params"]}, id, ""
}

// requestID validates a request's id member: a string, a number or null,
// kept as its JSON text (a json.RawMessage) so a response echoes it exactly —
// a number past 2^53 is not rounded through a float64. An absent id is
// answered as null.
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

// errorInfoReasons are the google.rpc.ErrorInfo reasons of the A2A errors:
// the error's name in UPPER_SNAKE_CASE without "Error" (A2A 1.0 §9.5).
var errorInfoReasons = map[int]string{
	ErrCodeTaskNotFound:                   "TASK_NOT_FOUND",
	ErrCodeTaskNotCancelable:              "TASK_NOT_CANCELABLE",
	ErrCodePushNotificationNotSupported:   "PUSH_NOTIFICATION_NOT_SUPPORTED",
	ErrCodeUnsupportedOperation:           "UNSUPPORTED_OPERATION",
	ErrCodeContentTypeNotSupported:        "CONTENT_TYPE_NOT_SUPPORTED",
	ErrCodeInvalidAgentResponse:           "INVALID_AGENT_RESPONSE",
	ErrCodeExtendedAgentCardNotConfigured: "EXTENDED_AGENT_CARD_NOT_CONFIGURED",
	ErrCodeExtensionSupportRequired:       "EXTENSION_SUPPORT_REQUIRED",
	ErrCodeVersionNotSupported:            "VERSION_NOT_SUPPORTED",
}

// errorInfo is a google.rpc.ErrorInfo in ProtoJSON form.
type errorInfo struct {
	Type     string            `json:"@type"`
	Reason   string            `json:"reason"`
	Domain   string            `json:"domain"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ErrorFor builds the JSON-RPC error object a server sends for code, in
// version v's shape. A 1.0 A2A error (-32001 to -32009) carries a
// google.rpc.ErrorInfo in data, an array of typed details (A2A 1.0 §9.5), with
// metadata when there is any; so does VersionNotSupportedError, which only
// 1.0 defines, whatever version the request was read as. A 0.3 error, whose
// clients read data as an object, and a standard JSON-RPC error carry no data.
func ErrorFor(v ProtocolVersion, code int, message string, metadata map[string]string) *JSONRPCError {
	e := &JSONRPCError{Code: code, Message: message}
	reason, isA2A := errorInfoReasons[code]
	if isA2A && (v != ProtocolVersion03 || code == ErrCodeVersionNotSupported) {
		e.Data = []errorInfo{{
			Type:     "type.googleapis.com/google.rpc.ErrorInfo",
			Reason:   reason,
			Domain:   "a2a-protocol.org",
			Metadata: metadata,
		}}
	}
	return e
}

// versionMetadata is a VersionNotSupportedError's ErrorInfo metadata: the
// version the request named and the versions this package speaks.
func versionMetadata(requested string) map[string]string {
	return map[string]string{
		"requestedVersion":  requested,
		"supportedVersions": string(ProtocolVersion10) + "," + string(ProtocolVersion03),
	}
}
