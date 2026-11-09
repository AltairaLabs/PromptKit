package sdk

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// convWireRequest is one HTTP request an OpenAI-compatible provider sent.
type convWireRequest struct {
	Path   string
	Query  url.Values
	Header http.Header
	Body   map[string]any
	Raw    []byte
}

// convWireReply is one scripted chat-completions answer: text, or a tool call
// when ToolName is set.
type convWireReply struct {
	Text     string
	ToolName string
	ToolArgs string
	// Echo answers "echo:" + the request's last user text.
	Echo bool
	// Status, when set, answers with that HTTP error status.
	Status int
}

// convWire is an OpenAI-compatible endpoint that records every request and
// answers chat completions from a script (the last reply repeats). It answers
// embeddings, speech and transcription requests too, so every capability a
// ProviderSpec can build is observable on the wire.
type convWire struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []convWireRequest
	replies []convWireReply
	chats   int
}

func newConvWire(t *testing.T, replies ...convWireReply) *convWire {
	t.Helper()
	if len(replies) == 0 {
		replies = []convWireReply{{Text: "hello"}}
	}
	w := &convWire{replies: replies}
	w.Server = httptest.NewServer(http.HandlerFunc(w.serve))
	t.Cleanup(w.Close)
	return w
}

func (w *convWire) serve(rw http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	w.mu.Lock()
	w.reqs = append(w.reqs, convWireRequest{Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body, Raw: raw})
	var reply convWireReply
	if strings.HasSuffix(r.URL.Path, "/chat/completions") {
		reply = w.replies[min(w.chats, len(w.replies)-1)]
		w.chats++
	}
	w.mu.Unlock()
	if reply.Echo {
		reply.Text = "echo:" + convLastUserText(body)
	}

	switch {
	case reply.Status != 0:
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(reply.Status)
		_, _ = io.WriteString(rw, `{"error":{"message":"scripted failure","type":"server_error"}}`)
	case strings.HasSuffix(r.URL.Path, "/embeddings"):
		n := 1
		if in, ok := body["input"].([]any); ok {
			n = len(in)
		}
		data := make([]string, n)
		for i := range data {
			data[i] = `{"object":"embedding","index":` + strconv.Itoa(i) + `,"embedding":[0.25,0.5]}`
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, `{"object":"list","data":[`+strings.Join(data, ",")+
			`],"model":"m","usage":{"prompt_tokens":1,"total_tokens":1}}`)
	case strings.HasSuffix(r.URL.Path, "/audio/speech"):
		rw.Header().Set("Content-Type", "audio/mpeg")
		_, _ = rw.Write([]byte("MP3DATA"))
	case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
		rw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(rw, `{"text":"transcribed words"}`)
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		convWriteChat(rw, body, reply)
	default:
		http.NotFound(rw, r)
	}
}

// convLastUserText is the text of the last user message in a chat body.
func convLastUserText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		raw, _ := json.Marshal(m["content"])
		return convContentText(raw)
	}
	return ""
}

func convWriteChat(rw http.ResponseWriter, body map[string]any, reply convWireReply) {
	stream, _ := body["stream"].(bool)
	if stream && reply.ToolName != "" {
		rw.Header().Set("Content-Type", "text/event-stream")
		args, _ := json.Marshal(reply.ToolArgs)
		_, _ = io.WriteString(rw, `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant",`+
			`"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"`+reply.ToolName+
			`","arguments":`+string(args)+`}}]}}]}`+"\n\n"+
			`data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n"+
			"data: [DONE]\n\n")
		return
	}
	if stream {
		rw.Header().Set("Content-Type", "text/event-stream")
		half := len(reply.Text) / 2
		for _, part := range []string{reply.Text[:half], reply.Text[half:]} {
			b, _ := json.Marshal(part)
			_, _ = io.WriteString(rw, `data: {"id":"1","choices":[{"index":0,"delta":{"role":"assistant","content":`+
				string(b)+`}}]}`+"\n\n")
		}
		_, _ = io.WriteString(rw, `data: {"id":"1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`+"\n\n"+
			`data: {"id":"1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`+"\n\n"+
			"data: [DONE]\n\n")
		return
	}
	rw.Header().Set("Content-Type", "application/json")
	if reply.ToolName != "" {
		args, _ := json.Marshal(reply.ToolArgs)
		_, _ = io.WriteString(rw, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":null,`+
			`"tool_calls":[{"id":"call_1","type":"function","function":{"name":"`+reply.ToolName+
			`","arguments":`+string(args)+`}}]},"finish_reason":"tool_calls"}],`+
			`"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
		return
	}
	text, _ := json.Marshal(reply.Text)
	_, _ = io.WriteString(rw, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":`+
		string(text)+`},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
}

// requests returns every recorded request whose path ends with suffix.
func (w *convWire) requests(suffix string) []convWireRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []convWireRequest
	for _, r := range w.reqs {
		if strings.HasSuffix(r.Path, suffix) {
			out = append(out, r)
		}
	}
	return out
}

// chatRequests returns the recorded chat-completions requests.
func (w *convWire) chatRequests() []convWireRequest { return w.requests("/chat/completions") }

// lastChat returns the most recent chat-completions request, failing if none.
func (w *convWire) lastChat(t *testing.T) convWireRequest {
	t.Helper()
	reqs := w.chatRequests()
	require.NotEmpty(t, reqs, "the provider sent no chat request")
	return reqs[len(reqs)-1]
}

// last returns the most recent request on any path, failing if none.
func (w *convWire) last(t *testing.T) convWireRequest {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	require.NotEmpty(t, w.reqs, "the provider sent no request")
	return w.reqs[len(w.reqs)-1]
}

// convWireMessage is a chat-completions message reduced to role and text.
type convWireMessage struct {
	Role string
	Text string
}

// messages reduces the request's messages to role and concatenated text.
func (r convWireRequest) messages(t *testing.T) []convWireMessage {
	t.Helper()
	raw, err := json.Marshal(r.Body["messages"])
	require.NoError(t, err)
	var wire []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	require.NoError(t, json.Unmarshal(raw, &wire))
	out := make([]convWireMessage, 0, len(wire))
	for _, m := range wire {
		out = append(out, convWireMessage{Role: m.Role, Text: convContentText(m.Content)})
	}
	return out
}

// system returns the request's system message text ("" when it has none).
func (r convWireRequest) system(t *testing.T) string {
	t.Helper()
	for _, m := range r.messages(t) {
		if m.Role == "system" {
			return m.Text
		}
	}
	return ""
}

func convContentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// convWireSpec is a ProviderSpec for an OpenAI agent provider served by w.
func convWireSpec(w *convWire) ProviderSpec {
	return ProviderSpec{ID: "agent", Type: "openai", Model: "gpt-4o-mini", BaseURL: w.URL}
}

// convChatPack is a one-prompt pack whose system template names {{name}}.
const convChatPack = `{
	"id": "conv-pack",
	"version": "1.0.0",
	"prompts": {
		"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
			"system_template": "You help {{name}}.",
			"variables": [{"name": "name", "type": "string", "required": false, "default": "nobody"}]}
	}
}`

// openConv opens convChatPack's chat prompt against w, with extra options.
func openConv(t *testing.T, w *convWire, opts ...Option) *Conversation {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "test-key")
	base := []Option{WithSkipSchemaValidation(), WithLLMProvider(convWireSpec(w))}
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv
}
