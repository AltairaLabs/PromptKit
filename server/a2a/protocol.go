package a2aserver

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/a2a"
)

// The server speaks A2A 1.0 and 0.3 over one endpoint.
//
// Every request is answered in the version it asked for: the A2A-Version
// header (or query parameter) when present, otherwise the version whose method
// name it used — 1.0's SendMessage, 0.3's message/send. The spec reads a
// request with no version as 0.3, and a 0.3 method name is exactly that; a
// 1.0 method name with no header is a 1.0 client that forgot the header, and
// answering it in 0.3 shapes would help nobody. Decoding needs no version at
// all: the runtime types accept every version's spelling.

// rpcCall is one JSON-RPC request with the protocol version to answer it in
// and, when tasks are scoped by caller, who the caller is.
type rpcCall struct {
	w     http.ResponseWriter
	r     *http.Request
	req   *a2a.JSONRPCRequest
	v     a2a.ProtocolVersion
	owner string
}

// requestVersion resolves the protocol version of a request: explicit
// A2A-Version first, the method name's version otherwise.
func requestVersion(r *http.Request, methodVersion a2a.ProtocolVersion) (a2a.ProtocolVersion, error) {
	raw := r.Header.Get(a2a.HeaderVersion)
	if raw == "" {
		raw = r.URL.Query().Get(a2a.HeaderVersion)
	}
	v, err := a2a.ParseProtocolVersion(raw)
	if err != nil {
		return "", err
	}
	if v == "" {
		v = methodVersion
	}
	return v, nil
}

// result writes a JSON-RPC success response.
func (c *rpcCall) result(result any) {
	writeRPCResult(c.w, c.req.ID, result)
}

// fail writes a JSON-RPC error response.
func (c *rpcCall) fail(code int, msg string) {
	writeRPCError(c.w, c.req.ID, code, msg)
}

// internalError logs cause and answers with a generic internal error, so
// server-side detail does not reach the caller.
func (c *rpcCall) internalError(what string, cause error) {
	log.Printf("a2a: %s: %v", what, cause)
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
