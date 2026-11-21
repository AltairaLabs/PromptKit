package a2aserver

import (
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
	// tenant is the tenant an A2A 1.0 request names in its params, "" for
	// none; 0.3 has no tenant.
	tenant string
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

// result writes a JSON-RPC success response.
func (c *rpcCall) result(result any) {
	writeRPCResult(c.w, c.req.ID, result, c.logger())
}

// fail writes a JSON-RPC error response.
func (c *rpcCall) fail(code int, msg string) {
	writeRPCErrorObject(c.w, http.StatusOK, c.req.ID, a2a.ErrorFor(c.v, code, msg, nil))
}

// failTask writes a JSON-RPC error response about taskID, which a 1.0
// error's ErrorInfo names.
func (c *rpcCall) failTask(code int, msg, taskID string) {
	writeRPCErrorObject(c.w, http.StatusOK, c.req.ID, a2a.ErrorFor(c.v, code, msg, map[string]string{"taskId": taskID}))
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
	if len(c.req.Params) == 0 || string(c.req.Params) == "null" {
		c.fail(a2a.ErrCodeInvalidParams, "Invalid params: params are required")
		return false
	}
	if err := json.Unmarshal(c.req.Params, dst); err != nil {
		c.fail(a2a.ErrCodeInvalidParams, fmt.Sprintf("Invalid params: %v", err))
		return false
	}
	return true
}

// errPushNotSupported is PushNotificationNotSupportedError's message.
const errPushNotSupported = "Push notifications are not supported"

// decodeSendParams decodes SendMessage params, as decodeParams does, and
// refuses a message carrying an inline push notification config: the server
// delivers no push notifications, and running the message with the config
// silently dropped would leave the caller waiting for notifications that
// never come (A2A 1.0 §3.3.4).
func (c *rpcCall) decodeSendParams(params *a2a.SendMessageRequest) bool {
	if !c.decodeParams(params) {
		return false
	}
	if params.Configuration != nil && params.Configuration.TaskPushNotificationConfig != nil {
		c.fail(a2a.ErrCodePushNotificationNotSupported, errPushNotSupported)
		return false
	}
	if params.Configuration != nil {
		return c.checkHistoryLength(params.Configuration.HistoryLength)
	}
	return true
}

// checkHistoryLength refuses a negative historyLength (A2A 1.0 §3.2.4 defines
// unset, zero and positive), answering InvalidParams itself.
func (c *rpcCall) checkHistoryLength(n *int) bool {
	if n != nil && *n < 0 {
		c.fail(a2a.ErrCodeInvalidParams, "Invalid params: historyLength must not be negative")
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

// requestTenant reads the tenant every A2A 1.0 request's params may carry.
// Params that are not an object name none.
func requestTenant(params json.RawMessage) string {
	var p struct {
		Tenant string `json:"tenant"`
	}
	if json.Unmarshal(params, &p) != nil {
		return ""
	}
	return p.Tenant
}

// checkStatusFilter refuses a ListTasks status filter that names no state. The
// runtime decodes an unrecognized state as TaskStateUnknown, which suits a
// client reading a newer server; a filter is a caller's input, and listing
// every task for a misspelled one would answer a question nobody asked.
func checkStatusFilter(params json.RawMessage) error {
	var raw struct {
		Status *string `json:"status"`
	}
	if json.Unmarshal(params, &raw) != nil || raw.Status == nil {
		return nil
	}
	_, err := a2a.ParseTaskState(*raw.Status)
	return err
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
// the streaming and push notification capabilities the server actually has:
// streaming unless the server was built WithoutStreaming.
//
// A card with no interfaces gets one pointing at this server's /a2a endpoint,
// derived from the request's Host. A JSON-RPC interface under any of the names
// PromptKit used before 1.0 is renamed "JSONRPC", one without a version is
// 1.0, and if no 0.3 JSON-RPC interface is declared a twin of the first 1.0 one
// is added, so 0.3 clients (which read url/preferredTransport) and 1.0 clients
// (which read supportedInterfaces) both find the endpoint.
func servedCard(card *a2a.AgentCard, r *http.Request, streaming bool) *a2a.AgentCard {
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
	// §3.3.4): unless streaming is off, it answers SendStreamingMessage and
	// SubscribeToTask in every mode — a conversation that cannot stream is
	// streamed from its Send result (sendAsStream) — and it refuses every
	// push notification method.
	cp.Capabilities.Streaming = streaming
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
